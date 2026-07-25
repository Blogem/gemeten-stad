package geo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// column is one target column read from the matching *_staging column of the same name (ogr2ogr
// lowercases all column names, so staging and target share names 1:1). cast, when non-empty, is
// the Postgres type the staging value is cast to on the way in — staging column types come from
// ogr2ogr's own field-type inference, so casting defensively keeps the upsert correct regardless
// of exactly what type ogr2ogr picked.
type column struct {
	name string
	cast string
}

// upsertSpec describes one target/staging table pair for the MERGE upsert: the identity keys
// used to match a staging row to its target row, and the full column list (including the keys)
// to insert for a new row. source_deleted_at is deliberately excluded — it is provenance the
// upsert itself manages, never sourced from staging.
type upsertSpec struct {
	target string
	keys   []string
	cols   []column
}

// upsertSpecs is the pinned load/geo <-> location resolver table contract: the BAG family keyed
// by voorkomen identity (identificatie, begingeldigheid, tijdstipregistratie), and the
// gebieden/CBS family keyed by identificatie alone.
var upsertSpecs = []upsertSpec{
	{
		target: "bag_openbareruimte",
		keys:   []string{"identificatie", "begingeldigheid", "tijdstipregistratie"},
		cols: []column{
			{"identificatie", ""},
			{"naam", ""},
			{"begingeldigheid", "date"},
			{"eindgeldigheid", "date"},
			{"eindregistratie", "timestamptz"},
			{"tijdstipregistratie", "timestamptz"},
			{"status", ""},
		},
	},
	{
		target: "bag_nummeraanduiding",
		keys:   []string{"identificatie", "begingeldigheid", "tijdstipregistratie"},
		cols: []column{
			{"identificatie", ""},
			{"postcode", ""},
			{"huisnummer", "integer"},
			{"openbareruimteref", ""},
			{"begingeldigheid", "date"},
			{"eindgeldigheid", "date"},
			{"eindregistratie", "timestamptz"},
			{"tijdstipregistratie", "timestamptz"},
			{"status", ""},
		},
	},
	{
		target: "bag_verblijfsobject",
		keys:   []string{"identificatie", "begingeldigheid", "tijdstipregistratie"},
		cols: []column{
			{"identificatie", ""},
			{"hoofdadresnummeraanduidingref", ""},
			{"begingeldigheid", "date"},
			{"eindgeldigheid", "date"},
			{"eindregistratie", "timestamptz"},
			{"tijdstipregistratie", "timestamptz"},
			{"status", ""},
			{"geom", ""},
		},
	},
	{
		target: "bag_ligplaats",
		keys:   []string{"identificatie", "begingeldigheid", "tijdstipregistratie"},
		cols: []column{
			{"identificatie", ""},
			{"hoofdadresnummeraanduidingref", ""},
			{"begingeldigheid", "date"},
			{"eindgeldigheid", "date"},
			{"eindregistratie", "timestamptz"},
			{"tijdstipregistratie", "timestamptz"},
			{"status", ""},
			{"geom", ""},
		},
	},
	{
		target: "bag_standplaats",
		keys:   []string{"identificatie", "begingeldigheid", "tijdstipregistratie"},
		cols: []column{
			{"identificatie", ""},
			{"hoofdadresnummeraanduidingref", ""},
			{"begingeldigheid", "date"},
			{"eindgeldigheid", "date"},
			{"eindregistratie", "timestamptz"},
			{"tijdstipregistratie", "timestamptz"},
			{"status", ""},
			{"geom", ""},
		},
	},
	{
		target: "gebieden_buurten",
		keys:   []string{"identificatie"},
		cols: []column{
			{"identificatie", ""},
			{"naam", ""},
			{"code", ""},
			{"ligtinwijkid", ""},
			{"geom", ""},
		},
	},
	{
		target: "gebieden_wijken",
		keys:   []string{"identificatie"},
		cols: []column{
			{"identificatie", ""},
			{"naam", ""},
			{"code", ""},
			{"geom", ""},
		},
	},
	{
		target: "cbs_buurten",
		keys:   []string{"identificatie"},
		cols: []column{
			{"identificatie", ""},
			{"geom", ""},
		},
	},
}

// mergeSQL builds the MERGE statement for spec. It takes a single $1 parameter, the load
// timestamp. Voorkomens present in staging but not the target are inserted; voorkomens matched by
// identity are left alone unless they need un-soft-deleting (a previously soft-deleted object
// reappearing in a fresh extract); target rows absent from staging are soft-deleted by stamping
// source_deleted_at = $1, guarded by "AND t.source_deleted_at IS NULL" so re-running against an
// unchanged extract never re-touches an already soft-deleted row (the no-op requirement).
func mergeSQL(spec upsertSpec) string {
	on := make([]string, len(spec.keys))
	for i, k := range spec.keys {
		on[i] = fmt.Sprintf("t.%s = s.%s", k, k)
	}

	names := make([]string, len(spec.cols))
	values := make([]string, len(spec.cols))
	for i, c := range spec.cols {
		names[i] = c.name
		if c.cast != "" {
			values[i] = fmt.Sprintf("s.%s::%s", c.name, c.cast)
		} else {
			values[i] = "s." + c.name
		}
	}

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
`, spec.target, spec.target+"_staging", strings.Join(on, " AND "), strings.Join(names, ", "), strings.Join(values, ", "))
}

// upsertAll runs every upsertSpec's MERGE in a single transaction against one load timestamp, so
// the whole reload is atomic: either every table reconciles against its staging snapshot, or
// none of it does.
func upsertAll(ctx context.Context, pool *pgxpool.Pool, loadTS time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("geo: upsert: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, spec := range upsertSpecs {
		if _, err := tx.Exec(ctx, mergeSQL(spec), loadTS); err != nil {
			return fmt.Errorf("geo: upsert %s: %w", spec.target, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("geo: upsert: commit: %w", err)
	}
	return nil
}
