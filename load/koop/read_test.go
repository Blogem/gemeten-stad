package koop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// This file exercises the load-koop-assembly resilience follow-up's readPublications skip
// behavior against the DOCUMENTED (unchanged) readPublications(*shared.RawStore) ([]Publication,
// error) contract: read.go is implemented by a concurrent coder agent and is not visible here.
// readPublications must now skip (log + continue) a record whose bytes are unreadable, whose SRU
// XML fails to parse, or whose sidecar read/parse fails, rather than aborting the whole batch —
// an error is returned only when the koop landing dir itself can't be listed. readPublications
// reads directly off disk (store.BasePath/koop), so this is unit-testable with a plain t.TempDir,
// no database.

// malformedRecordXML is deliberately unparseable SRU inner XML: the <dcterms:title> start tag is
// never closed by a matching </dcterms:title> before </srw:recordData> arrives, so parseRecord's
// decoder.DecodeElement for the title element fails with a genuine XML syntax error (mirrors the
// task contract's own "<title>oops unclosed" example).
const malformedRecordXML = `<srw:recordData>
  <dcterms:title>oops
</srw:recordData>
`

// landCopy copies testdata/<fixtureBase>.xml into store's koop/<name> — used to reuse the
// existing besluit_record.xml / metadata_sidecar.xml fixtures under the gmb-id-shaped filenames
// readPublications requires, without duplicating their contents inline.
func landCopy(t *testing.T, store *shared.RawStore, fixturePath, name string) {
	t.Helper()
	contents, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	writeKoopFile(t, store, name, contents)
}

// writeKoopFile writes contents directly under store's koop/ directory as name, creating the
// directory as needed.
func writeKoopFile(t *testing.T, store *shared.RawStore, name string, contents []byte) {
	t.Helper()
	dir := filepath.Join(store.BasePath, "koop")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), contents, 0o644))
}

// TestReadPublications_SkipsMalformedRecord covers the resilience follow-up's core scenario: one
// valid record + sidecar pair alongside one record whose SRU XML fails to parse. readPublications
// must return no error, skipping the malformed record while still returning the valid one.
func TestReadPublications_SkipsMalformedRecord(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	landCopy(t, store, "testdata/besluit_record.xml", "gmb-2022-900100.xml")
	landCopy(t, store, "testdata/metadata_sidecar.xml", "gmb-2022-900100.metadata.xml")

	writeKoopFile(t, store, "gmb-2022-900199.xml", []byte(malformedRecordXML))

	pubs, err := readPublications(store)
	require.NoError(t, err, "a per-record parse failure must not abort the whole batch")

	require.Len(t, pubs, 1, "expected exactly the one valid record, malformed record skipped")
	assert.Equal(t, "gmb-2022-900100", pubs[0].ID)

	for _, pub := range pubs {
		assert.NotEqual(t, "gmb-2022-900199", pub.ID, "the malformed record must not appear in the result")
	}
}

// TestReadPublications_MissingSidecarYieldsKeylessPublication covers the keyless path: a valid
// record with no metadata sidecar on disk at all must still come back as a Publication (with
// Zaaknummer == ""), confirming the new per-record skip logic didn't fold this pre-existing case
// into a skip.
func TestReadPublications_MissingSidecarYieldsKeylessPublication(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	landCopy(t, store, "testdata/besluit_record.xml", "gmb-2022-900200.xml")
	// deliberately no gmb-2022-900200.metadata.xml sidecar

	pubs, err := readPublications(store)
	require.NoError(t, err)

	require.Len(t, pubs, 1)
	assert.Equal(t, "gmb-2022-900200", pubs[0].ID)
	assert.Empty(t, pubs[0].Zaaknummer, "a record with no sidecar must yield a keyless Publication")
	assert.Nil(t, pubs[0].RawMetadata)
}
