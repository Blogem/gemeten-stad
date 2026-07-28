//go:build integration

// This file is a regression test for the load/bomen schema-qualification fix: Load's target-table
// DDL/DML (dropTargets, ensureSchema, upsertAll, resolveJoin) used to reference kapenherplant/
// stamgegevens UNQUALIFIED, so an unqualified name fell through to whichever schema in the pool's
// search_path happened to have a matching relation FIRST — not necessarily the load's own isolated
// schema. Concretely: cfg.Reset's `DROP TABLE IF EXISTS kapenherplant CASCADE` would resolve, via
// search_path, to an unrelated app table living earlier on the path and drop it — silently
// destroying data that belongs to another schema entirely. The fix resolves current_schema() and
// qualifies every target-table reference with it.
//
// This test proves the fix using TWO isolated internal/testdb schemas (never public, so the test
// itself can never risk real data): schema A stands in for "some other app schema reachable via
// search_path" (what public would be in production), holding decoy kapenherplant/stamgegevens
// tables with a sentinel row each; schema B is the load's own isolated schema (current_schema()).
// The pool's search_path is set to B,A,public — the load's own schema first, but an unrelated
// schema (A) still reachable, exactly the shape that triggers unqualified-name fall-through on the
// old code. Running bomen.Load(Config{Reset:true}) against this pool must never touch A: A's
// sentinel rows (and the tables themselves) must survive untouched, while B ends up correctly
// populated with the fixture. This fails on the old unqualified code (which drops A's decoy via
// fall-through) and passes once table references are qualified.
package bomen

import (
	"context"
	"testing"
	"time"

	"github.com/Blogem/gemeten-stad/ingest/shared"
	"github.com/Blogem/gemeten-stad/internal/testdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newIsolationPool creates two fresh internal/testdb schemas — schemaA (a stand-in for an
// unrelated app schema reachable via search_path, e.g. what "public" would be in production) and
// schemaB (the load's own isolated target schema) — both dropped on cleanup, and returns a pool
// whose every connection sets search_path = schemaB, schemaA, public. schemaB is deliberately
// first, so it is current_schema() (what the fix qualifies against); schemaA is deliberately
// second and reachable, standing in for the unrelated schema an unqualified name could fall
// through to on the old, unqualified code.
func newIsolationPool(t *testing.T, ctx context.Context, dsn string) (pool *pgxpool.Pool, schemaA, schemaB string) {
	t.Helper()

	var err error
	schemaA, err = testdb.NewSchemaName()
	require.NoError(t, err)
	require.NoError(t, testdb.CreateSchema(ctx, dsn, schemaA))
	t.Cleanup(func() {
		assert.NoError(t, testdb.DropSchema(ctx, dsn, schemaA), "DropSchema cleanup (A)")
	})

	schemaB, err = testdb.NewSchemaName()
	require.NoError(t, err)
	require.NoError(t, testdb.CreateSchema(ctx, dsn, schemaB))
	t.Cleanup(func() {
		assert.NoError(t, testdb.DropSchema(ctx, dsn, schemaB), "DropSchema cleanup (B)")
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	searchPath := pgx.Identifier{schemaB}.Sanitize() + ", " + pgx.Identifier{schemaA}.Sanitize() + ", public"
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path = "+searchPath)
		return err
	}

	pool, err = pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, pool.Ping(ctx))
	return pool, schemaA, schemaB
}

// seedDecoyTargets creates kapenherplant/stamgegevens tables in schema (shaped like schema.go's
// real targets, so they are plausible collision targets for search_path fall-through) with a
// single sentinel row each (id='SENTINEL') — the probe this test's assertions check for survival.
func seedDecoyTargets(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema string) {
	t.Helper()

	// postgis must exist for the decoys' geometry columns; harmless/idempotent if another test
	// (or Load itself, later) already created it.
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS postgis")
	require.NoError(t, err)

	ident := func(table string) string { return pgx.Identifier{schema, table}.Sanitize() }

	_, err = pool.Exec(ctx, `CREATE TABLE `+ident("kapenherplant")+` (
	    id                              text PRIMARY KEY,
	    "boomId"                        text,
	    "boomNieuwId"                   text,
	    "gbdBuurtId"                    text,
	    "dichtstbijzijndeBagAdres"      text,
	    postcode                        text,
	    soortnaam                       text,
	    "toeTePassenBoomsoort"          text,
	    "datumVergunningsaanvraag"      timestamptz,
	    "kapmaatregelDatumUitgevoerd"   timestamptz,
	    "plantmaatregelDatumUitgevoerd" timestamptz,
	    "resolvedGeom"                  geometry(Point, 4326),
	    "resolvedVia"                   text,
	    source_deleted_at               timestamptz,
	    raw                             jsonb NOT NULL
	)`)
	require.NoError(t, err, "create decoy kapenherplant in schema %s", schema)
	_, err = pool.Exec(ctx, `INSERT INTO `+ident("kapenherplant")+` (id, raw) VALUES ('SENTINEL', '{}'::jsonb)`)
	require.NoError(t, err, "insert sentinel row into decoy kapenherplant")

	_, err = pool.Exec(ctx, `CREATE TABLE `+ident("stamgegevens")+` (
	    id                 text PRIMARY KEY,
	    "gbdBuurtId"       text,
	    soortnaam          text,
	    geometrie          geometry(Point, 4326),
	    source_deleted_at  timestamptz,
	    raw                jsonb NOT NULL
	)`)
	require.NoError(t, err, "create decoy stamgegevens in schema %s", schema)
	_, err = pool.Exec(ctx, `INSERT INTO `+ident("stamgegevens")+` (id, raw) VALUES ('SENTINEL', '{}'::jsonb)`)
	require.NoError(t, err, "insert sentinel row into decoy stamgegevens")
}

// countInSchema counts table's rows in schema, always schema-qualified so the count can never
// itself be fooled by search_path resolution the way an unqualified query could.
func countInSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema, table, where string) int {
	t.Helper()
	sql := "SELECT count(*) FROM " + pgx.Identifier{schema, table}.Sanitize()
	if where != "" {
		sql += " WHERE " + where
	}
	var n int
	require.NoError(t, pool.QueryRow(ctx, sql).Scan(&n), "count %s.%s", schema, table)
	return n
}

// TestBomenLoad_ConfinedToOwnSchema is the regression test: Load(Config{Reset: true}) run against
// a pool whose search_path is <B>,<A>,public must operate exclusively on B (current_schema()) and
// must never read, write, or drop anything in A — even though A is reachable via search_path and
// holds tables with the exact same unqualified names as the load's own targets.
func TestBomenLoad_ConfinedToOwnSchema(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	dsURL := fusekiDatasetURL(t)
	ctx := context.Background()

	pool, schemaA, schemaB := newIsolationPool(t, ctx, dsn)

	// B (current_schema()) is the load's own isolated schema — the isolation guard must confirm
	// this before the regression assertions below mean anything.
	require.NoError(t, testdb.AssertIsolatedSchema(ctx, pool, schemaB))

	// A stands in for an unrelated app schema reachable via search_path (e.g. "public" in
	// production): decoy kapenherplant/stamgegevens tables with a sentinel row each.
	seedDecoyTargets(t, ctx, pool, schemaA)

	store := shared.NewRawStore(t.TempDir())
	fetchedAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	seed(t, store, fixtureKapenherplantV1(), fixtureStamgegevens(), fetchedAt)

	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: true}))

	t.Run("schema A's decoy tables and sentinel rows survive untouched", func(t *testing.T) {
		assert.Equal(t, 1, countInSchema(t, ctx, pool, schemaA, "kapenherplant", "id = 'SENTINEL'"),
			"A's decoy kapenherplant sentinel row must survive a Load(Reset:true) run targeting B")
		assert.Equal(t, 1, countInSchema(t, ctx, pool, schemaA, "stamgegevens", "id = 'SENTINEL'"),
			"A's decoy stamgegevens sentinel row must survive a Load(Reset:true) run targeting B")
	})

	t.Run("schema B is populated with the loaded fixture, not A", func(t *testing.T) {
		assert.Equal(t, len(fixtureKapenherplantV1()), countInSchema(t, ctx, pool, schemaB, "kapenherplant", ""),
			"B must hold the loaded kapenherplant rows")
		assert.Equal(t, len(fixtureStamgegevens()), countInSchema(t, ctx, pool, schemaB, "stamgegevens", ""),
			"B must hold the loaded stamgegevens rows")
		// The fixture never contains a SENTINEL row; if B has one, A and B were conflated.
		assert.Zero(t, countInSchema(t, ctx, pool, schemaB, "kapenherplant", "id = 'SENTINEL'"),
			"B must not have picked up A's sentinel row")
		assert.Zero(t, countInSchema(t, ctx, pool, schemaB, "stamgegevens", "id = 'SENTINEL'"),
			"B must not have picked up A's sentinel row")
	})
}
