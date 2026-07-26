package geo

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
