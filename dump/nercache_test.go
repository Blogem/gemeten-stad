package dump

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCaptureNERCache_AbsentDirectoryYieldsValidEmptyArchive(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "does-not-exist")

	var buf bytes.Buffer
	require.NoError(t, CaptureNERCache(cachePath, &buf))
	assert.NotZero(t, buf.Len(), "even an empty archive has gzip+tar terminator bytes")

	restoreDir := filepath.Join(dir, "restored")
	require.NoError(t, RestoreNERCache(&buf, restoreDir))

	entries, err := os.ReadDir(restoreDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestCaptureAndRestoreNERCache_RoundTripsFiles(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "ner-cache")
	require.NoError(t, os.MkdirAll(filepath.Join(cachePath, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cachePath, "doc1__modelA"), []byte("entry one"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cachePath, "sub", "doc2__modelB"), []byte("entry two"), 0o644))

	var buf bytes.Buffer
	require.NoError(t, CaptureNERCache(cachePath, &buf))

	restorePath := filepath.Join(dir, "restored-cache")
	require.NoError(t, RestoreNERCache(&buf, restorePath))

	got1, err := os.ReadFile(filepath.Join(restorePath, "doc1__modelA"))
	require.NoError(t, err)
	assert.Equal(t, "entry one", string(got1))

	got2, err := os.ReadFile(filepath.Join(restorePath, "sub", "doc2__modelB"))
	require.NoError(t, err)
	assert.Equal(t, "entry two", string(got2))
}

func TestRestoreNERCache_IsAdditive(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "ner-cache")
	require.NoError(t, os.MkdirAll(cachePath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cachePath, "new-entry"), []byte("new"), 0o644))

	var buf bytes.Buffer
	require.NoError(t, CaptureNERCache(cachePath, &buf))

	target := filepath.Join(dir, "target-cache")
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "pre-existing-entry"), []byte("keep me"), 0o644))

	require.NoError(t, RestoreNERCache(&buf, target))

	got, err := os.ReadFile(filepath.Join(target, "pre-existing-entry"))
	require.NoError(t, err)
	assert.Equal(t, "keep me", string(got))

	got, err = os.ReadFile(filepath.Join(target, "new-entry"))
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
}

func TestRestoreNERCache_RejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target-cache")

	var buf bytes.Buffer
	require.NoError(t, CaptureNERCache(filepath.Join(dir, "empty-source"), &buf))

	// A crafted archive entry escaping the target dir must be rejected outright.
	err := extractNERCacheEntry(bytes.NewReader(nil), target, "../escape.txt")
	assert.Error(t, err)
}
