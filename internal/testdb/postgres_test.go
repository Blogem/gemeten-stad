//go:build integration

package testdb

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// requireEnv fails the test loudly (never skips) if name is unset — an
// integration test must never silently pass with zero coverage because its
// isolated-target config was missing.
func requireEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatalf("%s must be set to run integration tests (see README.md)", name)
	}
	return v
}

func TestPostgresSchemaLifecycle(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	schemaName, err := NewSchemaName()
	if err != nil {
		t.Fatalf("NewSchemaName: %v", err)
	}

	if err := CreateSchema(ctx, dsn, schemaName); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}
	t.Cleanup(func() {
		if err := DropSchema(ctx, dsn, schemaName); err != nil {
			t.Errorf("DropSchema cleanup: %v", err)
		}
	})

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	table := pgx.Identifier{schemaName, "probe"}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE TABLE "+table+" (id int)"); err != nil {
		t.Fatalf("create table in isolated schema: %v", err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO "+table+" (id) VALUES (1)"); err != nil {
		t.Fatalf("insert into isolated schema: %v", err)
	}

	var count int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("query isolated schema: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestPostgresGuardAbortsOnProductionName(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	if err := CreateSchema(ctx, dsn, "gemeten_stad"); err == nil {
		t.Fatal("CreateSchema against the production database name must abort, got nil error")
	}
	if err := CreateSchema(ctx, dsn, "public"); err == nil {
		t.Fatal("CreateSchema against the default schema name must abort, got nil error")
	}
}
