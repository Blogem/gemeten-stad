package dump

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeArtifact(t *testing.T, dir, name string, content []byte) ArtifactDescriptor {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, content, 0o644))

	sha256Hex, size, err := ChecksumFile(path)
	require.NoError(t, err)
	return ArtifactDescriptor{Filename: name, Format: "text/plain", Size: size, SHA256: sha256Hex}
}

func TestWriteReadManifest_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	graphDesc := GraphDescriptor{
		ArtifactDescriptor: writeArtifact(t, dir, GraphFilename, []byte("graph-bytes")),
		NamedGraphs:        []string{"http://ex/run/1"},
		HasDefaultGraph:    true,
	}
	postgisDesc := writeArtifact(t, dir, PostGISFilename, []byte("postgis-bytes"))
	nerCacheDesc := writeArtifact(t, dir, NERCacheFilename, []byte("ner-cache-bytes"))

	want := BuildManifest(createdAt, true, graphDesc, postgisDesc, nerCacheDesc)
	require.NoError(t, WriteManifest(dir, want))

	got, err := ReadManifest(dir)
	require.NoError(t, err)

	assert.Equal(t, formatVersion, got.FormatVersion)
	assert.True(t, createdAt.Equal(got.CreatedAt))
	assert.True(t, got.SkipBAG)
	assert.Equal(t, graphDesc, got.Graph)
	assert.Equal(t, postgisDesc, got.PostGIS)
	assert.Equal(t, nerCacheDesc, got.NERCache)

	assert.NoError(t, ValidateBundle(dir, got))
}

func TestReadManifest_MissingFile(t *testing.T) {
	_, err := ReadManifest(t.TempDir())
	assert.Error(t, err)
}

func TestValidateArtifact_RejectsChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	desc := writeArtifact(t, dir, "artifact.bin", []byte("original content"))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.bin"), []byte("tampered-content"), 0o644))

	err := ValidateArtifact(dir, desc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

func TestValidateArtifact_RejectsSizeMismatch(t *testing.T) {
	dir := t.TempDir()
	desc := writeArtifact(t, dir, "artifact.bin", []byte("original"))
	desc.Size++

	err := ValidateArtifact(dir, desc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "size")
}

func TestValidateArtifact_MissingFile(t *testing.T) {
	dir := t.TempDir()
	err := ValidateArtifact(dir, ArtifactDescriptor{Filename: "does-not-exist.bin"})
	assert.Error(t, err)
}

func TestToolVersion_NeverEmpty(t *testing.T) {
	assert.NotEmpty(t, ToolVersion())
}
