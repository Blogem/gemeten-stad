//go:build integration

// This file is a regression test for the load/bomen numeric-id staging fix (read.go/stage.go): a
// landed kapenherplant/stamgegevens id/boomId arrives as a JSON *number* (docs/DATA_SOURCES.md
// §2a — the Datapunt bomen DSO API emits these unquoted), which used to decode through float64
// and render in scientific notation ("4.301428e+06") when staged into the text id column, instead
// of the exact integer string ("4301189"). This test drives the real Load path (read -> stage ->
// upsert) end to end against an isolated internal/testdb schema and asserts the persisted id
// columns are exact, scientific-notation-free integer strings — mirroring
// load_integration_test.go's harness (requireEnv, newSchemaPool, landPage,
// landStamgegevensGeoJSON, fusekiDatasetURL).
package bomen

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/Blogem/gemeten-stad/ingest/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainIntegerID matches a bare, all-digit integer string — never scientific notation
// ("4.301428e+06") or any other float rendering.
var plainIntegerID = regexp.MustCompile(`^[0-9]+$`)

// TestBomenLoad_NumericIDsStageExact lands a kapenherplant/stamgegevens fixture whose ids are raw
// JSON numbers (not quoted strings) — the Datapunt bomen DSO API's own shape — runs the real Load
// path, and asserts every persisted id column is an exact, all-digit integer string.
func TestBomenLoad_NumericIDsStageExact(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	dsURL := fusekiDatasetURL(t)
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	store := shared.NewRawStore(t.TempDir())

	fetchedAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Go int values here marshal to raw JSON numbers (landPage/landStamgegevensGeoJSON both
	// json.Marshal the row maps), exactly mirroring the live Datapunt bomen API's unquoted
	// id/boomId fields — the shape that triggered the float64-coercion bug.
	kapRows := []map[string]any{
		{
			"id": 4301189, "boomId": 1014499, "boomNieuwId": 2041042, "gbdBuurtId": "A01",
			"dichtstbijzijndeBagAdres": "Teststraat 1", "postcode": "1011AB",
			"soortnaam": "Tilia", "toeTePassenBoomsoort": "Tilia",
			"datumVergunningsaanvraag":      "2023-01-01T00:00:00Z",
			"kapmaatregelDatumUitgevoerd":   "2023-02-01T00:00:00Z",
			"plantmaatregelDatumUitgevoerd": "",
		},
	}
	stamRows := []map[string]any{
		{"id": 1014499, "gbdBuurtId": "A01", "soortnaam": "Tilia", "geometrie": geoJSONPoint(4.895, 52.370)},
	}

	landPage(t, store, artifactKapenherplant, embedKeyKapenherplant, kapRows, fetchedAt)
	landStamgegevensGeoJSON(t, store, artifactStamgegevens, stamRows, fetchedAt)

	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: true}))

	var kapID string
	require.NoError(t, pool.QueryRow(ctx, "SELECT id FROM "+kapenherplantTable+" LIMIT 1").Scan(&kapID))
	assert.Regexp(t, plainIntegerID, kapID, "kapenherplant.id must be an exact integer string, never scientific notation")
	assert.Equal(t, "4301189", kapID, "kapenherplant.id must equal the source's exact decimal text")

	var stamID string
	require.NoError(t, pool.QueryRow(ctx, "SELECT id FROM "+stamgegevensTable+" LIMIT 1").Scan(&stamID))
	assert.Regexp(t, plainIntegerID, stamID, "stamgegevens.id must be an exact integer string, never scientific notation")
	assert.Equal(t, "1014499", stamID, "stamgegevens.id must equal the source's exact decimal text")
}
