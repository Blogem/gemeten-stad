//go:build integration

// AssertIsolatedSchema tests: it must confirm a live pool's connections actually resolve
// current_schema() to the isolated schema a test believes it is targeting — the last line of
// defense against a harness bug (a missing/wrong search_path override) that would otherwise let an
// "isolated" integration test silently operate against public or another schema.
package testdb

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPoolWithSearchPath returns a pool whose every connection sets search_path to schema (plus
// public, for any Postgres/PostGIS built-ins a caller might still need), mirroring the
// load/bomen and load/geo integration harnesses' own newSchemaPool shape.
func newPoolWithSearchPath(t *testing.T, ctx context.Context, dsn, schema string) *pgxpool.Pool {
	t.Helper()

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

func TestAssertIsolatedSchema_PassesWhenSearchPathResolvesToWantSchema(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	schema, err := NewSchemaName()
	require.NoError(t, err)
	require.NoError(t, CreateSchema(ctx, dsn, schema))
	t.Cleanup(func() {
		assert.NoError(t, DropSchema(ctx, dsn, schema), "DropSchema cleanup")
	})

	pool := newPoolWithSearchPath(t, ctx, dsn, schema)

	assert.NoError(t, AssertIsolatedSchema(ctx, pool, schema),
		"a pool whose search_path resolves current_schema() to the isolated schema must pass")
}

func TestAssertIsolatedSchema_ErrorsWithNoSearchPathOverride(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	// wantSchema is a legitimately-generated isolated name, but this pool is built straight from
	// dsn with no AfterConnect override at all — the exact harness bug the guard exists to catch:
	// current_schema() resolves to the target database's default ("public"), never to wantSchema.
	wantSchema, err := NewSchemaName()
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, pool.Ping(ctx))

	err = AssertIsolatedSchema(ctx, pool, wantSchema)
	assert.Error(t, err, "a pool with no search_path override (current_schema()=public) must fail the isolation check")
}

func TestAssertIsolatedSchema_ErrorsOnSchemaMismatch(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	// The pool's search_path resolves current_schema() to actualSchema, but the caller asks the
	// guard to confirm a DIFFERENT isolated schema (wantSchema) — this must fail too, proving the
	// guard checks the specific wantSchema argument, not merely "isolated schema, any schema".
	actualSchema, err := NewSchemaName()
	require.NoError(t, err)
	require.NoError(t, CreateSchema(ctx, dsn, actualSchema))
	t.Cleanup(func() {
		assert.NoError(t, DropSchema(ctx, dsn, actualSchema), "DropSchema cleanup")
	})

	wantSchema, err := NewSchemaName()
	require.NoError(t, err)
	require.NotEqual(t, actualSchema, wantSchema, "test fixture bug: names must differ to exercise a mismatch")

	pool := newPoolWithSearchPath(t, ctx, dsn, actualSchema)

	err = AssertIsolatedSchema(ctx, pool, wantSchema)
	assert.Error(t, err, "current_schema()=%s must not satisfy a check for wantSchema=%s", actualSchema, wantSchema)
}
