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
// timestamp. Rows present in staging but not the target are inserted; rows matched by id are left
// alone unless they need un-soft-deleting (a previously soft-deleted row reappearing in a fresh
// export); target rows absent from staging are soft-deleted by stamping source_deleted_at = $1,
// guarded by "AND t.source_deleted_at IS NULL" so re-running against an unchanged export never
// re-touches an already soft-deleted row (the no-op requirement).
func mergeSQL(spec upsertSpec) string {
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

	source := spec.target + "_staging"

	return fmt.Sprintf(`
MERGE INTO %s AS t
USING %s AS s
ON %s
WHEN NOT MATCHED THEN
  INSERT (%s) VALUES (%s)
WHEN MATCHED AND t.source_deleted_at IS NOT NULL THEN
  UPDATE SET source_deleted_at = NULL
WHEN NOT MATCHED BY SOURCE AND t.source_deleted_at IS NULL THEN
  UPDATE SET source_deleted_at = $1;
`, spec.target, source, strings.Join(on, " AND "), strings.Join(names, ", "), strings.Join(values, ", "))
}

// upsertAll runs every upsertSpec's MERGE in a single transaction against one load timestamp, so
// the whole reload is atomic: either both targets reconcile against their staging snapshot, or
// neither does.
func upsertAll(ctx context.Context, pool *pgxpool.Pool, loadTS time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("bomen: upsert: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, spec := range upsertSpecs {
		if _, err := tx.Exec(ctx, mergeSQL(spec), loadTS); err != nil {
			return fmt.Errorf("bomen: upsert %s: %w", spec.target, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("bomen: upsert: commit: %w", err)
	}
	return nil
}
