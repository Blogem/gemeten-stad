package testdb

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AssertIsolatedSchema verifies that pool's connections resolve
// current_schema() to wantSchema and that wantSchema is not a reserved
// production/dev name — a runtime complement to AssertNotProduction that
// catches a search_path which would let unqualified DDL fall through to
// production data. Call it right after building an isolated-schema pool,
// before any destructive op.
func AssertIsolatedSchema(ctx context.Context, pool *pgxpool.Pool, wantSchema string) error {
	if err := AssertNotProduction(wantSchema, ""); err != nil {
		return err
	}

	var gotSchema string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&gotSchema); err != nil {
		return fmt.Errorf("testdb: query current_schema(): %w", err)
	}
	if gotSchema != wantSchema {
		return fmt.Errorf("testdb: connection's current_schema() = %q, expected the isolated schema %q — search_path is not isolated", gotSchema, wantSchema)
	}
	return nil
}
