package geo

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Blogem/gemeten-stad/ingest/shared"
	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultGDALPGConn is the sidecar's own Postgres connection string — ogr2ogr runs inside the
// gdal container, on the compose network, so it addresses Postgres as "db", not through whatever
// host/port the Go process's own pgx DSN (GS_DATABASE_URL) uses.
const defaultGDALPGConn = "PG:host=db port=5432 dbname=gemeten_stad user=gs password=gs"

// Config configures a geo Load run.
type Config struct {
	// Reset drops and rebuilds the target tables from the landed data (clean dev rebuild).
	// Absent, Load is the non-destructive upsert.
	Reset bool
	// PGConn is the sidecar's ogr2ogr Postgres connection string (GS_GDAL_PG_CONN), distinct
	// from the pgx DSN the Load caller itself connects with.
	PGConn string
}

// PGConnFromEnv resolves GS_GDAL_PG_CONN from env, falling back to the compose-network default
// (env is injected for testability — pass os.Getenv at the call site).
func PGConnFromEnv(env func(string) string) string {
	if conn := env("GS_GDAL_PG_CONN"); conn != "" {
		return conn
	}
	return defaultGDALPGConn
}

// Load runs the full geo backbone load: ensure extensions + target schema (rebuilding it first
// if cfg.Reset), stage BAG + gebieden + CBS via the GDAL sidecar into *_staging tables, upsert
// staging into targets (soft-deleting rows absent from the fresh staging snapshot), build the
// resolver indexes, and assert the post-load sanity gates. It returns a non-nil error — the
// caller should exit non-zero — if any step, including a sanity gate, fails.
func Load(ctx context.Context, pool *pgxpool.Pool, sc *shared.Sidecar, store *shared.RawStore, cfg Config) error {
	if err := ensureExtensions(ctx, pool); err != nil {
		return err
	}

	// Resolve the schema every subsequent DDL/DML statement is qualified against, once per Load,
	// instead of relying on the connection's search_path at each call site (see qualify/
	// loadSchema in schema.go). In production this resolves to "public"; under an integration
	// harness with search_path = <test_schema>, public, it confines every table op — including
	// the --reset drop — to the isolated test schema.
	schema, err := loadSchema(ctx, pool)
	if err != nil {
		return err
	}

	if cfg.Reset {
		if err := dropTargets(ctx, pool, schema); err != nil {
			return err
		}
	}

	if err := ensureSchema(ctx, pool, schema); err != nil {
		return err
	}

	pgConn := cfg.PGConn
	if pgConn == "" {
		pgConn = defaultGDALPGConn
	}

	if err := stageBAG(ctx, pool, sc, store, pgConn, schema); err != nil {
		return fmt.Errorf("geo: load: %w", err)
	}
	if err := stagePolygons(ctx, pool, sc, pgConn, schema); err != nil {
		return fmt.Errorf("geo: load: %w", err)
	}

	loadTS := time.Now().UTC()
	if err := upsertAll(ctx, pool, schema, loadTS); err != nil {
		return fmt.Errorf("geo: load: %w", err)
	}

	// CBS is a best-effort cross-reference (see cbsSpec): reconciled in its own transaction and
	// never allowed to fail the BAG + gebieden backbone. Log and continue on error.
	if err := upsertCBS(ctx, pool, schema, loadTS); err != nil {
		log.Printf("geo: load: %v (best-effort, non-fatal — continuing)", err)
	}

	if err := ensureIndexes(ctx, pool, schema); err != nil {
		return fmt.Errorf("geo: load: %w", err)
	}

	if err := runGates(ctx, pool, schema); err != nil {
		return fmt.Errorf("geo: load: %w", err)
	}

	return nil
}
