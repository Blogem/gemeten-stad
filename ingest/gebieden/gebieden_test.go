package gebieden

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -- BuildFeatureCollection ----------------------------------------------------

func samplePolygon(x, y float64) map[string]any {
	return map[string]any{
		"type": "Polygon",
		"coordinates": []any{
			[]any{
				[]any{x, y},
				[]any{x + 100, y},
				[]any{x + 100, y + 100},
				[]any{x, y + 100},
				[]any{x, y},
			},
		},
	}
}

func TestBuildFeatureCollection_KeepsOnlyCurrentWithGeometry(t *testing.T) {
	records := []map[string]any{
		{ // current, has geometry -> kept
			"identificatie":  "03630012095714",
			"geometrie":      samplePolygon(121000, 487000),
			"eindGeldigheid": nil,
		},
		{ // superseded (non-null eindGeldigheid) -> dropped even though it has geometry
			"identificatie":  "03630012095715",
			"geometrie":      samplePolygon(122000, 488000),
			"eindGeldigheid": "2020-01-01T00:00:00",
		},
		{ // current but no geometry -> dropped
			"identificatie":  "03630012095716",
			"eindGeldigheid": nil,
		},
	}

	out, err := BuildFeatureCollection(records)
	require.NoError(t, err)
	require.NotEmpty(t, out)

	var fc struct {
		Type     string          `json:"type"`
		CRS      json.RawMessage `json:"crs"`
		Features []struct {
			Type       string         `json:"type"`
			Properties map[string]any `json:"properties"`
			Geometry   map[string]any `json:"geometry"`
		} `json:"features"`
	}
	require.NoError(t, json.Unmarshal(out, &fc))

	assert.Equal(t, "FeatureCollection", fc.Type)
	require.Len(t, fc.Features, 1, "only the current record with geometry should survive")

	got := fc.Features[0]
	assert.Equal(t, "03630012095714", got.Properties["identificatie"],
		"the surviving feature must carry its own identificatie")
	assert.NotEmpty(t, got.Geometry, "the surviving feature must carry its geometry")

	// RD (EPSG:28992) CRS header, either as a top-level "crs" GeoJSON member
	// or embedded elsewhere in the document.
	assert.NotEmpty(t, fc.CRS, "expected a top-level crs member declaring the RD CRS")
	assert.Contains(t, string(out), "28992", "expected the RD SRID (28992) somewhere in the FeatureCollection")
}

func TestBuildFeatureCollection_EmptyInput(t *testing.T) {
	out, err := BuildFeatureCollection(nil)
	require.NoError(t, err)

	var fc struct {
		Type     string `json:"type"`
		Features []any  `json:"features"`
	}
	require.NoError(t, json.Unmarshal(out, &fc))
	assert.Equal(t, "FeatureCollection", fc.Type)
	assert.Empty(t, fc.Features)
}

// -- CBSWFSURL ------------------------------------------------------------------

func TestCBSWFSURL(t *testing.T) {
	raw := CBSWFSURL()
	require.NotEmpty(t, raw)

	u, err := url.Parse(raw)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(u.Scheme, "http"), "expected an absolute http(s) URL, got %q", raw)

	// Build a decoded search space: query values are decoded by url.Query();
	// also unescape the raw string as a fallback for anything outside the query.
	q := u.Query()
	var decodedParts []string
	for k, vs := range q {
		decodedParts = append(decodedParts, k)
		decodedParts = append(decodedParts, vs...)
	}
	unescaped, _ := url.QueryUnescape(raw)
	searchable := strings.Join(decodedParts, " ") + " " + unescaped

	assert.Contains(t, searchable, "gemeentecode", "expected the CBS gemeentecode filter")
	assert.Contains(t, searchable, "GM0363", "expected the filter scoped to gemeente 0363 (Amsterdam)")
	assert.Contains(t, searchable, "wijkenbuurten:buurten", "expected the CBS wijkenbuurten:buurten layer name")
	assert.True(t,
		strings.Contains(searchable, "28992") || strings.Contains(strings.ToUpper(searchable), "EPSG:28992"),
		"expected an RD (EPSG:28992) SRS reference, searchable=%q", searchable)
}

// -- landed-artifact name constants ---------------------------------------------

func TestLandedArtifactNames(t *testing.T) {
	assert.NotEmpty(t, BuurtenName)
	assert.NotEmpty(t, WijkenName)
	assert.NotEmpty(t, CBSName)

	// The three names must be distinct landing-store artifacts.
	assert.NotEqual(t, BuurtenName, WijkenName)
	assert.NotEqual(t, BuurtenName, CBSName)
	assert.NotEqual(t, WijkenName, CBSName)
}
