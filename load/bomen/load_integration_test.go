//go:build integration

// This file drives the REAL load/bomen backbone end to end (Load's own
// read -> stage -> upsert -> join call order, load.go) against a real
// Postgres+PostGIS, in an isolated internal/testdb schema — mirroring
// load/geo/load_integration_test.go's harness shape (requireEnv,
// testdb.NewSchemaName/CreateSchema/DropSchema, a pgxpool with AfterConnect
// setting search_path to <schema>,public so both the unqualified table names
// load.go/upsert.go/join.go use and the postgis functions in public resolve).
//
// Unlike load/geo (whose staging is populated by an ogr2ogr sidecar this repo
// has no bomen counterpart for), seeding here goes through the REAL
// ingest/shared.RawStore.LandVersion landing seam: fixtures are landed as
// DSO-shaped page bodies (`{"_embedded":{"kapenherplant":[...]}}` /
// `{"_embedded":{"stamgegevens":[...]}}`), and Load's own readLandedRows
// reads them back — so this test exercises the actual bronze->silver seam,
// not just the DB half of it.
package bomen

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Blogem/gemeten-stad/ingest/shared"
	"github.com/Blogem/gemeten-stad/internal/testdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireEnv fails the test loudly (never skips) if name is unset, matching the P5 convention in
// internal/testdb/postgres_test.go and load/geo's/location's own integration tests.
func requireEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	require.NotEmptyf(t, v, "%s must be set to run integration tests (see README.md)", name)
	return v
}

// newSchemaPool creates a fresh internal/testdb schema (dropped on cleanup) and returns a pool
// whose every connection has search_path set to <schema>,public. load.go/upsert.go/join.go all
// use UNQUALIFIED table names, and PostGIS functions (ST_GeomFromGeoJSON, ST_X, ST_Y, ...) live in
// public, so both must be on the path.
func newSchemaPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()

	schema, err := testdb.NewSchemaName()
	require.NoError(t, err)
	require.NoError(t, testdb.CreateSchema(ctx, dsn, schema))
	t.Cleanup(func() {
		assert.NoError(t, testdb.DropSchema(ctx, dsn, schema), "DropSchema cleanup")
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path = "+pgx.Identifier{schema}.Sanitize()+", public")
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, pool.Ping(ctx))
	require.NoError(t, testdb.AssertIsolatedSchema(ctx, pool, schema),
		"harness guard: pool's search_path must resolve current_schema() to the isolated test schema")
	return pool
}

// geoJSONPoint returns a GeoJSON Point object shaped exactly like the Datapunt bomen API's
// `geometrie` field ([lon, lat] in EPSG:4326).
func geoJSONPoint(lon, lat float64) map[string]any {
	return map[string]any{"type": "Point", "coordinates": []float64{lon, lat}}
}

// landPage marshals rows under embedKey inside a single DSO-shaped page body and lands it as a
// new version of artifact via the real shared.RawStore.LandVersion — the actual seam Load's
// readLandedRows reads back through (read.go), not a shortcut around it. Landing identical
// content twice is intentionally content-hash idempotent (LandVersion's own dedup), which the
// "re-run is a no-op" scenario below relies on. kapenherplant still ingests via this paged _embedded
// shape.
func landPage(t *testing.T, store *shared.RawStore, artifact, embedKey string, rows []map[string]any, fetchedAt time.Time) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"_embedded": map[string]any{embedKey: rows}})
	require.NoError(t, err)
	_, err = store.LandVersion(artifact, strings.NewReader(string(body)+"\n"), "https://example.com/"+artifact, fetchedAt)
	require.NoError(t, err)
}

// landStamgegevensGeoJSON marshals rows into a GeoJSON FeatureCollection body — feature.properties
// holding every row field except `geometrie`, feature.geometry holding the row's own `geometrie`
// value — and lands it as a new version of artifact via the real shared.RawStore.LandVersion,
// mirroring stamgegevens' new DSO geojson-export ingest shape (readLandedRows auto-detects the
// "features" key and unpacks each feature back into properties + {"geometrie": geometry}, see
// read.go). Landing identical content twice is content-hash idempotent, same as landPage.
func landStamgegevensGeoJSON(t *testing.T, store *shared.RawStore, artifact string, rows []map[string]any, fetchedAt time.Time) {
	t.Helper()

	features := make([]map[string]any, len(rows))
	for i, row := range rows {
		props := make(map[string]any, len(row))
		for k, v := range row {
			if k == "geometrie" {
				continue
			}
			props[k] = v
		}
		features[i] = map[string]any{
			"type":       "Feature",
			"id":         row["id"],
			"geometry":   row["geometrie"],
			"properties": props,
		}
	}

	body, err := json.Marshal(map[string]any{"type": "FeatureCollection", "features": features})
	require.NoError(t, err)
	_, err = store.LandVersion(artifact, strings.NewReader(string(body)+"\n"), "https://example.com/"+artifact+"?_format=geojson", fetchedAt)
	require.NoError(t, err)
}

// fixtureStamgegevens is the checked-in stamgegevens subset: stam-1 and stam-2 are reachable via
// a kapenherplant row's boomId/boomNieuwId respectively; stam-3 seeds an extra target row so the
// stamgegevens count exceeds the number of rows kapenherplant actually joins against.
func fixtureStamgegevens() []map[string]any {
	return []map[string]any{
		{"id": "stam-1", "gbdBuurtId": "A01", "soortnaam": "Tilia", "geometrie": geoJSONPoint(4.895, 52.370)},
		{"id": "stam-2", "gbdBuurtId": "A02", "soortnaam": "Quercus", "geometrie": geoJSONPoint(4.900, 52.375)},
		{"id": "stam-3", "gbdBuurtId": "A04", "soortnaam": "Fraxinus", "geometrie": geoJSONPoint(4.905, 52.380)},
	}
}

// fixtureKapenherplantV1 is the checked-in initial kapenherplant subset, spanning the three join
// cases:
//   - kap-a: boomId directly matches a stamgegevens row (resolvedVia=boomId). Its
//     plantmaatregelDatumUitgevoerd starts out empty (an unfilled lifecycle date, a normal state
//     for a tree not yet replanted), so a later fill-in exercises mergeSQL's update-on-match path.
//   - kap-b: boomId matches nothing (a retired id), boomNieuwId matches stam-2
//     (resolvedVia=boomNieuwId fallback).
//   - kap-c: neither boomId nor boomNieuwId matches anything (genuinely unresolved). Dropped
//     entirely in v2 to exercise the soft-delete path.
func fixtureKapenherplantV1() []map[string]any {
	return []map[string]any{
		{
			"id": "kap-a", "boomId": "stam-1", "boomNieuwId": "", "gbdBuurtId": "A01",
			"dichtstbijzijndeBagAdres": "Teststraat 1", "postcode": "1011AB",
			"soortnaam": "Tilia", "toeTePassenBoomsoort": "Tilia",
			"datumVergunningsaanvraag":      "2023-01-01T00:00:00Z",
			"kapmaatregelDatumUitgevoerd":   "2023-02-01T00:00:00Z",
			"plantmaatregelDatumUitgevoerd": "",
		},
		{
			"id": "kap-b", "boomId": "retired-1", "boomNieuwId": "stam-2", "gbdBuurtId": "A02",
			"dichtstbijzijndeBagAdres": "Teststraat 2", "postcode": "1012CD",
			"soortnaam": "Quercus", "toeTePassenBoomsoort": "Quercus",
			"datumVergunningsaanvraag":      "2023-04-01",
			"kapmaatregelDatumUitgevoerd":   "2023-05-01",
			"plantmaatregelDatumUitgevoerd": "2023-06-01",
		},
		{
			"id": "kap-c", "boomId": "unknown-1", "boomNieuwId": "unknown-2", "gbdBuurtId": "A03",
			"dichtstbijzijndeBagAdres": "Teststraat 3", "postcode": "1013EF",
			"soortnaam": "Populus", "toeTePassenBoomsoort": "Populus",
			"datumVergunningsaanvraag":      "2023-07-01T00:00:00Z",
			"kapmaatregelDatumUitgevoerd":   "2023-08-01T00:00:00Z",
			"plantmaatregelDatumUitgevoerd": "2023-09-01T00:00:00Z",
		},
	}
}

// fixtureKapenherplantV2 is a fresh export superseding v1: kap-a's plantmaatregelDatumUitgevoerd
// is newly filled in (its raw content changed, so mergeSQL's WHEN MATCHED AND raw changed clause
// must refresh it), kap-b is carried over byte-for-byte unchanged, and kap-c is dropped entirely
// (absent from the fresh export, not an explicit tombstone — the soft-delete case).
func fixtureKapenherplantV2() []map[string]any {
	v1 := fixtureKapenherplantV1()

	kapA := make(map[string]any, len(v1[0]))
	for k, v := range v1[0] {
		kapA[k] = v
	}
	kapA["plantmaatregelDatumUitgevoerd"] = "2023-03-01T00:00:00Z"

	return []map[string]any{kapA, v1[1]}
}

// seed lands both kapenherplant and stamgegevens rows as one new version each, mirroring how
// ingest/bomen lands both datasets independently: kapenherplant still lands as an _embedded page
// (landPage, using load.go's artifactKapenherplant/embedKeyKapenherplant), stamgegevens now lands
// as a GeoJSON FeatureCollection (landStamgegevensGeoJSON, using artifactStamgegevens) — so this
// test exercises the real ingest<->load landing contract for both current shapes.
func seed(t *testing.T, store *shared.RawStore, kapRows, stamRows []map[string]any, fetchedAt time.Time) {
	t.Helper()
	landPage(t, store, artifactKapenherplant, embedKeyKapenherplant, kapRows, fetchedAt)
	landStamgegevensGeoJSON(t, store, artifactStamgegevens, stamRows, fetchedAt)
}

// countRowsWhere counts table's rows matching where (or all rows, if where is empty).
func countRowsWhere(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, where string) int {
	t.Helper()
	sql := "SELECT count(*) FROM " + table
	if where != "" {
		sql += " WHERE " + where
	}
	var n int
	require.NoError(t, pool.QueryRow(ctx, sql).Scan(&n))
	return n
}

// kapRow is one queried-back kapenherplant row's columns relevant to the assertions below.
type kapRow struct {
	ResolvedVia   string
	ResolvedLon   *float64
	ResolvedLat   *float64
	SourceDeleted *time.Time
	PlantDatum    *time.Time
}

func queryKap(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) kapRow {
	t.Helper()
	const sql = `SELECT "resolvedVia", ST_X("resolvedGeom"), ST_Y("resolvedGeom"),
		source_deleted_at, "plantmaatregelDatumUitgevoerd"
		FROM kapenherplant WHERE id = $1`
	var r kapRow
	err := pool.QueryRow(ctx, sql, id).Scan(&r.ResolvedVia, &r.ResolvedLon, &r.ResolvedLat, &r.SourceDeleted, &r.PlantDatum)
	require.NoError(t, err, "query kapenherplant row %s", id)
	return r
}

// stamgegevensPoint reads back a stamgegevens row's geometrie point, so a kapenherplant row's
// resolvedGeom can be asserted to equal it exactly (rather than duplicating the fixture's own
// coordinates in the assertion).
func stamgegevensPoint(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) (lon, lat float64) {
	t.Helper()
	const sql = `SELECT ST_X(geometrie), ST_Y(geometrie) FROM stamgegevens WHERE id = $1`
	require.NoError(t, pool.QueryRow(ctx, sql, id).Scan(&lon, &lat))
	return lon, lat
}

// TestBomenLoad_EndToEnd runs the real Load end to end (read landed rows -> stage -> upsert ->
// resolveJoin) against a real Postgres+PostGIS, in an isolated internal/testdb schema, across a
// sequence of Load calls that each land a fresh version through the real
// ingest/shared.RawStore.LandVersion seam.
func TestBomenLoad_EndToEnd(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	dsURL := fusekiDatasetURL(t)
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	store := shared.NewRawStore(t.TempDir())

	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	kapV1 := fixtureKapenherplantV1()
	stamV1 := fixtureStamgegevens()
	seed(t, store, kapV1, stamV1, t1)

	// -- scenario: initial load (Config{Reset:true}) ------------------------------

	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: true}))

	t.Run("initial load: target tables populated, no soft-deletes", func(t *testing.T) {
		assert.Equal(t, len(kapV1), countRowsWhere(t, ctx, pool, kapenherplantTable, ""), "kapenherplant row count")
		assert.Equal(t, len(stamV1), countRowsWhere(t, ctx, pool, stamgegevensTable, ""), "stamgegevens row count")
		assert.Zero(t, countRowsWhere(t, ctx, pool, kapenherplantTable, "source_deleted_at IS NOT NULL"))
		assert.Zero(t, countRowsWhere(t, ctx, pool, stamgegevensTable, "source_deleted_at IS NOT NULL"))
	})

	// -- scenario: join resolution -------------------------------------------------

	t.Run("join resolution: boomId match, boomNieuwId fallback, unresolved", func(t *testing.T) {
		a := queryKap(t, ctx, pool, "kap-a")
		assert.Equal(t, ResolvedViaBoomID, a.ResolvedVia)
		if assert.NotNil(t, a.ResolvedLon) && assert.NotNil(t, a.ResolvedLat) {
			wantLon, wantLat := stamgegevensPoint(t, ctx, pool, "stam-1")
			assert.InDelta(t, wantLon, *a.ResolvedLon, 1e-9)
			assert.InDelta(t, wantLat, *a.ResolvedLat, 1e-9)
		}

		b := queryKap(t, ctx, pool, "kap-b")
		assert.Equal(t, ResolvedViaBoomNieuwID, b.ResolvedVia)
		if assert.NotNil(t, b.ResolvedLon) && assert.NotNil(t, b.ResolvedLat) {
			wantLon, wantLat := stamgegevensPoint(t, ctx, pool, "stam-2")
			assert.InDelta(t, wantLon, *b.ResolvedLon, 1e-9)
			assert.InDelta(t, wantLat, *b.ResolvedLat, 1e-9)
		}

		c := queryKap(t, ctx, pool, "kap-c")
		assert.Equal(t, ResolvedViaUnresolved, c.ResolvedVia)
		assert.Nil(t, c.ResolvedLon, "unresolved row must have a null resolvedGeom")
		assert.Nil(t, c.ResolvedLat)
	})

	// -- scenario: re-run against identical landed content is a no-op -------------

	t.Run("re-run against identical landed content is a no-op", func(t *testing.T) {
		// Same fixture content, landed again: LandVersion is content-hash idempotent, so no
		// new version is written and readLandedRows reads back the exact same snapshot.
		seed(t, store, kapV1, stamV1, t1.Add(time.Hour))
		require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: false}))

		assert.Equal(t, len(kapV1), countRowsWhere(t, ctx, pool, kapenherplantTable, ""))
		assert.Equal(t, len(stamV1), countRowsWhere(t, ctx, pool, stamgegevensTable, ""))
		assert.Zero(t, countRowsWhere(t, ctx, pool, kapenherplantTable, "source_deleted_at IS NOT NULL"))
		assert.Zero(t, countRowsWhere(t, ctx, pool, stamgegevensTable, "source_deleted_at IS NOT NULL"))

		a := queryKap(t, ctx, pool, "kap-a")
		assert.Equal(t, ResolvedViaBoomID, a.ResolvedVia, "resolvedVia must be unchanged by a no-op re-run")
		assert.Nil(t, a.PlantDatum, "kap-a's plantmaatregelDatumUitgevoerd must still be NULL before the v2 update")
	})

	// -- scenarios: soft-delete + update-on-match ----------------------------------

	t.Run("new version: kap-c soft-deleted, kap-a refreshed, kap-b unaffected", func(t *testing.T) {
		t2 := t1.Add(24 * time.Hour)
		kapV2 := fixtureKapenherplantV2()
		landPage(t, store, artifactKapenherplant, embedKeyKapenherplant, kapV2, t2)
		// stamgegevens is unchanged: land the same content again (dedupes, no new version).
		landStamgegevensGeoJSON(t, store, artifactStamgegevens, stamV1, t2)

		loadTime := time.Now().UTC()
		require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: false}))

		// kap-c: absent from the fresh export -> retained (not physically dropped), with
		// source_deleted_at stamped to the load timestamp.
		c := queryKap(t, ctx, pool, "kap-c")
		require.NotNil(t, c.SourceDeleted, "kap-c must be retained with source_deleted_at set, not dropped")
		assert.WithinDuration(t, loadTime, *c.SourceDeleted, 5*time.Second)

		// kap-a: still present, its raw content changed -> mergeSQL's WHEN MATCHED AND raw
		// changed clause refreshes plantmaatregelDatumUitgevoerd; must stay NOT soft-deleted.
		a := queryKap(t, ctx, pool, "kap-a")
		assert.Nil(t, a.SourceDeleted, "kap-a must not be soft-deleted by an unrelated row's removal")
		if assert.NotNil(t, a.PlantDatum, "kap-a's plantmaatregelDatumUitgevoerd must be refreshed from the new export") {
			assert.True(t, a.PlantDatum.Equal(time.Date(2023, 3, 1, 0, 0, 0, 0, time.UTC)),
				"plantmaatregelDatumUitgevoerd must equal the new export's value, got %v", a.PlantDatum)
		}

		// kap-b: unrelated to either change, must be untouched.
		b := queryKap(t, ctx, pool, "kap-b")
		assert.Nil(t, b.SourceDeleted, "kap-b must be unaffected by kap-a's update or kap-c's removal")
		assert.Equal(t, ResolvedViaBoomNieuwID, b.ResolvedVia)

		// Row count unchanged: soft-delete never physically removes a row.
		assert.Equal(t, len(kapV1), countRowsWhere(t, ctx, pool, kapenherplantTable, ""),
			"soft-deleted rows are retained, not dropped, so the count is unchanged")
	})
}

// TestBomenLoad_FullCorpusGate documents the full-corpus tier — this is NOT a runnable test (no
// live network call belongs in default CI, per design.md D6), just the recipe for the opt-in,
// manual gate:
//
//  1. Run the real paged pull against the live Datapunt bomen API via ingest/bomen.IngestAll
//     (both kapenherplant and stamgegevens, full-city, no date/area filter), landing each via
//     shared.RawStore.LandVersion exactly as production does.
//  2. Call load/bomen.Load against a real (or --reset) Postgres+PostGIS target with that raw
//     store, cfg.Reset as appropriate.
//  3. Assert the exact Spike C city-wide counts: 35,202 kapenherplant rows and 323,728
//     stamgegevens rows.
//  4. Assert the join-resolution rate: count kapenherplant rows where "resolvedVia" !=
//     'unresolved', divide by 35,202, and assert it is approximately 98% (design.md's
//     ~98%/~71%-boomId-only figures).
//
// This gate is deliberately NOT wired into `task test:integration` or the go-integration CI job:
// it is network-dependent (hits the live Datapunt API) and slow (hundreds of paged requests for
// 358,930 total rows) relative to the seeded, checked-in subset TestBomenLoad_EndToEnd above
// exercises on every CI run. Run it manually/ad hoc when validating against the live source (e.g.
// after a Datapunt API/schema change), not as part of routine verification.
func TestBomenLoad_FullCorpusGate(t *testing.T) {
	t.Skip("full-corpus gate is documentation-only — see the doc comment above; not wired into CI or task test:integration")
}
