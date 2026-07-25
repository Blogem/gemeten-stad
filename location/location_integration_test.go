//go:build integration

package location

// Integration tests for P6 task 7.6: location.Resolve exercised end-to-end
// against a real-shaped BAG + gebieden subset (testdata/target_seed.sql),
// SQL-seeded directly into the TARGET tables (bag_*, gebieden_*) in an isolated
// internal/testdb schema. This package never drives load/geo's staging/upsert
// seam (its load-from-staging entry points are unexported, and Load() itself
// needs the gdal sidecar) — Resolve only ever reads the target tables, so
// seeding them directly keeps this test self-contained, per the task contract.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Blogem/gemeten-stad/internal/testdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireEnv fails the test loudly (never skips) if name is unset, matching the
// P5 convention in internal/testdb/postgres_test.go.
func requireEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	require.NotEmptyf(t, v, "%s must be set to run integration tests (see README.md)", name)
	return v
}

// newSchemaPool creates a fresh internal/testdb schema (dropped on cleanup) and
// returns a pool whose every connection has search_path set to <schema>,public.
// location's resolver SQL (sql.go) uses UNQUALIFIED table names, and the
// ST_Contains/ST_Centroid/similarity() PostGIS/pg_trgm functions live in public,
// so both must be on the path.
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

	// The load/geo migration (not driven here — see package doc) is the thing
	// that normally creates postgis/pg_trgm; this test seeds the target tables
	// directly, so it must ensure the same extensions itself.
	_, err = pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS postgis; CREATE EXTENSION IF NOT EXISTS pg_trgm;")
	require.NoError(t, err)

	return pool
}

// seedTargets loads testdata/target_seed.sql (the checked-in real-shaped
// subset, mirroring load/geo/testdata/staging_seed.sql) directly into the
// target tables.
func seedTargets(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	sql, err := os.ReadFile("testdata/target_seed.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(sql))
	require.NoError(t, err)
}

// testDate is the query valid-time date used throughout: after num-a's,
// num-c's, num-d's, num-e's and num-f's begingeldigheid (so those voorkomens are
// valid at this date), but before num-b's begingeldigheid (so num-b can only
// resolve via the any-time fallback).
var testDate = time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

func TestResolve_AgainstSeededTargets(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedTargets(t, ctx, pool)

	t.Run("address via (postcode, huisnummer)", func(t *testing.T) {
		result, err := Resolve(ctx, pool, Query{Postcode: "1000AB", Huisnummer: 1, Date: testDate})
		require.NoError(t, err)
		assert.Equal(t, PlaceAddress, result.PlaceLevel)
		assert.Equal(t, 0.90, result.Confidence)
		assert.Contains(t, result.Geom, "POINT(121000 487000)", "geom must be vbo-1's own point")
		assert.Equal(t, timeMatchValidAtDate, result.TimeMatch)
		assert.Empty(t, result.Caveats)
	})

	t.Run("address via exact street match", func(t *testing.T) {
		result, err := Resolve(ctx, pool, Query{Street: "Teststraat", Huisnummer: 4, Date: testDate})
		require.NoError(t, err)
		assert.Equal(t, PlaceAddress, result.PlaceLevel)
		assert.Equal(t, 0.90, result.Confidence)
		assert.Contains(t, result.Geom, "POINT(121030 487030)", "geom must be vbo-4's own point (num-d)")
	})

	t.Run("postcode-only fallback: PC6 exists but huisnummer does not resolve", func(t *testing.T) {
		result, err := Resolve(ctx, pool, Query{Postcode: "1000AB", Huisnummer: 999, Date: testDate})
		require.NoError(t, err)
		assert.Equal(t, PlacePostcode, result.PlaceLevel)
		assert.Equal(t, 0.70, result.Confidence)
		assert.Empty(t, result.Geom, "no single point applies at the postcode tier")
	})

	t.Run("buurt PIP fallback: point carries a known gbdBuurtId", func(t *testing.T) {
		// Inside buurt-noord, away from any seeded address point.
		result, err := Resolve(ctx, pool, Query{Point: &RDPoint{X: 120900, Y: 486900}, Date: testDate})
		require.NoError(t, err)
		assert.Equal(t, PlaceBuurt, result.PlaceLevel)
		assert.Equal(t, 0.50, result.Confidence)
		assert.Equal(t, "buurt-noord", result.BuurtID)
		assert.Contains(t, result.Caveats, caveatUnresolvedLocation)
	})

	t.Run("buurt PIP fallback: point is NOT snapped to a nearer address", func(t *testing.T) {
		// Exactly vbo-1's own coordinates, but no street/postcode/huisnummer
		// given — must stay at buurt, never get promoted to vbo-1's address.
		result, err := Resolve(ctx, pool, Query{Point: &RDPoint{X: 121000, Y: 487000}, Date: testDate})
		require.NoError(t, err)
		assert.Equal(t, PlaceBuurt, result.PlaceLevel, "must not snap to the nearby address vbo-1")
		assert.Equal(t, "buurt-noord", result.BuurtID)
		assert.Equal(t, 0.50, result.Confidence)
	})

	t.Run("buurt PIP fallback in a disjoint buurt with no seeded address", func(t *testing.T) {
		result, err := Resolve(ctx, pool, Query{Point: &RDPoint{X: 130050, Y: 490050}, Date: testDate})
		require.NoError(t, err)
		assert.Equal(t, PlaceBuurt, result.PlaceLevel)
		assert.Equal(t, "buurt-anders", result.BuurtID)
	})

	t.Run("valid-at-date voorkomen is preferred", func(t *testing.T) {
		result, err := Resolve(ctx, pool, Query{Postcode: "1000AB", Huisnummer: 1, Date: testDate})
		require.NoError(t, err)
		assert.Equal(t, timeMatchValidAtDate, result.TimeMatch)
		assert.NotContains(t, result.Caveats, caveatTimeMismatch)
	})

	t.Run("any-time fallback is flagged", func(t *testing.T) {
		// num-b's only best-known voorkomen begins 2025-01-01, after testDate.
		result, err := Resolve(ctx, pool, Query{Postcode: "1000AC", Huisnummer: 2, Date: testDate})
		require.NoError(t, err)
		assert.Equal(t, PlaceAddress, result.PlaceLevel, "resolves anyway, flagged rather than dropped")
		assert.Equal(t, timeMatchAnyTime, result.TimeMatch)
		assert.Contains(t, result.Caveats, caveatTimeMismatch)
	})

	t.Run("since-withdrawn address still resolves", func(t *testing.T) {
		// num-c's best-known voorkomen has status 'Naamgeving ingetrokken'.
		result, err := Resolve(ctx, pool, Query{Postcode: "1000AD", Huisnummer: 3, Date: testDate})
		require.NoError(t, err)
		assert.Equal(t, PlaceAddress, result.PlaceLevel, "no status filter is applied")
		assert.Equal(t, timeMatchValidAtDate, result.TimeMatch)
	})

	t.Run("ligplaats centroid path", func(t *testing.T) {
		result, err := Resolve(ctx, pool, Query{Postcode: "1000AG", Huisnummer: 6, Date: testDate})
		require.NoError(t, err)
		assert.Equal(t, PlaceAddress, result.PlaceLevel)
		assert.Contains(t, result.Geom, "POINT(121105 487105)", "geom must be lig-1's polygon centroid")
	})

	t.Run("standplaats centroid path", func(t *testing.T) {
		result, err := Resolve(ctx, pool, Query{Postcode: "1000AH", Huisnummer: 7, Date: testDate})
		require.NoError(t, err)
		assert.Equal(t, PlaceAddress, result.PlaceLevel)
		assert.Contains(t, result.Geom, "POINT(121205 487205)", "geom must be sta-1's polygon centroid")
	})

	t.Run("nothing resolves is an error", func(t *testing.T) {
		_, err := Resolve(ctx, pool, Query{Postcode: "9999ZZ", Huisnummer: 1, Date: testDate})
		assert.Error(t, err)
	})
}
