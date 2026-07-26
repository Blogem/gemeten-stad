package koop

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// -- Identifier validation ----------------------------------------------------
//
// Covers the identifier-validation security fix: a publication lands ONLY if
// rec.Identifier fully matches ^gmb-\d{4}-\d+$. An empty identifier and a
// path-traversal identifier that still starts with "gmb-" (proving a
// prefix-only guard, e.g. strings.HasPrefix(id, "gmb-"), is NOT sufficient)
// must both be skipped: no artifact written anywhere, no error from Ingest,
// and a valid record in the same response must still land.

func TestIngest_SkipsInvalidIdentifiers(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	fixture := readFixture(t, "invalid_identifiers.xml")
	var calls []string
	httpGet := fakeSRUGetter(t, &calls, map[string][]byte{"1": fixture})

	err := Ingest(context.Background(), store, httpGet)
	require.NoError(t, err, "Ingest must not error when the response contains invalid-identifier records, as long as they are skipped rather than landed")

	const validName = "koop/gmb-2022-291126.xml"
	landed, err := store.Landed(validName)
	require.NoError(t, err)
	assert.True(t, landed, "the valid record in the same response must still land even though its neighbors have invalid identifiers")

	// The traversal identifier "gmb-../../../../tmp/koop-evil" -- if it were
	// ever handed to RawStore.Land -- resolves (via filepath.Join's ".."
	// collapsing) to a path outside the koop/ prefix, and in fact outside
	// store.BasePath entirely. Assert that exact escape target was never
	// created.
	const evilIdentifier = "gmb-../../../../tmp/koop-evil"
	escapedPath := filepath.Join(store.BasePath, "koop", evilIdentifier+".xml")
	_, statErr := os.Stat(escapedPath)
	assert.True(t, os.IsNotExist(statErr),
		"a path-traversal identifier must never result in a file written at its resolved escape target, got stat err = %v", statErr)

	// Only the valid record (plus its provenance sidecar) and the run's
	// cursor file may exist under BasePath -- nothing for the empty or
	// traversal identifiers, anywhere in the tree.
	files := listLandedFiles(t, store.BasePath)
	assert.ElementsMatch(t, []string{
		validName,
		validName + ".prov.jsonl",
		CursorArtifact,
	}, files, "no artifact may be written under BasePath for the empty or path-traversal identifier records")
}

// -- Malformed dt.available does not poison the cursor ------------------------
//
// Covers the cursor-poisoning-guard fix: a record whose dt.available is not a
// parseable YYYY-MM-DD date must not become the persisted high-water mark.
// The record still lands (its identifier is valid); the persisted
// koop/_cursor.json high-water mark remains a valid YYYY-MM-DD; and a
// subsequent Ingest run completes without error, proving the cursor was not
// poisoned (an unparseable high-water mark would fail at queryLowerBound on
// the very next run).

func TestIngest_MalformedAvailableDoesNotPoisonCursor(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	fixture := readFixture(t, "malformed_available.xml")
	var calls []string
	httpGet := fakeSRUGetter(t, &calls, map[string][]byte{"1": fixture})

	err := Ingest(context.Background(), store, httpGet)
	require.NoError(t, err, "Ingest must not error on a record with a malformed dt.available")

	const name = "koop/gmb-2022-950001.xml"
	landed, err := store.Landed(name)
	require.NoError(t, err)
	assert.True(t, landed, "a record with a valid identifier but malformed dt.available must still land")

	hwm, err := readCursor(store)
	require.NoError(t, err)
	_, parseErr := time.Parse(cursorDateLayout, hwm)
	assert.NoError(t, parseErr,
		"the persisted high-water mark %q must remain a valid YYYY-MM-DD date even though this run's only record had a malformed dt.available", hwm)

	// A second run must complete without error: if the malformed
	// "onbekend" value HAD been persisted as the high-water mark, this run's
	// queryLowerBound(hwm) would fail to time.Parse it and Ingest would
	// return an error.
	httpGet = fakeSRUGetter(t, &calls, map[string][]byte{"1": fixture})
	assert.NoError(t, Ingest(context.Background(), store, httpGet),
		"a second Ingest run must succeed, proving the cursor was not poisoned by the malformed dt.available")
}
