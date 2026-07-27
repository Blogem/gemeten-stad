package koop

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// -- URL + naming convention (task 2.1) ---------------------------------------

func TestMetadataURLAndArtifactName(t *testing.T) {
	const id = "gmb-2022-291126"
	assert.Equal(t, "https://zoek.officielebekendmakingen.nl/gmb-2022-291126/metadata.xml", metadataURL(id))
	assert.Equal(t, "koop/gmb-2022-291126.metadata.xml", metadataArtifactName(id))
	// The sidecar name must be distinct from the SRU record's, so the load stage can tell the two
	// verbatim artifacts apart and never mistake a sidecar for a record.
	assert.NotEqual(t, "koop/"+id+".xml", metadataArtifactName(id))
}

// TestMain neutralises the pacing/backoff sleeps so the retry tests run instantly.
func TestMain(m *testing.M) {
	metadataSleep = func(time.Duration) {}
	os.Exit(m.Run())
}

// fakeMetadataGetter serves body for any metadata.xml URL and records every requested URL. When
// body is nil it returns a definitive 404 (shared.ErrNotFound — a genuinely absent sidecar, not
// retried). A non-metadata URL is an unexpected call and fails the test.
func fakeMetadataGetter(t *testing.T, calls *[]string, body []byte) shared.HTTPGetFunc {
	t.Helper()
	return func(_ context.Context, rawURL string) (io.ReadCloser, error) {
		*calls = append(*calls, rawURL)
		require.Contains(t, rawURL, "/metadata.xml", "only metadata sidecar fetches are expected here")
		if body == nil {
			return nil, fmt.Errorf("get %s: %w", rawURL, shared.ErrNotFound)
		}
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}

// -- Lands the sidecar verbatim with provenance (task 2.2) ---------------------

func TestLandMetadata_LandsVerbatimWithProvenance(t *testing.T) {
	fixture := readFixture(t, "metadata_noord.xml")
	store := shared.NewRawStore(t.TempDir())
	const id = "gmb-2022-291126"
	name := metadataArtifactName(id)

	var calls []string
	getter := fakeMetadataGetter(t, &calls, fixture)
	require.NoError(t, landMetadata(context.Background(), store, getter, id, time.Now(), true))

	got, err := os.ReadFile(filepath.Join(store.BasePath, name))
	require.NoError(t, err)
	assert.Equal(t, string(fixture), string(got), "the sidecar must be landed verbatim")

	require.Len(t, calls, 1, "the sidecar must be fetched exactly once")
	assert.Equal(t, 1, provenanceLineCount(t, store, name), "a landed sidecar must have one provenance line")
}

// -- Non-fatal on a missing/unavailable sidecar (task 2.2) ---------------------

func TestLandMetadata_MissingSidecarIsNonFatal(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())
	const id = "gmb-2019-100000"
	name := metadataArtifactName(id)

	var calls []string
	getter := fakeMetadataGetter(t, &calls, nil) // 404
	// The SRU record is assumed landed by landRecord; a 404 sidecar must not fail the harvest.
	require.NoError(t, landMetadata(context.Background(), store, getter, id, time.Now(), true))

	landed, err := store.Landed(name)
	require.NoError(t, err)
	assert.False(t, landed, "no sidecar artifact is written when the source returns 404")
	require.Len(t, calls, 1, "the fetch was attempted once")
}

func TestLandMetadata_EmptySidecarIsNonFatal(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())
	const id = "gmb-2022-291126"

	var calls []string
	getter := fakeMetadataGetter(t, &calls, []byte{}) // empty body
	require.NoError(t, landMetadata(context.Background(), store, getter, id, time.Now(), true))

	landed, err := store.Landed(metadataArtifactName(id))
	require.NoError(t, err)
	assert.False(t, landed, "an empty sidecar body is not landed")
}

// -- Pacing is env-configurable (design D5) -----------------------------------

func TestMetadataRateInterval(t *testing.T) {
	t.Setenv("GS_KOOP_METADATA_RATE_MS", "")
	assert.Equal(t, 200*time.Millisecond, metadataRateInterval(), "default is 200ms")

	t.Setenv("GS_KOOP_METADATA_RATE_MS", "500")
	assert.Equal(t, 500*time.Millisecond, metadataRateInterval(), "override honoured")

	t.Setenv("GS_KOOP_METADATA_RATE_MS", "0")
	assert.Equal(t, time.Duration(0), metadataRateInterval(), "0 disables pacing")

	t.Setenv("GS_KOOP_METADATA_RATE_MS", "-5")
	assert.Equal(t, 200*time.Millisecond, metadataRateInterval(), "negative falls back to default")

	t.Setenv("GS_KOOP_METADATA_RATE_MS", "notanumber")
	assert.Equal(t, 200*time.Millisecond, metadataRateInterval(), "non-numeric falls back to default")
}

// -- Transient errors are retried, not mistaken for absence (task 2.2) ---------

// flakyGetter fails the first failN calls with a transient (non-404) error, then serves body.
func flakyGetter(failN int, body []byte) shared.HTTPGetFunc {
	var n int
	return func(_ context.Context, rawURL string) (io.ReadCloser, error) {
		n++
		if n <= failN {
			return nil, fmt.Errorf("get %s: EOF", rawURL) // a transient reset, NOT shared.ErrNotFound
		}
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}

func TestLandMetadata_RetriesTransientErrorThenLands(t *testing.T) {
	fixture := readFixture(t, "metadata_noord.xml")
	store := shared.NewRawStore(t.TempDir())
	const id = "gmb-2022-291126"

	// Two connection resets, then success — must land, not be dropped as "absent".
	require.NoError(t, landMetadata(context.Background(), store, flakyGetter(2, fixture), id, time.Now(), true))

	got, err := os.ReadFile(filepath.Join(store.BasePath, metadataArtifactName(id)))
	require.NoError(t, err)
	assert.Equal(t, string(fixture), string(got), "a sidecar that succeeds after retries must be landed")
}

func TestLandMetadata_PersistentTransientErrorSignalsExhaustion(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())
	const id = "gmb-2022-291126"

	// Every attempt fails transiently: the whole backoff chain never recovers, so landMetadata
	// signals errMetadataExhausted (for the harvest circuit breaker) and leaves the sidecar
	// UNLANDED — so a later run retries it (unlike a genuine 404, which is a definitive nil skip).
	err := landMetadata(context.Background(), store, flakyGetter(1000, nil), id, time.Now(), true)
	require.ErrorIs(t, err, errMetadataExhausted)

	landed, err := store.Landed(metadataArtifactName(id))
	require.NoError(t, err)
	assert.False(t, landed, "a sidecar that never succeeds is left unlanded, not half-written")
}

// -- Circuit breaker: abort the harvest when the host is clearly blocking ------

func TestMetadataMaxConsecutiveFails(t *testing.T) {
	t.Setenv("GS_KOOP_METADATA_MAX_CONSECUTIVE_FAILS", "")
	assert.Equal(t, 2, metadataMaxConsecutiveFails(), "default threshold is 2")
	t.Setenv("GS_KOOP_METADATA_MAX_CONSECUTIVE_FAILS", "5")
	assert.Equal(t, 5, metadataMaxConsecutiveFails(), "override honoured")
	t.Setenv("GS_KOOP_METADATA_MAX_CONSECUTIVE_FAILS", "0")
	assert.Equal(t, 0, metadataMaxConsecutiveFails(), "0 disables the breaker")
	t.Setenv("GS_KOOP_METADATA_MAX_CONSECUTIVE_FAILS", "bad")
	assert.Equal(t, 2, metadataMaxConsecutiveFails(), "invalid falls back to default")
}

func TestIngest_CircuitBreakerAbortsWhenHostBlocks(t *testing.T) {
	// SRU pages return records fine; every metadata fetch fails transiently (a block). The harvest
	// must abort once metadataMaxConsecutiveFails (default 2) chains exhaust in a row, rather than
	// grinding through the whole corpus.
	sru := readFixture(t, "paging_page1.xml") // multiple records on one page
	_, recs, err := shared.ParseSRUResponse(sru)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(recs), 2, "fixture must carry >=2 records to trip the breaker")

	store := shared.NewRawStore(t.TempDir())
	getter := func(_ context.Context, rawURL string) (io.ReadCloser, error) {
		if bytes.HasSuffix([]byte(rawURL), []byte("/metadata.xml")) {
			return nil, fmt.Errorf("get %s: EOF", rawURL) // always a transient reset
		}
		return io.NopCloser(bytes.NewReader(sru)), nil
	}

	err = Ingest(context.Background(), store, getter)
	require.Error(t, err, "the harvest must abort when the metadata host keeps blocking")
	assert.Contains(t, err.Error(), "aborting harvest", "the error should explain the circuit breaker tripped")

	// Cursor must NOT have advanced (so a resume re-queries), and no sidecar was written.
	assert.Equal(t, 0, len(mustGlob(t, store.BasePath, "koop/*.metadata.xml")), "no sidecar landed under a block")
}

// mustGlob returns matches of pattern under base (relative), for asserting landed-file sets.
func mustGlob(t *testing.T, base, pattern string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(base, pattern))
	require.NoError(t, err)
	return m
}

// -- Idempotent / gated on need (task 2.2) ------------------------------------

func TestLandMetadata_SkipsFetchWhenLandedAndRecordUnchanged(t *testing.T) {
	fixture := readFixture(t, "metadata_noord.xml")
	store := shared.NewRawStore(t.TempDir())
	const id = "gmb-2022-291126"
	name := metadataArtifactName(id)

	var calls []string
	getter := fakeMetadataGetter(t, &calls, fixture)

	// First run: record changed -> fetch + land.
	require.NoError(t, landMetadata(context.Background(), store, getter, id, time.Now(), true))
	require.Len(t, calls, 1)

	// Second run: sidecar already landed AND the SRU record was unchanged -> no fetch at all (D2).
	require.NoError(t, landMetadata(context.Background(), store, getter, id, time.Now(), false))
	assert.Len(t, calls, 1, "an unchanged re-run must not re-fetch an already-landed sidecar")
	assert.Equal(t, 1, provenanceLineCount(t, store, name), "no new provenance line on a no-op re-run")
}

func TestLandMetadata_RetriesWhenNotYetLanded(t *testing.T) {
	fixture := readFixture(t, "metadata_noord.xml")
	store := shared.NewRawStore(t.TempDir())
	const id = "gmb-2022-291126"

	// A prior run failed to land the sidecar (404). A later run finds it available and lands it,
	// even though the SRU record itself is unchanged (recordChanged=false) — because the sidecar
	// is not yet present.
	var calls []string
	require.NoError(t, landMetadata(context.Background(), store, fakeMetadataGetter(t, &calls, nil), id, time.Now(), false))
	landed, err := store.Landed(metadataArtifactName(id))
	require.NoError(t, err)
	require.False(t, landed)

	require.NoError(t, landMetadata(context.Background(), store, fakeMetadataGetter(t, &calls, fixture), id, time.Now(), false))
	landed, err = store.Landed(metadataArtifactName(id))
	require.NoError(t, err)
	assert.True(t, landed, "a missing sidecar is retried and landed on a later run")
}

func TestLandMetadata_ReLandsWhenContentChanged(t *testing.T) {
	original := readFixture(t, "metadata_noord.xml")
	changed := append([]byte(nil), original...)
	changed = bytes.Replace(changed, []byte("Z2022-N002608"), []byte("Z2022-N009999"), 1)
	require.NotEqual(t, string(original), string(changed))

	store := shared.NewRawStore(t.TempDir())
	const id = "gmb-2022-291126"
	name := metadataArtifactName(id)

	var calls []string
	require.NoError(t, landMetadata(context.Background(), store, fakeMetadataGetter(t, &calls, original), id, time.Now(), true))
	// Record changed again; the sidecar content also changed -> re-land + append provenance.
	require.NoError(t, landMetadata(context.Background(), store, fakeMetadataGetter(t, &calls, changed), id, time.Now(), true))

	got, err := os.ReadFile(filepath.Join(store.BasePath, name))
	require.NoError(t, err)
	assert.Equal(t, string(changed), string(got))
	assert.Equal(t, 2, provenanceLineCount(t, store, name), "a changed sidecar appends a second provenance line")
}

// -- End to end through Ingest: the sidecar lands alongside the record ---------

func TestIngest_LandsMetadataSidecarAlongsideRecord(t *testing.T) {
	sru := readFixture(t, "single_publication.xml") // gmb-2022-900001
	meta := readFixture(t, "metadata_noord.xml")
	store := shared.NewRawStore(t.TempDir())

	// Combined getter: SRU pages by startRecord, metadata by URL suffix.
	getter := func(_ context.Context, rawURL string) (io.ReadCloser, error) {
		if bytes.HasSuffix([]byte(rawURL), []byte("/metadata.xml")) {
			return io.NopCloser(bytes.NewReader(meta)), nil
		}
		return io.NopCloser(bytes.NewReader(sru)), nil
	}

	require.NoError(t, Ingest(context.Background(), store, getter))

	metaName := metadataArtifactName("gmb-2022-900001")
	got, err := os.ReadFile(filepath.Join(store.BasePath, metaName))
	require.NoError(t, err)
	assert.Equal(t, string(meta), string(got), "the metadata sidecar lands verbatim through Ingest")

	// The SRU record still lands as before.
	landed, err := store.Landed("koop/gmb-2022-900001.xml")
	require.NoError(t, err)
	assert.True(t, landed)

	// A second, unchanged run re-fetches nothing (record unchanged + sidecar present): no new
	// provenance line on either artifact.
	require.NoError(t, Ingest(context.Background(), store, getter))
	assert.Equal(t, 1, provenanceLineCount(t, store, metaName), "unchanged re-run appends no sidecar provenance")
	assert.Equal(t, 1, provenanceLineCount(t, store, "koop/gmb-2022-900001.xml"))
}

// -- Defensive: an invalid identifier never reaches URL/path construction ------

func TestLandMetadata_SkipsInvalidIdentifier(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())
	getter := func(_ context.Context, rawURL string) (io.ReadCloser, error) {
		t.Fatalf("httpGet must not be called for an invalid identifier (got %q)", rawURL)
		return nil, nil
	}
	require.NoError(t, landMetadata(context.Background(), store, getter, "gmb-../../etc/passwd", time.Now(), true))
}
