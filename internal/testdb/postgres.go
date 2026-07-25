package testdb

import (
	"context"
	"fmt"

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
