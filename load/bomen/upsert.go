package bomen

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// column is one target column copied from the matching *_staging column of the same name. cast,
// when non-empty, is the Postgres type the staging value is cast to on the way in — staging
// columns here are already typed to match their targets (stage.go inserts typed values, not raw
// ogr2ogr text), so no upsertSpec below needs a cast; the field exists to mirror load/geo's
// upsertSpec shape exactly.
type column struct {
	name string
	cast string
}

// upsertSpec describes one target/staging table pair for the MERGE upsert: the identity key used
// to match a staging row to its target row, and the full column list (including the key) to
// insert for a new row. resolvedGeom/resolvedVia and source_deleted_at are deliberately excluded —
// they are load-owned columns the upsert itself never sources from staging (resolveJoin and the
// MERGE's own soft-delete clause manage those separately).
type upsertSpec struct {
	target string
	keys   []string
	cols   []column
}

// upsertSpecs is the pinned load/bomen upsert contract: both kapenherplant and stamgegevens keyed
// by the source's own natural row id (no voorkomens here, unlike BAG — see design.md D3, there is
// no valid-time versioning to preserve for either sub-dataset).
var upsertSpecs = []upsertSpec{
	{
		target: kapenherplantTable,
		keys:   []string{"id"},
		cols: []column{
			{"id", ""},
			{"boomId", ""},
			{"boomNieuwId", ""},
			{"gbdBuurtId", ""},
			{"dichtstbijzijndeBagAdres", ""},
			{"postcode", ""},
			{"soortnaam", ""},
			{"toeTePassenBoomsoort", ""},
			{"datumVergunningsaanvraag", ""},
			{"kapmaatregelDatumUitgevoerd", ""},
			{"plantmaatregelDatumUitgevoerd", ""},
			{"raw", ""},
		},
	},
	{
		target: stamgegevensTable,
		keys:   []string{"id"},
		cols: []column{
			{"id", ""},
			{"gbdBuurtId", ""},
			{"soortnaam", ""},
			{"geometrie", ""},
			{"raw", ""},
		},
	},
}

// mergeSQL builds the MERGE statement for spec. It takes a single $1 parameter, the load
// timestamp. Unlike load/geo's voorkomen identity (immutable once recorded), bomen's `id` is a
// MUTABLE key — fields such as plantmaatregelDatumUitgevoerd fill in over time on the same row —
// so a matched row must be refreshed when its content actually changed, not just left alone. The
// WHEN clauses below are listed in the order Postgres MERGE evaluates them (first match wins per
// row):
//  1. matched AND raw changed: refresh every non-key column from staging and revive
//     (source_deleted_at = NULL) in one step, covering both a plain content update and a changed
//     row that had been soft-deleted and reappeared.
//  2. matched, raw unchanged, but previously soft-deleted: revive only.
//  3. not matched: insert the new row.
//  4. not matched by source, not already soft-deleted: soft-delete, stamping source_deleted_at =
//     $1 — guarded by "AND t.source_deleted_at IS NULL" so re-running against an unchanged export
//     never re-touches an already soft-deleted row.
//
// A matched row that is unchanged and not soft-deleted satisfies none of the MATCHED clauses, so
// it is left untouched — the no-op requirement for a re-run against the same export holds.
func mergeSQL(schema string, spec upsertSpec) string {
	keySet := make(map[string]struct{}, len(spec.keys))
	for _, k := range spec.keys {
		keySet[k] = struct{}{}
	}

	on := make([]string, len(spec.keys))
	for i, k := range spec.keys {
		ident := pgx.Identifier{k}.Sanitize()
		on[i] = fmt.Sprintf("t.%s = s.%s", ident, ident)
	}

	names := make([]string, len(spec.cols))
	values := make([]string, len(spec.cols))
	for i, c := range spec.cols {
		ident := pgx.Identifier{c.name}.Sanitize()
		names[i] = ident
		if c.cast != "" {
			values[i] = fmt.Sprintf("s.%s::%s", ident, c.cast)
		} else {
			values[i] = "s." + ident
		}
	}

	var refreshSets []string
	for _, c := range spec.cols {
		if _, isKey := keySet[c.name]; isKey {
			continue
		}
		ident := pgx.Identifier{c.name}.Sanitize()
		value := "s." + ident
		if c.cast != "" {
			value = fmt.Sprintf("s.%s::%s", ident, c.cast)
		}
		refreshSets = append(refreshSets, fmt.Sprintf("%s = %s", ident, value))
	}
	refreshSets = append(refreshSets, "source_deleted_at = NULL")

	target := qualify(schema, spec.target)
	source := qualify(schema, spec.target+"_staging")

	return fmt.Sprintf(`
MERGE INTO %s AS t
USING %s AS s
ON %s
WHEN MATCHED AND t.raw IS DISTINCT FROM s.raw THEN
  UPDATE SET %s
WHEN MATCHED AND t.source_deleted_at IS NOT NULL THEN
  UPDATE SET source_deleted_at = NULL
WHEN NOT MATCHED THEN
  INSERT (%s) VALUES (%s)
WHEN NOT MATCHED BY SOURCE AND t.source_deleted_at IS NULL THEN
  UPDATE SET source_deleted_at = $1;
`, target, source, strings.Join(on, " AND "), strings.Join(refreshSets, ", "), strings.Join(names, ", "), strings.Join(values, ", "))
}

// upsertAll runs every upsertSpec's MERGE in a single transaction against one load timestamp, so
// the whole reload is atomic: either both targets reconcile against their staging snapshot, or
// neither does.
func upsertAll(ctx context.Context, pool *pgxpool.Pool, schema string, loadTS time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("bomen: upsert: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, spec := range upsertSpecs {
		if _, err := tx.Exec(ctx, mergeSQL(schema, spec), loadTS); err != nil {
			return fmt.Errorf("bomen: upsert %s: %w", spec.target, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("bomen: upsert: commit: %w", err)
	}
	return nil
}
