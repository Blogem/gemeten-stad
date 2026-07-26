package koop

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// readFixture loads a recorded SRU 2.0 searchRetrieve response fixture from
// ingest/koop/testdata.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return data
}

// fakeSRUGetter builds a shared.HTTPGetFunc-shaped fake that dispatches
// recorded page fixtures by the startRecord query parameter of the requested
// URL -- mirroring ingest/shared's own SRU test helper (fakeSRUHTTPGet) --
// and appends every requested URL (in order) to *calls, so cursor/overlap/
// query-shape assertions can inspect exactly what the harvester asked for
// across one or more Ingest calls sharing the same *calls slice.
//
// It does NOT filter by the query clause itself: the fixtures are dispatched
// purely by startRecord, same as a real single/multi-page result would be,
// letting tests assert on the *sent* query independently of what gets
// "served" back.
func fakeSRUGetter(t *testing.T, calls *[]string, pagesByStart map[string][]byte) shared.HTTPGetFunc {
	t.Helper()
	return func(_ context.Context, rawURL string) (io.ReadCloser, error) {
		*calls = append(*calls, rawURL)

		u, err := url.Parse(rawURL)
		require.NoError(t, err)
		start := u.Query().Get("startRecord")

		page, ok := pagesByStart[start]
		if !ok {
			return nil, fmt.Errorf("fakeSRUGetter: unexpected startRecord %q in URL %q", start, rawURL)
		}
		return io.NopCloser(bytes.NewReader(page)), nil
	}
}

// queryParam url-decodes the `query` parameter out of a captured SRU request
// URL, for asserting on the actual CQL clause the harvester sent (e.g. the
// dt.available>= lower bound used on a given run).
func queryParam(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	return u.Query().Get("query")
}

// listLandedFiles returns the sorted-by-walk-order set of file paths (relative
// to base) under a raw store's base path, for snapshotting "no new artifact
// was written" across a re-run.
func listLandedFiles(t *testing.T, base string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	require.NoError(t, err)
	return files
}

// provenanceLineCount reports how many provenance lines exist for a landed
// artifact name (0 if it has never been landed), for asserting that a no-op
// re-run truly appends nothing.
func provenanceLineCount(t *testing.T, store *shared.RawStore, name string) int {
	t.Helper()
	history, err := store.ProvenanceHistory(name)
	require.NoError(t, err)
	return len(history)
}
