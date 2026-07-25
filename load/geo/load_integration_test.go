//go:build integration

package geo

// Integration tests for P6 tasks 7.4/7.5: a checked-in real-shaped BAG + gebieden
// subset (testdata/staging_seed.sql), SQL-seeded into *_staging tables in an
// isolated internal/testdb schema, driving the REAL load-from-staging seam
// (ensureExtensions -> ensureSchema -> upsertAll -> ensureIndexes -> gateSRID),
// mirroring load.go's own call order. No gdal sidecar: staging is SQL-seeded here,
// never ogr2ogr'd.
//
// Per the task contract, gateNoordGroundTruth's 69/15 anchor is NOT asserted to
// hold on this subset (design D6: that gate is the full-corpus/manual anchor) —
// this file instead asserts it FAILS on the subset, documenting the seam.

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
// The load DDL and the upsert/gate SQL all use UNQUALIFIED table names, and
// PostGIS functions (ST_GeomFromText, etc.) live in public, so both must be on
// the path.
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
	return pool
}

// seedStaging loads testdata/staging_seed.sql (the checked-in real-shaped
// subset, task 7.4) into the *_staging tables. It has no bound parameters, so
// pgx sends it as a single simple-protocol message — the only mode Postgres
// accepts multiple ;-separated statements in (see also ensureSchema/
// ensureIndexes, which rely on the same zero-arg-implies-simple-protocol
// behavior in production).
func seedStaging(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	sql, err := os.ReadFile("testdata/staging_seed.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(sql))
	require.NoError(t, err)
}

func sourceDeletedAtCount(ctx context.Context, pool *pgxpool.Pool, table string) (int, error) {
	return countWhere(ctx, pool, table, "source_deleted_at IS NOT NULL")
}

func TestGeoLoad_FromStaging(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)

	require.NoError(t, ensureExtensions(ctx, pool))
	require.NoError(t, ensureSchema(ctx, pool))
	seedStaging(t, ctx, pool)

	// -- 7.5 upsert insert: the seeded voorkomens land in the targets ------------

	loadTS1 := time.Now().UTC()
	require.NoError(t, upsertAll(ctx, pool, loadTS1))

	wantCounts := map[string]int{
		"bag_openbareruimte":  1,
		"bag_nummeraanduiding": 8, // num-a (2 voorkomens) + b,c,d,e,f,g
		"bag_verblijfsobject": 4,
		"bag_ligplaats":       1,
		"bag_standplaats":     1,
		"gebieden_buurten":    2,
		"gebieden_wijken":     1,
		"cbs_buurten":         1,
	}

	assertCounts := func(t *testing.T) {
		t.Helper()
		for table, want := range wantCounts {
			got, err := countWhere(ctx, pool, table, "")
			require.NoError(t, err, "count %s", table)
			assert.Equalf(t, want, got, "table %s row count", table)
		}
	}

	assertNoSoftDeletes := func(t *testing.T) {
		t.Helper()
		for table := range wantCounts {
			n, err := sourceDeletedAtCount(ctx, pool, table)
			require.NoError(t, err, "count soft-deleted rows in %s", table)
			assert.Zerof(t, n, "table %s: expected no soft-deleted rows", table)
		}
	}

	t.Run("upsert insert: staging voorkomens land in targets", func(t *testing.T) {
		assertCounts(t)
		assertNoSoftDeletes(t)
	})

	// -- 7.5 re-run no-op: unchanged staging inserts/updates/soft-deletes nothing --

	t.Run("re-run against unchanged staging is a no-op", func(t *testing.T) {
		loadTS2 := time.Now().UTC()
		require.NoError(t, upsertAll(ctx, pool, loadTS2))
		assertCounts(t)
		assertNoSoftDeletes(t)
	})

	// -- 7.5 soft-delete: an object absent from a fresh staging snapshot ----------

	t.Run("soft-delete: object removed from staging is retained with source_deleted_at set", func(t *testing.T) {
		_, err := pool.Exec(ctx, "DELETE FROM bag_nummeraanduiding_staging WHERE identificatie = 'num-g'")
		require.NoError(t, err)

		loadTS3 := time.Now().UTC()
		require.NoError(t, upsertAll(ctx, pool, loadTS3))

		// num-g's target row is retained (not physically deleted) with
		// source_deleted_at stamped to the load timestamp.
		var deletedAt time.Time
		err = pool.QueryRow(ctx,
			"SELECT source_deleted_at FROM bag_nummeraanduiding WHERE identificatie = 'num-g'",
		).Scan(&deletedAt)
		require.NoError(t, err, "num-g row must still be present")
		assert.WithinDuration(t, loadTS3, deletedAt, time.Second)

		// num-a's still-present voorkomen keeps source_deleted_at NULL.
		var stillNull *time.Time
		err = pool.QueryRow(ctx,
			"SELECT source_deleted_at FROM bag_nummeraanduiding WHERE identificatie = 'num-a' AND begingeldigheid = '2020-01-01'",
		).Scan(&stillNull)
		require.NoError(t, err)
		assert.Nil(t, stillNull)

		n, err := countWhere(ctx, pool, "bag_nummeraanduiding", "source_deleted_at IS NOT NULL")
		require.NoError(t, err)
		assert.Equal(t, 1, n, "only num-g's row should be soft-deleted")

		// Row count is unchanged — soft-delete never physically removes a row.
		got, err := countWhere(ctx, pool, "bag_nummeraanduiding", "")
		require.NoError(t, err)
		assert.Equal(t, wantCounts["bag_nummeraanduiding"], got)
	})

	// -- 7.5 new voorkomen: an additional voorkomen for an existing object --------

	t.Run("new voorkomen: additional voorkomen for an existing object inserts, priors remain", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO bag_nummeraanduiding_staging
			(identificatie, postcode, huisnummer, openbareruimteref, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status)
			VALUES ('num-a', '1000AB', '1', 'opr-teststraat', '2024-01-01', NULL, NULL, '2024-01-01T00:00:00Z', 'Naamgeving uitgegeven')`)
		require.NoError(t, err)

		loadTS4 := time.Now().UTC()
		require.NoError(t, upsertAll(ctx, pool, loadTS4))

		got, err := countWhere(ctx, pool, "bag_nummeraanduiding", "identificatie = 'num-a'")
		require.NoError(t, err)
		assert.Equal(t, 3, got, "num-a's 2 prior voorkomens must remain plus the new one")

		// The prior voorkomens are unchanged (still present, not soft-deleted).
		for _, begin := range []string{"2000-01-01", "2020-01-01"} {
			var deletedAt *time.Time
			err := pool.QueryRow(ctx,
				"SELECT source_deleted_at FROM bag_nummeraanduiding WHERE identificatie = 'num-a' AND begingeldigheid = $1",
				begin,
			).Scan(&deletedAt)
			require.NoError(t, err, "prior voorkomen begingeldigheid=%s must still exist", begin)
			assert.Nil(t, deletedAt, "prior voorkomen begingeldigheid=%s must not be soft-deleted", begin)
		}
	})

	// -- 7.5 indexes: ensureIndexes creates the resolver's indexes ----------------

	t.Run("ensureIndexes creates the resolver's indexes", func(t *testing.T) {
		require.NoError(t, ensureIndexes(ctx, pool))

		wantIndexes := []string{
			"ix_vbo_geom", "ix_lig_geom", "ix_sta_geom", "ix_buurt_geom",
			"ix_opr_naam_trgm",
			"ix_num_pc_hn", "ix_num_ident", "ix_num_opr_ref",
			"ix_vbo_ref", "ix_lig_ref", "ix_sta_ref", "ix_opr_ident",
		}
		for _, idx := range wantIndexes {
			var exists bool
			err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)", idx).Scan(&exists)
			require.NoError(t, err, "query pg_indexes for %s", idx)
			assert.Truef(t, exists, "expected index %s to exist after ensureIndexes", idx)
		}
	})

	// -- 7.5 SRID gate -------------------------------------------------------------

	t.Run("gateSRID passes on the seeded 28992 geometry", func(t *testing.T) {
		assert.NoError(t, gateSRID(ctx, pool))
	})

	// documents the seam: the 69/15 Noord ground-truth gate is a full-corpus
	// anchor (design D6) and cannot hold on this subset.
	t.Run("gateNoordGroundTruth fails on the subset (full-corpus-only anchor)", func(t *testing.T) {
		assert.Error(t, gateNoordGroundTruth(ctx, pool))
	})
}
