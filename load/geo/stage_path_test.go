package geo

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVsizipPath guards the GDAL /vsizip/ path format: the absolute container
// path must keep its leading slash, yielding the double-slash form GDAL needs
// for an absolute archive path. A single slash makes ogr2ogr treat the path as
// relative to the working dir and fail to open the datasource (see the P6
// full-corpus load and Spike D's load.sh).
func TestVsizipPath(t *testing.T) {
	assert.Equal(t, "/vsizip//data/bag/9999OPR08072026.zip",
		vsizipPath("bag/9999OPR08072026.zip"))
}

// TestContainerPath guards the plain (non-vsizip) container path used for the
// landed GeoJSON/CBS files.
func TestContainerPath(t *testing.T) {
	assert.Equal(t, "/data/gebieden_buurten.geojson",
		containerPath("gebieden_buurten.geojson"))
}

// writeOuterZip writes an outer zip at path containing one entry named innerName holding
// innerContent — the "9999<code>*.zip" inner archive extractInnerZip pulls out. Its bytes are
// copied verbatim (never parsed), so arbitrary content stands in for a real inner BAG zip.
func writeOuterZip(t *testing.T, path, innerName string, innerContent []byte) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	zw := zip.NewWriter(f)
	w, err := zw.Create(innerName)
	require.NoError(t, err)
	_, err = w.Write(innerContent)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
}

// TestExtractInnerZip_ExtractsThenIsIdempotent covers extractInnerZip's contract: pull the
// "9999<code>*.zip" inner archive out of the outer BAG extract into destDir, and a second call is a
// no-op returning the same name (the landed-once idempotency stance). Crucially it also asserts the
// atomic temp+rename leaves destDir holding EXACTLY the finished file with no stray temp — so the
// stat-guard's "exists == valid" assumption holds, and a mid-write failure can never leave a
// permanent truncated poison the guard would skip re-extracting.
func TestExtractInnerZip_ExtractsThenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	outer := filepath.Join(dir, "lvbag-extract-nl.zip")
	dest := filepath.Join(dir, "bag")
	innerName := "9999OPR20260101.zip"
	innerContent := []byte("inner-bag-openbareruimte-bytes")
	writeOuterZip(t, outer, innerName, innerContent)

	name, err := extractInnerZip(outer, dest, "OPR")
	require.NoError(t, err)
	assert.Equal(t, innerName, name)

	got, err := os.ReadFile(filepath.Join(dest, innerName))
	require.NoError(t, err)
	assert.Equal(t, innerContent, got, "extracted inner zip must hold the outer entry's bytes verbatim")

	entries, err := os.ReadDir(dest)
	require.NoError(t, err)
	require.Len(t, entries, 1, "atomic rename must leave no temp file behind")
	assert.Equal(t, innerName, entries[0].Name())

	name2, err := extractInnerZip(outer, dest, "OPR")
	require.NoError(t, err)
	assert.Equal(t, innerName, name2, "a second call is an idempotent no-op via the stat-guard")
}
