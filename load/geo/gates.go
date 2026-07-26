package geo

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// expectedNoordBuurten and expectedNoordWijken are the Spike D verified anchor: stadsdeel Noord's
// ground truth must be present in the whole-city load. Noord buurten/wijken are identified by
// gebieden's own code convention: code LIKE 'N%'.
const (
	expectedNoordBuurten = 69
	expectedNoordWijken  = 15
	expectedSRID         = 28992
)

// geomTables lists every table the SRID gate checks — every table the geo load populates a
// geometry column for.
var geomTables = []string{
	"bag_verblijfsobject",
	"bag_ligplaats",
	"bag_standplaats",
	"gebieden_buurten",
	"gebieden_wijken",
	"cbs_buurten",
}

// runGates asserts the post-load sanity gates and logs whole-city row counts. It returns a
// non-nil error (the caller exits non-zero) if any gate is not met — these gates exist because a
// wrong SRID or a missing Noord ground truth silently breaks point-in-polygon resolution instead
// of failing loudly.
func runGates(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	if err := gateSRID(ctx, pool, schema); err != nil {
		return err
	}
	if err := gateNoordGroundTruth(ctx, pool, schema); err != nil {
		return err
	}
	if err := logRowCounts(ctx, pool, schema); err != nil {
		return err
	}
	return nil
}

// gateSRID asserts every loaded geometry (across every geom-bearing table) shares a single SRID,
// and that it is 28992 (RD) — a mismatched SRID makes ST_Contains silently return nothing.
func gateSRID(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	selects := make([]string, len(geomTables))
	for i, table := range geomTables {
		selects[i] = fmt.Sprintf("SELECT ST_SRID(geom) AS srid FROM %s WHERE geom IS NOT NULL", qualify(schema, table))
	}
	stmt := "SELECT DISTINCT srid FROM (" + strings.Join(selects, " UNION ALL ") + ") s;"

	rows, err := pool.Query(ctx, stmt)
	if err != nil {
		return fmt.Errorf("geo: gate: query distinct SRIDs: %w", err)
	}
	defer rows.Close()

	var srids []int
	for rows.Next() {
		var srid int
		if err := rows.Scan(&srid); err != nil {
			return fmt.Errorf("geo: gate: scan SRID: %w", err)
		}
		srids = append(srids, srid)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("geo: gate: read SRIDs: %w", err)
	}

	if len(srids) != 1 || srids[0] != expectedSRID {
		return fmt.Errorf("geo: gate FAILED: expected a single distinct SRID %d across loaded geometry, got %v", expectedSRID, srids)
	}
	return nil
}

// gateNoordGroundTruth asserts the Spike D verified anchor: 69 Noord buurten and 15 Noord wijken
// present among the loaded whole-city polygons (soft-deleted rows excluded — they are no longer
// "present").
func gateNoordGroundTruth(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	buurten, err := countWhere(ctx, pool, schema, "gebieden_buurten", "code LIKE 'N%' AND source_deleted_at IS NULL")
	if err != nil {
		return fmt.Errorf("geo: gate: count Noord buurten: %w", err)
	}
	if buurten != expectedNoordBuurten {
		return fmt.Errorf("geo: gate FAILED: expected %d Noord buurten (code LIKE 'N%%'), got %d", expectedNoordBuurten, buurten)
	}

	wijken, err := countWhere(ctx, pool, schema, "gebieden_wijken", "code LIKE 'N%' AND source_deleted_at IS NULL")
	if err != nil {
		return fmt.Errorf("geo: gate: count Noord wijken: %w", err)
	}
	if wijken != expectedNoordWijken {
		return fmt.Errorf("geo: gate FAILED: expected %d Noord wijken (code LIKE 'N%%'), got %d", expectedNoordWijken, wijken)
	}
	return nil
}

// logRowCounts logs whole-city row counts (including soft-deleted rows) for every target table,
// for operator visibility after a load.
func logRowCounts(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	for _, table := range targetTables {
		count, err := countWhere(ctx, pool, schema, table, "")
		if err != nil {
			return fmt.Errorf("geo: log row count for %s: %w", table, err)
		}
		log.Printf("geo: %s: %d rows", table, count)
	}
	return nil
}

// countWhere returns count(*) from schema.table, optionally filtered by a raw WHERE clause (empty
// = unfiltered). table and where are always internal constants, never user input.
func countWhere(ctx context.Context, pool *pgxpool.Pool, schema, table, where string) (int, error) {
	stmt := "SELECT count(*) FROM " + qualify(schema, table)
	if where != "" {
		stmt += " WHERE " + where
	}
	var count int
	if err := pool.QueryRow(ctx, stmt).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
