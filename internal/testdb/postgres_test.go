//go:build integration

package testdb

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// requireEnv fails the test loudly (never skips) if name is unset — an
// integration test must never silently pass with zero coverage because its
// isolated-target config was missing.
func requireEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	require.NotEmptyf(t, v, "%s must be set to run integration tests (see README.md)", name)
	return v
}

func TestPostgresSchemaLifecycle(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	schemaName, err := NewSchemaName()
	require.NoError(t, err)

	require.NoError(t, CreateSchema(ctx, dsn, schemaName))
	t.Cleanup(func() {
		require.NoError(t, DropSchema(ctx, dsn, schemaName), "DropSchema cleanup")
	})

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	table := pgx.Identifier{schemaName, "probe"}.Sanitize()
	_, err = conn.Exec(ctx, "CREATE TABLE "+table+" (id int)")
	require.NoError(t, err, "create table in isolated schema")
	_, err = conn.Exec(ctx, "INSERT INTO "+table+" (id) VALUES (1)")
	require.NoError(t, err, "insert into isolated schema")

	var count int
	require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count),
		"query isolated schema")
	require.Equal(t, 1, count)
}

func TestPostgresGuardAbortsOnProductionName(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	require.Error(t, CreateSchema(ctx, dsn, "gemeten_stad"),
		"CreateSchema against the production database name must abort")
	require.Error(t, CreateSchema(ctx, dsn, "public"),
		"CreateSchema against the default schema name must abort")
}
