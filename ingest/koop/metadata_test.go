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

func TestLandMetadata_PersistentTransientErrorIsNonFatalAndUnlanded(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())
	const id = "gmb-2022-291126"

	// Every attempt fails transiently: non-fatal (no error), but the sidecar is NOT landed — so a
	// later run will retry it (it stays unlanded, unlike a genuine 404 which is also unlanded but
	// simply has no sidecar to fetch).
	require.NoError(t, landMetadata(context.Background(), store, flakyGetter(1000, nil), id, time.Now(), true))

	landed, err := store.Landed(metadataArtifactName(id))
	require.NoError(t, err)
	assert.False(t, landed, "a sidecar that never succeeds is left unlanded, not half-written")
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
