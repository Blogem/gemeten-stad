package testdb

import (
	"context"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5"
)

// CreateSchema connects to dsn and creates schemaName. It re-checks the guard
// itself (in addition to any check the caller already did) so this function
// can never be used to create a reserved schema even if called directly.
func CreateSchema(ctx context.Context, dsn, schemaName string) error {
	if err := AssertNotProduction(schemaName, ""); err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("testdb: connect to %s: %w", dsn, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	ident := pgx.Identifier{schemaName}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		return fmt.Errorf("testdb: create schema %s: %w", schemaName, err)
	}
	return nil
}

// DropSchema connects to dsn and drops schemaName (CASCADE). It re-checks the
// guard itself so a caller can never be tricked into dropping a reserved
// schema either.
func DropSchema(ctx context.Context, dsn, schemaName string) error {
	if err := AssertNotProduction(schemaName, ""); err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("testdb: connect to %s: %w", dsn, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	ident := pgx.Identifier{schemaName}.Sanitize()
	if _, err := conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+ident+" CASCADE"); err != nil {
		return fmt.Errorf("testdb: drop schema %s: %w", schemaName, err)
	}
	return nil
}

// CreateDatabase connects to dsn (any existing database on the server) and creates dbName as a
// fresh, empty database. Distinct from CreateSchema: pg_dump/pg_restore (P9 dump/restore) operate
// at whole-database granularity, so testing them needs a dedicated throwaway database, not just
// an isolated schema within one shared database.
func CreateDatabase(ctx context.Context, dsn, dbName string) error {
	if err := AssertNotProduction(dbName, ""); err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("testdb: connect to %s: %w", dsn, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	ident := pgx.Identifier{dbName}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		return fmt.Errorf("testdb: create database %s: %w", dbName, err)
	}
	return nil
}

// DropDatabase connects to dsn and drops dbName, forcibly disconnecting any remaining sessions
// (WITH (FORCE), PG13+) so a test's own pooled connections never block cleanup. It re-checks the
// guard itself so a caller can never be tricked into dropping a reserved database.
func DropDatabase(ctx context.Context, dsn, dbName string) error {
	if err := AssertNotProduction(dbName, ""); err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("testdb: connect to %s: %w", dsn, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	ident := pgx.Identifier{dbName}.Sanitize()
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)"); err != nil {
		return fmt.Errorf("testdb: drop database %s: %w", dbName, err)
	}
	return nil
}

// WithDatabase returns dsn with its database name (the URL path) replaced by dbName — used to
// build a connection string for a freshly created isolated database (CreateDatabase) from an
// existing admin DSN such as GS_TEST_DATABASE_URL.
func WithDatabase(dsn, dbName string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("testdb: parse dsn: %w", err)
	}
	u.Path = "/" + dbName
	return u.String(), nil
}
