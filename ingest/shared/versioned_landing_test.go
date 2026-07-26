package shared

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countVersionEntries returns the number of on-disk entries under
// <BasePath>/<name> excluding the provenance.jsonl log, i.e. the number of
// distinct versions actually persisted.
func countVersionEntries(t *testing.T, store *RawStore, name string) int {
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

// readProvenanceLines returns the number of non-empty lines in
// <BasePath>/<name>/provenance.jsonl.
func readProvenanceLines(t *testing.T, store *RawStore, name string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(store.BasePath, name, "provenance.jsonl"))
	require.NoError(t, err)

	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	count := 0
	for _, l := range lines {
		if len(bytes.TrimSpace(l)) == 0 {
			continue
		}
		count++
	}
	return count
}

// TestRawStore_LandVersion_FirstVersion covers acceptance scenario 1: landing
// an artifact for the first time under the versioned scheme creates a version
// keyed by the UTC-formatted fetch time, returns a Provenance describing it,
// and LatestVersion resolves to that same version with the bytes intact.
func TestRawStore_LandVersion_FirstVersion(t *testing.T) {
	store := NewRawStore(t.TempDir())

	const name = "bomen_kapenherplant"
	data1 := []byte("first version payload bytes")
	url1 := "https://api.data.amsterdam.nl/v1/wfs/bomen_kapenherplant/"
	t1 := time.Date(2026, 7, 20, 9, 15, 30, 0, time.UTC)

	prov, err := store.LandVersion(name, bytes.NewReader(data1), url1, t1)
	require.NoError(t, err)

	wantVersion := t1.UTC().Format("20060102T150405Z")
	assert.Equal(t, wantVersion, prov.Version, "Version must be the UTC-formatted fetch time")
	assert.Equal(t, int64(len(data1)), prov.ByteSize)
	assert.NotEmpty(t, prov.ContentSHA256)
	assert.Equal(t, sha256Hex(data1), prov.ContentSHA256)
	assert.Equal(t, url1, prov.SourceURL)

	relPath, ok, err := store.LatestVersion(name)
	require.NoError(t, err)
	require.True(t, ok, "LatestVersion must report ok=true once a version has been landed")
	assert.Equal(t, filepath.Join(name, wantVersion), relPath)

	onDisk, err := os.ReadFile(filepath.Join(store.BasePath, relPath))
	require.NoError(t, err)
	assert.Equal(t, data1, onDisk, "the landed version file must contain the exact bytes, verbatim")
}

// TestRawStore_LandVersion_SecondDistinctVersionKept covers acceptance
// scenario 2: landing genuinely different content at a later timestamp must
// NOT overwrite or remove the first version - both must remain readable on
// disk - and LatestVersion + provenance history must advance to reflect the
// new version.
func TestRawStore_LandVersion_SecondDistinctVersionKept(t *testing.T) {
	store := NewRawStore(t.TempDir())

	const name = "bomen_kapenherplant"
	data1 := []byte("first version payload bytes")
	t1 := time.Date(2026, 7, 20, 9, 15, 30, 0, time.UTC)
	data2 := []byte("second version payload bytes, distinctly different content")
	t2 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)

	prov1, err := store.LandVersion(name, bytes.NewReader(data1), "https://example.org/v1", t1)
	require.NoError(t, err)

	prov2, err := store.LandVersion(name, bytes.NewReader(data2), "https://example.org/v2", t2)
	require.NoError(t, err)

	require.NotEqual(t, prov1.ContentSHA256, prov2.ContentSHA256, "sanity: the two landings must have distinct content digests")
	require.NotEqual(t, prov1.Version, prov2.Version)

	// Both version files must still be present and readable on disk.
	path1 := filepath.Join(store.BasePath, name, prov1.Version)
	body1, err := os.ReadFile(path1)
	require.NoError(t, err, "the first version file must not have been removed or overwritten")
	assert.Equal(t, data1, body1)

	path2 := filepath.Join(store.BasePath, name, prov2.Version)
	body2, err := os.ReadFile(path2)
	require.NoError(t, err)
	assert.Equal(t, data2, body2)

	// LatestVersion must now point at the second (newer) version.
	relPath, ok, err := store.LatestVersion(name)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(name, prov2.Version), relPath)

	// provenance.jsonl must have exactly two records: one per landed version.
	assert.Equal(t, 2, readProvenanceLines(t, store, name))
	assert.Equal(t, 2, countVersionEntries(t, store, name), "both version files must be kept on disk")
}

// TestRawStore_LandVersion_UnchangedContentIsNoOp covers acceptance scenario
// 3: re-landing byte-identical content at a later fetch time must be a no-op
// - no third version directory is created, LatestVersion keeps pointing at
// the newest DISTINCT version, and the provenance log does not grow.
func TestRawStore_LandVersion_UnchangedContentIsNoOp(t *testing.T) {
	store := NewRawStore(t.TempDir())

	const name = "bomen_kapenherplant"
	data1 := []byte("first version payload bytes")
	t1 := time.Date(2026, 7, 20, 9, 15, 30, 0, time.UTC)
	data2 := []byte("second version payload bytes, distinctly different content")
	t2 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 7, 22, 11, 0, 0, 0, time.UTC)

	_, err := store.LandVersion(name, bytes.NewReader(data1), "https://example.org/v1", t1)
	require.NoError(t, err)

	prov2, err := store.LandVersion(name, bytes.NewReader(data2), "https://example.org/v2", t2)
	require.NoError(t, err)

	versionCountBefore := countVersionEntries(t, store, name)
	provLinesBefore := readProvenanceLines(t, store, name)
	require.Equal(t, 2, versionCountBefore)
	require.Equal(t, 2, provLinesBefore)

	// Re-land data1 (identical bytes to the FIRST version, but the latest
	// landed version is data2) - the no-op rule compares against the latest
	// version's digest, and data1 != data2, so per the contract this is NOT
	// a no-op purely by "seen before" but specifically by "== the latest
	// version's" content. Since data1 != latest (data2), this would actually
	// create a third version. To exercise the true no-op path we must re-land
	// content identical to the CURRENT latest (data2).
	provNoop, err := store.LandVersion(name, bytes.NewReader(data2), "https://example.org/v2-refetch", t3)
	require.NoError(t, err)

	// No-op: returns the existing latest Provenance (same version/digest),
	// not a new one stamped at t3.
	assert.Equal(t, prov2.Version, provNoop.Version, "unchanged content must not stamp a new version")
	assert.Equal(t, prov2.ContentSHA256, provNoop.ContentSHA256)

	relPath, ok, err := store.LatestVersion(name)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(name, prov2.Version), relPath, "LatestVersion must still point at the newest DISTINCT version")

	assert.Equal(t, versionCountBefore, countVersionEntries(t, store, name), "on-disk version count must not grow on a no-op re-land")
	assert.Equal(t, provLinesBefore, readProvenanceLines(t, store, name), "provenance record count must not grow on a no-op re-land")
}

// TestRawStore_LandVersion_ContentMatchingOlderVersionIsNotNoOp guards
// against an implementation that dedupes against "any version ever landed"
// instead of specifically "the latest version". Per the pinned semantics,
// the no-op check compares only to the latest version's digest - re-landing
// bytes identical to an OLDER (non-latest) version must create a new
// version, not be treated as unchanged.
func TestRawStore_LandVersion_ContentMatchingOlderVersionIsNotNoOp(t *testing.T) {
	store := NewRawStore(t.TempDir())

	const name = "bomen_kapenherplant"
	data1 := []byte("first version payload bytes")
	t1 := time.Date(2026, 7, 20, 9, 15, 30, 0, time.UTC)
	data2 := []byte("second version payload bytes, distinctly different content")
	t2 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 7, 22, 11, 0, 0, 0, time.UTC)

	prov1, err := store.LandVersion(name, bytes.NewReader(data1), "https://example.org/v1", t1)
	require.NoError(t, err)
	_, err = store.LandVersion(name, bytes.NewReader(data2), "https://example.org/v2", t2)
	require.NoError(t, err)

	// Re-land data1, which matches the FIRST (now-older) version, not the
	// current latest (data2). This must create a third version.
	prov3, err := store.LandVersion(name, bytes.NewReader(data1), "https://example.org/v1-again", t3)
	require.NoError(t, err)

	wantVersion3 := t3.UTC().Format("20060102T150405Z")
	assert.Equal(t, wantVersion3, prov3.Version, "content matching an older, non-latest version must still stamp a fresh version at the new fetch time")
	assert.NotEqual(t, prov1.Version, prov3.Version)

	relPath, ok, err := store.LatestVersion(name)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(name, wantVersion3), relPath)

	assert.Equal(t, 3, countVersionEntries(t, store, name))
	assert.Equal(t, 3, readProvenanceLines(t, store, name))
}

// TestRawStore_LatestVersion_AbsentName covers acceptance scenario 4:
// querying LatestVersion for a name that has never been landed reports
// ok=false and no error.
func TestRawStore_LatestVersion_AbsentName(t *testing.T) {
	store := NewRawStore(t.TempDir())

	relPath, ok, err := store.LatestVersion("never-landed")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, relPath)
}
