package bomen

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Blogem/gemeten-stad/ingest/shared"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Landed artifact names load/bomen expects ingest/bomen to have already landed under the shared
// raw store (bronze) via shared.RawStore.LandVersion — the seam between the two packages. They
// double as the DSO API's own _embedded key (docs/DATA_SOURCES.md §2a), since ingest/bomen lands
// each dataset's own name.
const (
	ArtifactKapenherplant = "kapenherplant"
	ArtifactStamgegevens  = "stamgegevens"
)

// Config configures a bomen Load run.
type Config struct {
	// Reset drops and rebuilds the target tables from the landed data (clean dev rebuild).
	// Absent, Load is the non-destructive upsert.
	Reset bool
}

// Load runs the full bomen backbone load: ensure extensions + target schema (rebuilding it first
// if cfg.Reset), read the landed kapenherplant/stamgegevens exports, stage each into its
// *_staging table, upsert staging into targets (soft-deleting rows absent from the fresh staging
// snapshot), materialize the kapenherplant -> stamgegevens point resolution, and log the resulting
// row counts. It returns a non-nil error — the caller should exit non-zero — if any step fails.
func Load(ctx context.Context, pool *pgxpool.Pool, store *shared.RawStore, cfg Config) error {
	if err := ensureExtensions(ctx, pool); err != nil {
		return err
	}

	if cfg.Reset {
		if err := dropTargets(ctx, pool); err != nil {
			return err
		}
	}

	if err := ensureSchema(ctx, pool); err != nil {
		return err
	}

	kapRows, err := readLandedRows(store, ArtifactKapenherplant, ArtifactKapenherplant)
	if err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}
	stamRows, err := readLandedRows(store, ArtifactStamgegevens, ArtifactStamgegevens)
	if err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}

	if err := stageKapenherplant(ctx, pool, kapRows); err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}
	if err := stageStamgegevens(ctx, pool, stamRows); err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}

	loadTS := time.Now().UTC()
	if err := upsertAll(ctx, pool, loadTS); err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}

	if err := resolveJoin(ctx, pool); err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}

	return logRowCounts(ctx, pool)
}

// logRowCounts logs the resulting kapenherplant/stamgegevens target row counts after a load. The
// full-city targets are 35,202 kapenherplant / 323,728 stamgegevens rows (design.md), but this
// logs whatever count the current data actually produced — never a hardcoded expectation.
func logRowCounts(ctx context.Context, pool *pgxpool.Pool) error {
	for _, table := range targetTables {
		count, err := countRows(ctx, pool, table)
		if err != nil {
			return fmt.Errorf("bomen: log row count for %s: %w", table, err)
		}
		log.Printf("bomen: %s: %d rows", table, count)
	}
	return nil
}

func countRows(ctx context.Context, pool *pgxpool.Pool, table string) (int, error) {
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
