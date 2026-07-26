package bomen

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Target and staging table names (source dataset names, unqualified).
const (
	kapenherplantTable        = "kapenherplant"
	kapenherplantStagingTable = "kapenherplant_staging"
	stamgegevensTable         = "stamgegevens"
	stamgegevensStagingTable  = "stamgegevens_staging"
)

// targetTables lists every table the bomen load owns (targets only — the kapenherplant <->
// stamgegevens join is resolved in Go by resolveJoin, not a DB-level FK), in the order dropTargets
// drops them.
var targetTables = []string{kapenherplantTable, stamgegevensTable}

// ensureExtensions creates the extension the load depends on: postgis for the loaded/resolved
// geometry columns.
func ensureExtensions(ctx context.Context, pool *pgxpool.Pool) error {
	const stmt = `CREATE EXTENSION IF NOT EXISTS postgis;`
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("bomen: ensure extensions: %w", err)
	}
	return nil
}

// dropTargets drops the target tables (--reset only), so ensureSchema rebuilds them clean from
// the next load's staged data. It does NOT touch the *_staging tables — those are truncated and
// reloaded on every stage call regardless of Reset (see stage.go).
func dropTargets(ctx context.Context, pool *pgxpool.Pool) error {
	for _, table := range targetTables {
		stmt := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE;", table)
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("bomen: reset: drop %s: %w", table, err)
		}
	}
	return nil
}

// ensureSchema creates the kapenherplant/stamgegevens target and staging tables (CREATE TABLE IF
// NOT EXISTS) if not already present. Columns are grouped as: (1) the source's own natural key and
// the documented columns (docs/DATA_SOURCES.md §2a), typed explicitly; (2) a "raw" jsonb column
// holding the full landed source row verbatim, so a source column absent from group (1) is never
// silently dropped; (3) on target tables only, the load-owned columns (source_deleted_at, and —
// kapenherplant only — resolvedGeom/resolvedVia per design.md D3/D4), set exclusively by
// resolveJoin/upsertAll's soft-delete clause, never sourced from staging.
func ensureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	const stmt = `
CREATE TABLE IF NOT EXISTS kapenherplant (
    id                              text PRIMARY KEY,
    "boomId"                        text,
    "boomNieuwId"                   text,
    "gbdBuurtId"                    text,
    "dichtstbijzijndeBagAdres"      text,
    postcode                        text,
    soortnaam                       text,
    "toeTePassenBoomsoort"          text,
    "datumVergunningsaanvraag"      timestamptz,
    "kapmaatregelDatumUitgevoerd"   timestamptz,
    "plantmaatregelDatumUitgevoerd" timestamptz,
    "resolvedGeom"                  geometry(Point, 4326),
    "resolvedVia"                   text,
    source_deleted_at               timestamptz,
    raw                             jsonb NOT NULL
);

CREATE TABLE IF NOT EXISTS kapenherplant_staging (
    id                              text,
    "boomId"                        text,
    "boomNieuwId"                   text,
    "gbdBuurtId"                    text,
    "dichtstbijzijndeBagAdres"      text,
    postcode                        text,
    soortnaam                       text,
    "toeTePassenBoomsoort"          text,
    "datumVergunningsaanvraag"      timestamptz,
    "kapmaatregelDatumUitgevoerd"   timestamptz,
    "plantmaatregelDatumUitgevoerd" timestamptz,
    raw                             jsonb NOT NULL
);

CREATE TABLE IF NOT EXISTS stamgegevens (
    id                 text PRIMARY KEY,
    "gbdBuurtId"       text,
    soortnaam          text,
    geometrie          geometry(Point, 4326),
    source_deleted_at  timestamptz,
    raw                jsonb NOT NULL
);

CREATE TABLE IF NOT EXISTS stamgegevens_staging (
    id           text,
    "gbdBuurtId" text,
    soortnaam    text,
    geometrie    geometry(Point, 4326),
    raw          jsonb NOT NULL
);
`
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("bomen: ensure schema: %w", err)
	}
	return nil
}
