package coverage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// auditMetricsTable/auditMetricsStagingTable are the target and staging table names (unqualified —
// every use below is routed through qualify(schema, ...), mirroring load/bomen/schema.go, so an
// unqualified name can never fall through search_path into another schema).
const (
	auditMetricsTable        = "audit_metrics"
	auditMetricsStagingTable = "audit_metrics_staging"
)

// qualify returns a schema-qualified, sanitized table identifier (mirrors load/bomen/schema.go's
// qualify), so an unqualified name can never fall through search_path into another schema (e.g. a
// test's search_path=<test>,public must never let a DROP/CREATE reach public's real tables).
func qualify(schema, table string) string {
	return pgx.Identifier{schema, table}.Sanitize()
}

// Metric is one permit's derived coverage numbers (design.md D8) — the value-store counterpart to
// the graph's anchor/period/Observation. It carries no confidence/granularity/caveat (the link,
// which lives in the graph) and no geometry (which lives in kapenherplant).
type Metric struct {
	Zaaknummer           string
	Matched              bool
	AssignedFellingIDs   []string
	AssignedFellingCount int
	CandidateCount       int
	NearestDistM         *float64 // nil -> SQL NULL (no resolved point, or no candidate)
	RunID                string
}

// EnsureAuditMetrics creates the audit_metrics target table and its staging twin (CREATE TABLE IF
// NOT EXISTS), mirroring load/bomen/schema.go's ensureSchema. Idempotent: safe to call on every
// derive run.
func EnsureAuditMetrics(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	stmt := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    zaaknummer             text PRIMARY KEY,
    matched                boolean NOT NULL,
    assigned_felling_ids   text[] NOT NULL,
    assigned_felling_count int NOT NULL,
    candidate_count        int NOT NULL,
    nearest_dist_m         double precision,
    run_id                 text NOT NULL
);

CREATE TABLE IF NOT EXISTS %s (
    zaaknummer             text,
    matched                boolean,
    assigned_felling_ids   text[],
    assigned_felling_count int,
    candidate_count        int,
    nearest_dist_m         double precision,
    run_id                 text
);
`,
		qualify(schema, auditMetricsTable),
		qualify(schema, auditMetricsStagingTable),
	)
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("coverage: ensure audit_metrics schema: %w", err)
	}
	return nil
}

// DropAuditMetrics drops the audit_metrics target table (--reset only), mirroring
// load/bomen/schema.go's dropTargets — it does NOT touch the staging table, which is truncated and
// reloaded on every UpsertAuditMetrics call regardless of reset.
func DropAuditMetrics(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	stmt := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE;", qualify(schema, auditMetricsTable))
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("coverage: reset: drop audit_metrics: %w", err)
	}
	return nil
}

// auditMetricsMergeSQL builds the MERGE statement upserting the staging table's rows into the
// target, keyed by zaaknummer. A matched-but-unchanged row satisfies neither WHEN clause and is
// left untouched — re-upserting identical rows is a no-op (design.md D8/task 4.5's idempotency
// requirement), mirroring load/bomen/upsert.go's mergeSQL "matched AND changed" pattern (there is
// no soft-delete concept here, so there is no NOT MATCHED BY SOURCE clause: a permit's current
// derive-run numbers simply replace its prior ones).
func auditMetricsMergeSQL(schema string) string {
	target := qualify(schema, auditMetricsTable)
	source := qualify(schema, auditMetricsStagingTable)

	return fmt.Sprintf(`
MERGE INTO %s AS t
USING %s AS s
ON t.zaaknummer = s.zaaknummer
WHEN MATCHED AND (
  t.matched IS DISTINCT FROM s.matched OR
  t.assigned_felling_ids IS DISTINCT FROM s.assigned_felling_ids OR
  t.assigned_felling_count IS DISTINCT FROM s.assigned_felling_count OR
  t.candidate_count IS DISTINCT FROM s.candidate_count OR
  t.nearest_dist_m IS DISTINCT FROM s.nearest_dist_m OR
  t.run_id IS DISTINCT FROM s.run_id
) THEN
  UPDATE SET
    matched = s.matched,
    assigned_felling_ids = s.assigned_felling_ids,
    assigned_felling_count = s.assigned_felling_count,
    candidate_count = s.candidate_count,
    nearest_dist_m = s.nearest_dist_m,
    run_id = s.run_id
WHEN NOT MATCHED THEN
  INSERT (zaaknummer, matched, assigned_felling_ids, assigned_felling_count, candidate_count, nearest_dist_m, run_id)
  VALUES (s.zaaknummer, s.matched, s.assigned_felling_ids, s.assigned_felling_count, s.candidate_count, s.nearest_dist_m, s.run_id);
`, target, source)
}

// UpsertAuditMetrics reconciles rows into audit_metrics in one transaction: truncate the staging
// table, insert rows into it, then MERGE staging into the target — mirroring
// load/bomen/upsert.go's upsertAll, so either the whole reconciliation succeeds or none of it does.
func UpsertAuditMetrics(ctx context.Context, pool *pgxpool.Pool, schema string, rows []Metric) error {
	stagingTable := qualify(schema, auditMetricsStagingTable)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("coverage: upsert audit_metrics: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", stagingTable)); err != nil {
		return fmt.Errorf("coverage: upsert audit_metrics: truncate staging: %w", err)
	}

	insertSQL := fmt.Sprintf(`
INSERT INTO %s
    (zaaknummer, matched, assigned_felling_ids, assigned_felling_count, candidate_count, nearest_dist_m, run_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)`, stagingTable)

	for _, row := range rows {
		assignedFellingIDs := row.AssignedFellingIDs
		if assignedFellingIDs == nil {
			// Defensive: audit_metrics.assigned_felling_ids is NOT NULL, and pgx
			// stages a nil []string as SQL NULL, not "{}" — coerce any caller's
			// nil slice to an empty one so the MERGE's INSERT never violates the
			// constraint (belt-and-braces alongside coverage.go's own nil-guard).
			assignedFellingIDs = []string{}
		}
		if _, err := tx.Exec(ctx, insertSQL,
			row.Zaaknummer,
			row.Matched,
			assignedFellingIDs,
			row.AssignedFellingCount,
			row.CandidateCount,
			row.NearestDistM,
			row.RunID,
		); err != nil {
			return fmt.Errorf("coverage: upsert audit_metrics: stage row zaaknummer=%s: %w", row.Zaaknummer, err)
		}
	}

	if _, err := tx.Exec(ctx, auditMetricsMergeSQL(schema)); err != nil {
		return fmt.Errorf("coverage: upsert audit_metrics: merge: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("coverage: upsert audit_metrics: commit: %w", err)
	}
	return nil
}
