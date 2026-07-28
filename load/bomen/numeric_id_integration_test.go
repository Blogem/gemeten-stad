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
	"fmt"
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

// -- soft-deleted felled rows must not be projected into the graph --------------
//
// Regression: queryFelledRows (graph.go) now filters `AND source_deleted_at IS NULL` — before this
// fix, a felled row soft-deleted by upsertAll (its own soft-delete clause on a re-run whose fresh
// export no longer contains that row) was still projected into the graph as a gs:Felling,
// permanently asserting a felling event the source itself no longer vouches for.
//
// This test seeds one live felled row through the real Load path (which also runs the real
// loadFelledGraph projection step as part of Load), then directly stamps source_deleted_at on a
// second felled row inserted straight into the target table — mirroring exactly what upsertAll's
// own soft-delete clause does (load_integration_test.go's "kap-c soft-deleted" scenario) — rather
// than re-running the full Load, which would re-stage that row's still-present landed content and,
// via upsertAll's reappearing-row un-delete clause, undo the soft-delete before the assertion ever
// runs. It then calls the real loadFelledGraph directly to prove the soft-deleted row's gs:Felling
// is never written while the live row's is.
func TestBomenLoad_GraphProjection_ExcludesSoftDeletedRow(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	dsURL := fusekiDatasetURL(t)
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	store := shared.NewRawStore(t.TempDir())

	fetchedAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	liveKapRow := map[string]any{
		"id": "kap-graph-live", "boomId": "stam-graph-live", "boomNieuwId": "", "gbdBuurtId": "A10",
		"dichtstbijzijndeBagAdres": "Softdeletestraat 1", "postcode": "1098ZZ",
		"soortnaam": "Tilia", "toeTePassenBoomsoort": "Tilia",
		"datumVergunningsaanvraag":      "2023-01-01T00:00:00Z",
		"kapmaatregelDatumUitgevoerd":   "2023-02-01T00:00:00Z",
		"plantmaatregelDatumUitgevoerd": "",
	}
	liveStamRow := map[string]any{
		"id": "stam-graph-live", "gbdBuurtId": "A10", "soortnaam": "Tilia", "geometrie": geoJSONPoint(4.93, 52.41),
	}

	seed(t, store, []map[string]any{liveKapRow}, []map[string]any{liveStamRow}, fetchedAt)

	// Initial load: builds the schema, stages/upserts the live row (source_deleted_at NULL), and
	// runs the real loadFelledGraph projection once — the live row's gs:Felling must exist.
	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: true}))

	assert.True(t, sparqlAsk(t, ctx, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <http://gemetenstad.nl/id/felling/kap-graph-live> a gs:Felling } }`),
		"a live (non-soft-deleted) felled row must be projected as a gs:Felling")

	// Directly insert a second felled row into the target table with source_deleted_at already
	// set, bypassing staging/upsert entirely, so the setup can never be undone by a second Load's
	// own un-delete-on-reappear behavior.
	schema, err := loadSchema(ctx, pool)
	require.NoError(t, err)

	softDeletedAt := time.Now().UTC()
	felledOn := time.Date(2023, 3, 1, 0, 0, 0, 0, time.UTC)
	insertSQL := fmt.Sprintf(`INSERT INTO %s (id, "boomId", "kapmaatregelDatumUitgevoerd", source_deleted_at, raw)
		VALUES ($1, $2, $3, $4, $5)`, qualify(schema, kapenherplantTable))
	_, err = pool.Exec(ctx, insertSQL,
		"kap-graph-softdeleted", "stam-graph-softdeleted", felledOn, softDeletedAt, []byte("{}"))
	require.NoError(t, err, "seed a directly soft-deleted felled row")

	// Run the real loadFelledGraph projection step directly (not the full Load, which would
	// re-stage the live row's landed content and, via upsertAll's reappearing-row un-delete clause,
	// have no bearing on the row above anyway since it was never staged — but calling loadFelledGraph
	// directly keeps this test's assertion scoped to exactly the fix under test: queryFelledRows'
	// source_deleted_at filter).
	require.NoError(t, loadFelledGraph(ctx, pool, schema, dsURL))

	assert.False(t, sparqlAsk(t, ctx, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <http://gemetenstad.nl/id/felling/kap-graph-softdeleted> a gs:Felling } }`),
		"a soft-deleted felled row must never be projected as a gs:Felling")
	assert.False(t, sparqlAsk(t, ctx, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <http://gemetenstad.nl/id/tree/stam-graph-softdeleted> a gs:Tree } }`),
		"a soft-deleted felled row's tree must never enter the graph")

	// The live row's felling must remain present after the second projection run too.
	assert.True(t, sparqlAsk(t, ctx, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <http://gemetenstad.nl/id/felling/kap-graph-live> a gs:Felling } }`),
		"the live row's felling must remain projected")
}
