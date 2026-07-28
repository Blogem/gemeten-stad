package bomen

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Blogem/gemeten-stad/ingest/shared"
	loadgraph "github.com/Blogem/gemeten-stad/load/graph"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Landed artifact names load/bomen expects ingest/bomen to have already landed under the shared
// raw store (bronze) via shared.RawStore.LandVersion — the seam between the two packages. These
// values must match ingest/bomen.Artifact* exactly (load/bomen deliberately does not import
// ingest/bomen for them — see load/geo's own "re-declare matching-value consts" pattern — but
// contract_test.go guards the two from drifting apart).
const (
	artifactKapenherplant = "bomen_kapenherplant"
	artifactStamgegevens  = "bomen_stamgegevens"
)

// embedKey* are the Datapunt bomen DSO API's own _embedded keys (docs/DATA_SOURCES.md §2a) — the
// key each landed page's HAL envelope nests its dataset's rows under. Distinct from the landing
// artifact name above: the artifact is how/where the data is stored on disk, the embed key is a
// property of the source API's response shape.
const (
	embedKeyKapenherplant = "kapenherplant"
	embedKeyStamgegevens  = "stamgegevens"
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
// snapshot), materialize the kapenherplant -> stamgegevens point resolution, project the felled
// trees + their felling events into the RDF graph through the SHACL-gated graph loader
// (model-felled-trees D1/D4), and log the resulting row counts. It returns a non-nil error — the
// caller should exit non-zero — if any step fails.
//
// fusekiURL is only used for the graph projection step; Reset there is always false — a bomen-only
// --reset (cfg.Reset) governs the PostGIS targets alone and must never clear other load stages'
// run graphs in Fuseki (mirrors load/koop.Load's identical rationale for its own graph write).
func Load(ctx context.Context, pool *pgxpool.Pool, store *shared.RawStore, fusekiURL string, cfg Config) error {
	if err := ensureExtensions(ctx, pool); err != nil {
		return err
	}

	// Resolve the schema this load operates in once, and thread it into every table op below, so
	// an unqualified table name can never fall through search_path into another schema (e.g. the
	// integration harness's search_path=<test schema>,public must never let this load reach
	// public's real tables).
	schema, err := loadSchema(ctx, pool)
	if err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}

	if cfg.Reset {
		if err := dropTargets(ctx, pool, schema); err != nil {
			return err
		}
	}

	if err := ensureSchema(ctx, pool, schema); err != nil {
		return err
	}

	kapRows, err := readLandedRows(store, artifactKapenherplant, embedKeyKapenherplant)
	if err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}
	stamRows, err := readLandedRows(store, artifactStamgegevens, embedKeyStamgegevens)
	if err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}

	if err := stageKapenherplant(ctx, pool, schema, kapRows); err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}
	if err := stageStamgegevens(ctx, pool, schema, stamRows); err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}

	loadTS := time.Now().UTC()
	if err := upsertAll(ctx, pool, schema, loadTS); err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}

	if err := resolveJoin(ctx, pool, schema); err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}

	if err := loadFelledGraph(ctx, pool, schema, fusekiURL); err != nil {
		return fmt.Errorf("bomen: load: %w", err)
	}

	return logRowCounts(ctx, pool, schema)
}

// loadFelledGraph queries the felled kapenherplant rows, assembles them into a Turtle candidate,
// and writes it through the SHACL-gated graph loader (load/graph.Load) with Reset: false — the
// bomen graph projection never clears other run graphs; idempotency comes from the load gate's
// SCD2 signature (design.md, load/koop.Load's identical Reset rationale). A row skipped by
// buildFelledCandidate (unsafe IRI or missing date) is logged, not fatal — mirrors
// load/koop.Load's graphSkipped handling. An empty candidate (no felled rows, or every row
// skipped) is a clean no-op: the graph write is skipped entirely.
func loadFelledGraph(ctx context.Context, pool *pgxpool.Pool, schema, fusekiURL string) error {
	rows, err := queryFelledRows(ctx, pool, schema)
	if err != nil {
		return err
	}

	candidate, skipped := buildFelledCandidate(rows)
	for _, id := range skipped {
		log.Printf("bomen: felling %s skipped from graph (empty boomId, unsafe IRI, or missing felling date)", id)
	}
	if len(candidate) == 0 {
		return nil
	}

	if err := loadgraph.Load(ctx, fusekiURL, candidate, loadgraph.Config{Reset: false}); err != nil {
		return fmt.Errorf("load felled graph: %w", err)
	}
	return nil
}

// logRowCounts logs the resulting kapenherplant/stamgegevens target row counts after a load. The
// full-city targets are 35,202 kapenherplant / 323,728 stamgegevens rows (design.md), but this
// logs whatever count the current data actually produced — never a hardcoded expectation.
func logRowCounts(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	for _, table := range targetTables {
		count, err := countRows(ctx, pool, schema, table)
		if err != nil {
			return fmt.Errorf("bomen: log row count for %s: %w", table, err)
		}
		log.Printf("bomen: %s: %d rows", table, count)
	}
	return nil
}

func countRows(ctx context.Context, pool *pgxpool.Pool, schema, table string) (int, error) {
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+qualify(schema, table)).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
