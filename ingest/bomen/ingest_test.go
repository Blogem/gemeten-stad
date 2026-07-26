package bomen

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// -- 1. firstPageURL ---------------------------------------------------------

// TestFirstPageURL covers the pinned firstPageURL contract: it appends the Datapunt paging
// params (_pageSize, page=1) to baseURL, and must never smuggle in the `[isnull]` filter
// (DATA_SOURCES.md: silently returns an empty response, no error) or a `geometrie` spatial
// filter (kapenherplant rejects it with HTTP 403 — only stamgegevens accepts it, and Ingest
// does not use it for either endpoint).
func TestFirstPageURL(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		pageSize int
		want     string
	}{
		{
			name:     "trailing slash base URL, default-shaped page size",
			baseURL:  "https://x/y/",
			pageSize: 1000,
			want:     "https://x/y/?_pageSize=1000&page=1",
		},
		{
			name:     "DefaultPageSize constant",
			baseURL:  "https://api.data.amsterdam.nl/v1/bomen/kapenherplant/",
			pageSize: DefaultPageSize,
			want:     "https://api.data.amsterdam.nl/v1/bomen/kapenherplant/?_pageSize=1000&page=1",
		},
		{
			name:     "smaller page size",
			baseURL:  "https://x/y/",
			pageSize: 50,
			want:     "https://x/y/?_pageSize=50&page=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstPageURL(tt.baseURL, tt.pageSize)
			assert.Equal(t, tt.want, got)
			assert.NotContains(t, got, "[isnull]", "the [isnull] filter operator is a known dead end (empty response, no error)")
			assert.NotContains(t, got, "geometrie", "kapenherplant rejects geometrie[within] with HTTP 403; firstPageURL must not add a spatial filter")
		})
	}
}

// TestDefaultPageSize pins the constant's value: it is part of the public contract other
// callers (and this test file) rely on.
func TestDefaultPageSize(t *testing.T) {
	assert.Equal(t, 1000, DefaultPageSize)
}

// -- 2. Ingest ----------------------------------------------------------------

// Fake page bodies. kapenherplant is served across two HAL-style pages: page 1 carries
// _links.next.href pointing at page 2, which itself has no next link and so ends paging.
// stamgegevens is served as a single page (no next link at all).
const (
	kapenherplantPage1 = `{"_embedded":{"kapenherplant":[{"boomId":"1"},{"boomId":"2"}]},"_links":{"next":{"href":"https://fake.example/bomen/kapenherplant-next-page"}}}`
	kapenherplantPage2 = `{"_embedded":{"kapenherplant":[{"boomId":"3"}]}}`
	stamgegevensPage1  = `{"_embedded":{"stamgegevens":[{"id":"boom-1"}]}}`

	kapenherplantNextURL = "https://fake.example/bomen/kapenherplant-next-page"
)

// fakePagedHTTPGet dispatches by URL content rather than exact match for first-page requests
// (the real base URLs are an ingest/bomen implementation detail, not pinned), but matches the
// next-page URL exactly once it has been handed out via _links.next.href — mirroring how a
// real caller would follow the HAL link verbatim.
func fakePagedHTTPGet(calls *[]string) func(ctx context.Context, url string) (io.ReadCloser, error) {
	return func(_ context.Context, url string) (io.ReadCloser, error) {
		*calls = append(*calls, url)
		switch {
		case url == kapenherplantNextURL:
			return io.NopCloser(strings.NewReader(kapenherplantPage2)), nil
		case strings.Contains(url, "kapenherplant"):
			return io.NopCloser(strings.NewReader(kapenherplantPage1)), nil
		case strings.Contains(url, "stamgegevens"):
			return io.NopCloser(strings.NewReader(stamgegevensPage1)), nil
		default:
			return nil, fmt.Errorf("unexpected URL %q", url)
		}
	}
}

// countVersionEntries returns the number of on-disk version files under <BasePath>/<name>,
// excluding the provenance.jsonl log — the number of distinct versions actually persisted by
// LandVersion.
func countVersionEntries(t *testing.T, store *shared.RawStore, name string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(store.BasePath, name))
	require.NoError(t, err)

	count := 0
	for _, e := range entries {
		if e.Name() == "provenance.jsonl" {
			continue
		}
		count++
	}
	return count
}

// readLandedJSONLLines reads the latest landed version of name and splits it into non-empty
// lines, mirroring how load/bomen's readLandedRows will later decode it page by page.
func readLandedJSONLLines(t *testing.T, store *shared.RawStore, name string) []string {
	t.Helper()
	relPath, ok, err := store.LatestVersion(name)
	require.NoError(t, err)
	require.True(t, ok, "expected %q to have a landed version", name)

	data, err := os.ReadFile(filepath.Join(store.BasePath, relPath))
	require.NoError(t, err)

	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

// TestIngest_LandsBothArtifactsAcrossPages covers the paging + landing happy path: a two-page
// kapenherplant harvest (following _links.next.href) and a one-page stamgegevens harvest both
// land as versioned artifacts, with one JSONL line per fetched page, each line the exact raw
// page body verbatim (no re-encoding).
func TestIngest_LandsBothArtifactsAcrossPages(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())
	var calls []string

	err := Ingest(context.Background(), store, fakePagedHTTPGet(&calls))
	require.NoError(t, err)

	kapenherplantLanded, ok, err := store.LatestVersion(ArtifactKapenherplant)
	require.NoError(t, err)
	require.True(t, ok, "kapenherplant must be landed after Ingest")
	assert.NotEmpty(t, kapenherplantLanded)

	stamgegevensLanded, ok, err := store.LatestVersion(ArtifactStamgegevens)
	require.NoError(t, err)
	require.True(t, ok, "stamgegevens must be landed after Ingest")
	assert.NotEmpty(t, stamgegevensLanded)

	kapenherplantLines := readLandedJSONLLines(t, store, ArtifactKapenherplant)
	require.Len(t, kapenherplantLines, 2, "one JSONL line per fetched kapenherplant page")
	assert.Equal(t, kapenherplantPage1, kapenherplantLines[0], "page 1 must be landed verbatim, not re-encoded")
	assert.Equal(t, kapenherplantPage2, kapenherplantLines[1], "page 2 must be landed verbatim, not re-encoded")

	stamgegevensLines := readLandedJSONLLines(t, store, ArtifactStamgegevens)
	require.Len(t, stamgegevensLines, 1, "one JSONL line for the single fetched stamgegevens page")
	assert.Equal(t, stamgegevensPage1, stamgegevensLines[0], "the page must be landed verbatim, not re-encoded")

	assert.Contains(t, calls, kapenherplantNextURL, "Ingest must follow _links.next.href to fetch page 2")
}

// TestIngest_RerunWithIdenticalContentIsNoOp covers the versioned-landing idempotency contract:
// re-running Ingest against a source that returns byte-identical pages must not create a new
// version — LandVersion's content-hash dedup makes this a no-op.
func TestIngest_RerunWithIdenticalContentIsNoOp(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	var firstCalls []string
	err := Ingest(context.Background(), store, fakePagedHTTPGet(&firstCalls))
	require.NoError(t, err)

	kapenherplantVersionsBefore := countVersionEntries(t, store, ArtifactKapenherplant)
	stamgegevensVersionsBefore := countVersionEntries(t, store, ArtifactStamgegevens)
	require.Equal(t, 1, kapenherplantVersionsBefore)
	require.Equal(t, 1, stamgegevensVersionsBefore)

	var secondCalls []string
	err = Ingest(context.Background(), store, fakePagedHTTPGet(&secondCalls))
	require.NoError(t, err)

	assert.Equal(t, kapenherplantVersionsBefore, countVersionEntries(t, store, ArtifactKapenherplant),
		"identical re-fetched content must not create a new kapenherplant version")
	assert.Equal(t, stamgegevensVersionsBefore, countVersionEntries(t, store, ArtifactStamgegevens),
		"identical re-fetched content must not create a new stamgegevens version")
}

// TestArtifactNames pins the two landing artifact names used throughout Ingest and
// load/bomen's readLandedRows.
func TestArtifactNames(t *testing.T) {
	assert.Equal(t, "bomen_kapenherplant", ArtifactKapenherplant)
	assert.Equal(t, "bomen_stamgegevens", ArtifactStamgegevens)
}
