package koop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// -- Land verbatim, with provenance -------------------------------------------
//
// Covers spec scenarios "A harvested publication is landed verbatim" and "No
// structured field is extracted or dropped at harvest": the artifact bytes on
// disk must equal the SRU record's own InnerXML byte-for-byte (no field
// mapping, filtering, or reshaping), and a provenance record must capture the
// source URL, fetch timestamp, byte size, and content hash.

func TestIngest_LandsPublicationVerbatimWithProvenance(t *testing.T) {
	fixture := readFixture(t, "single_publication.xml")

	// Compute the expected verbatim bytes independently of the harvester,
	// straight off the shared SRU parser, so this test does not merely
	// echo back whatever the harvester happens to write.
	_, wantRecords, err := shared.ParseSRUResponse(fixture)
	require.NoError(t, err)
	require.Len(t, wantRecords, 1)
	want := wantRecords[0]
	require.Equal(t, "gmb-2022-900001", want.Identifier)

	store := shared.NewRawStore(t.TempDir())
	before := time.Now()

	var calls []string
	httpGet := fakeSRUGetter(t, &calls, map[string][]byte{"1": fixture})
	require.NoError(t, Ingest(context.Background(), store, httpGet))

	after := time.Now()

	const name = "koop/gmb-2022-900001.xml"
	gotBytes, err := os.ReadFile(filepath.Join(store.BasePath, name))
	require.NoError(t, err)

	assert.Equal(t, string(want.InnerXML), string(gotBytes),
		"the landed artifact bytes must equal the SRU record's verbatim InnerXML -- no field mapping, filtering, or reshaping")

	history, err := store.ProvenanceHistory(name)
	require.NoError(t, err)
	require.Len(t, history, 1, "a freshly landed publication must have exactly one provenance line")

	prov := history[0]
	assert.NotEmpty(t, prov.SourceURL, "provenance must record the source SRU URL")
	assert.True(t, !prov.FetchedAt.Before(before) && !prov.FetchedAt.After(after),
		"provenance's fetched_at (%v) must fall within the run's wall-clock window [%v, %v]", prov.FetchedAt, before, after)
	assert.Equal(t, int64(len(gotBytes)), prov.ByteSize, "provenance's byte_size must match the artifact's actual byte length")
	assert.Equal(t, sha256Hex(gotBytes), prov.ContentSHA256, "provenance's content_sha256 must match sha256 of the exact landed bytes")
}

// -- Changed publication is re-landed ------------------------------------------
//
// Covers spec scenario "A changed publication is re-landed" and design D4: a
// correction (same identifier, different content) must overwrite the artifact
// bytes with the new content AND append a second provenance line, preserving
// the original provenance record rather than replacing it.

func TestIngest_ChangedPublicationIsReLandedWithAppendedProvenance(t *testing.T) {
	original := readFixture(t, "single_publication.xml")
	corrected := readFixture(t, "single_publication_corrected.xml")

	_, originalRecords, err := shared.ParseSRUResponse(original)
	require.NoError(t, err)
	_, correctedRecords, err := shared.ParseSRUResponse(corrected)
	require.NoError(t, err)
	require.NotEqual(t, string(originalRecords[0].InnerXML), string(correctedRecords[0].InnerXML),
		"sanity: the two fixtures must actually carry different bytes for the same publication id")

	store := shared.NewRawStore(t.TempDir())
	const name = "koop/gmb-2022-900001.xml"

	var calls []string
	httpGet := fakeSRUGetter(t, &calls, map[string][]byte{"1": original})
	require.NoError(t, Ingest(context.Background(), store, httpGet))

	firstBytes, err := os.ReadFile(filepath.Join(store.BasePath, name))
	require.NoError(t, err)
	firstHistory, err := store.ProvenanceHistory(name)
	require.NoError(t, err)
	require.Len(t, firstHistory, 1)

	// Second run: the SAME publication id, but the upstream record's content
	// has changed (a correction).
	httpGet = fakeSRUGetter(t, &calls, map[string][]byte{"1": corrected})
	require.NoError(t, Ingest(context.Background(), store, httpGet))

	secondBytes, err := os.ReadFile(filepath.Join(store.BasePath, name))
	require.NoError(t, err)
	assert.NotEqual(t, string(firstBytes), string(secondBytes),
		"the artifact bytes must be updated to the corrected content")
	assert.Equal(t, string(correctedRecords[0].InnerXML), string(secondBytes),
		"the artifact bytes must equal the corrected record's verbatim InnerXML")

	secondHistory, err := store.ProvenanceHistory(name)
	require.NoError(t, err)
	require.Len(t, secondHistory, 2, "a changed publication must APPEND a new provenance line, not replace the existing one")

	assert.Equal(t, firstHistory[0], secondHistory[0],
		"the original provenance record must be preserved unchanged as history[0]")
	assert.Equal(t, sha256Hex(secondBytes), secondHistory[1].ContentSHA256,
		"the new provenance line must reflect the corrected content's hash")
	assert.NotEqual(t, secondHistory[0].ContentSHA256, secondHistory[1].ContentSHA256)
}
