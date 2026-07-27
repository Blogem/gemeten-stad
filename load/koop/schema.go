package koop

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Target and staging table names (unqualified). Every use of these below is routed through
// qualify(schema, ...) — never referenced bare in a SQL statement — so an unqualified name can
// never fall through search_path into another schema.
const (
	publicationsTable        = "koop_publications"
	publicationsStagingTable = "koop_publications_staging"
)

// targetTables lists every table the koop load owns (targets only), in the order dropTargets
// drops them.
var targetTables = []string{publicationsTable}

// qualify returns a schema-qualified, sanitized table identifier, so an unqualified name can never
// fall through search_path into another schema (e.g. a test's search_path=<test>,public must never
// let a DROP/CREATE reach public's real tables).
func qualify(schema, table string) string {
	return pgx.Identifier{schema, table}.Sanitize()
}

// loadSchema resolves the schema the load operates in: the current search_path schema — public in
// production, the isolated schema under the integration harness.
func loadSchema(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var s string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&s); err != nil {
		return "", fmt.Errorf("koop: resolve current schema: %w", err)
	}
	if s == "" {
		return "", fmt.Errorf("koop: current_schema() is empty (no schema on search_path)")
	}
	return s, nil
}

// ensureExtensions creates the extension the load depends on: postgis for the geom column.
func ensureExtensions(ctx context.Context, pool *pgxpool.Pool) error {
	const stmt = `CREATE EXTENSION IF NOT EXISTS postgis;`
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("koop: ensure extensions: %w", err)
	}
	return nil
}

// dropTargets drops the target tables (--reset only), so ensureSchema rebuilds them clean from the
// next load's staged data. It does NOT touch the *_staging table — that is truncated and reloaded
// on every stage call regardless of Reset (see stage.go).
func dropTargets(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	for _, table := range targetTables {
		stmt := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE;", qualify(schema, table))
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("koop: reset: drop %s: %w", table, err)
		}
	}
	return nil
}

// ensureSchema creates the koop_publications target and staging tables (CREATE TABLE IF NOT
// EXISTS) if not already present. One row per landed publication (aanvraag/besluit/ontwerpbesluit/
// verlenging/ingetrokken/other), keyed by the SRU record's own gmb_id. The resolved_* columns and
// unresolved are populated only for the audited besluit in a trail (design.md D8) — every other
// publication row in the trail carries them as NULL/false, since only the besluit is actually
// resolved against a Place (see PublicationRow in stage.go). geom is RD/EPSG:28992, matching every
// other koop coordinate — NOT 4326 (load/bomen's SRID).
//
// raw is the change-detection anchor: it snapshots the publication's own fields (not its
// resolution — the resolved_* columns are compared separately by upsertPublications' MERGE), so any
// source field this schema doesn't model explicitly is never silently dropped.
//
// loaded_at (target table only — never staged) records when the row's content was last written by
// upsertPublications' MERGE; it is excluded from change-detection so idempotent re-runs don't touch
// it (see publicationsMergeSQL).
func ensureSchema(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	stmt := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    gmb_id                  text PRIMARY KEY,
    zaaknummer              text,
    kind                    text,
    available               date,
    geom                    geometry(Point, 28992),
    postcode                text,
    huisnummer              integer,
    resolved_identificatie  text,
    resolved_buurt_code     text,
    resolved_confidence     double precision,
    resolved_geom           geometry(Point, 28992),
    resolved_tier           text,
    caveats                 text[],
    in_noord                boolean,
    unresolved              boolean NOT NULL DEFAULT false,
    raw                     jsonb NOT NULL,
    loaded_at               timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS %s (
    gmb_id                  text,
    zaaknummer              text,
    kind                    text,
    available               date,
    geom                    geometry(Point, 28992),
    postcode                text,
    huisnummer              integer,
    resolved_identificatie  text,
    resolved_buurt_code     text,
    resolved_confidence     double precision,
    resolved_geom           geometry(Point, 28992),
    resolved_tier           text,
    caveats                 text[],
    in_noord                boolean,
    unresolved              boolean NOT NULL DEFAULT false,
    raw                     jsonb NOT NULL
);
`,
		qualify(schema, publicationsTable),
		qualify(schema, publicationsStagingTable),
	)
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("koop: ensure schema: %w", err)
	}
	return nil
}
