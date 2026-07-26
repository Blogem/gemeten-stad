package bomen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// stageColumn describes how to extract one staging-table column's SQL bind value from a landed
// source row, and — for a geometry column — how to wrap its placeholder so Postgres parses it
// rather than storing GeoJSON text verbatim. bomen has no ogr2ogr sidecar (unlike load/geo), so
// staging is a direct SQL insert built from these column descriptors.
type stageColumn struct {
	name string
	fn   func(row map[string]any) (any, error)
	wrap func(placeholder string) string // nil means "use the placeholder as-is"
}

// textColumn reads a plain string field, treating a missing key, nil value, or empty string as
// SQL NULL.
func textColumn(name string) stageColumn {
	return stageColumn{name: name, fn: func(row map[string]any) (any, error) {
		return rowString(row, name), nil
	}}
}

// timestampColumn reads a lifecycle-date field into a Go time.Time for a timestamptz column, or
// nil if the field is null/empty (docs/DATA_SOURCES.md §2a: the source's `[isnull]` filter is
// unreliable, so null lifecycle dates are always handled client-side, never via a query filter —
// this is the load-side counterpart: accept and store the null rather than reject the row).
func timestampColumn(name string) stageColumn {
	return stageColumn{name: name, fn: func(row map[string]any) (any, error) {
		return parseTimestamp(row[name])
	}}
}

// geometryColumn reads a GeoJSON-shaped geometry field (the Datapunt bomen API's JSON format
// encodes `geometrie` as GeoJSON) and wraps its placeholder in ST_GeomFromGeoJSON so Postgres
// parses it into the geometry(Point,4326) column.
func geometryColumn(name string) stageColumn {
	return stageColumn{
		name: name,
		fn: func(row map[string]any) (any, error) {
			return geoJSONText(row[name])
		},
		wrap: func(placeholder string) string {
			return fmt.Sprintf("ST_GeomFromGeoJSON(%s)", placeholder)
		},
	}
}

// rawColumn marshals the whole landed row into the staging table's raw jsonb column, so any
// source column not modeled explicitly above is never silently dropped (mirrors schema.go's
// rationale for the raw column).
func rawColumn() stageColumn {
	return stageColumn{name: "raw", fn: func(row map[string]any) (any, error) {
		b, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("marshal raw row: %w", err)
		}
		return b, nil
	}}
}

// kapenherplantStagingColumns and stamgegevensStagingColumns list the staging-table columns using
// the source's own field names (docs/DATA_SOURCES.md §2a), plus the raw column every staging row
// also carries.
var kapenherplantStagingColumns = []stageColumn{
	textColumn("id"),
	textColumn("boomId"),
	textColumn("boomNieuwId"),
	textColumn("gbdBuurtId"),
	textColumn("dichtstbijzijndeBagAdres"),
	textColumn("postcode"),
	textColumn("soortnaam"),
	textColumn("toeTePassenBoomsoort"),
	timestampColumn("datumVergunningsaanvraag"),
	timestampColumn("kapmaatregelDatumUitgevoerd"),
	timestampColumn("plantmaatregelDatumUitgevoerd"),
	rawColumn(),
}

var stamgegevensStagingColumns = []stageColumn{
	textColumn("id"),
	textColumn("gbdBuurtId"),
	textColumn("soortnaam"),
	geometryColumn("geometrie"),
	rawColumn(),
}

// stageKapenherplant loads rows — the already-parsed landed kapenherplant export, one map per row
// keyed by the source's own field names — into kapenherplant_staging, replacing whatever was
// staged there before: the staging table always reflects exactly the latest export, so upsertAll
// can diff against it via the MERGE's own NOT MATCHED BY SOURCE clause.
func stageKapenherplant(ctx context.Context, pool *pgxpool.Pool, rows []map[string]any) error {
	return stageRows(ctx, pool, kapenherplantStagingTable, kapenherplantStagingColumns, rows)
}

// stageStamgegevens is stageKapenherplant's stamgegevens counterpart.
func stageStamgegevens(ctx context.Context, pool *pgxpool.Pool, rows []map[string]any) error {
	return stageRows(ctx, pool, stamgegevensStagingTable, stamgegevensStagingColumns, rows)
}

func stageRows(ctx context.Context, pool *pgxpool.Pool, stagingTable string, columns []stageColumn, rows []map[string]any) error {
	truncateSQL := fmt.Sprintf(`TRUNCATE TABLE %s`, stagingTable)
	if _, err := pool.Exec(ctx, truncateSQL); err != nil {
		return fmt.Errorf("bomen: truncate %s: %w", stagingTable, err)
	}

	names := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, c := range columns {
		names[i] = pgx.Identifier{c.name}.Sanitize()
		ph := fmt.Sprintf("$%d", i+1)
		if c.wrap != nil {
			ph = c.wrap(ph)
		}
		placeholders[i] = ph
	}
	insertSQL := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`,
		stagingTable, strings.Join(names, ", "), strings.Join(placeholders, ", "))

	for _, row := range rows {
		args := make([]any, len(columns))
		for i, c := range columns {
			v, err := c.fn(row)
			if err != nil {
				return fmt.Errorf("bomen: stage row id=%v column %s: %w", row["id"], c.name, err)
			}
			args[i] = v
		}
		if _, err := pool.Exec(ctx, insertSQL, args...); err != nil {
			return fmt.Errorf("bomen: insert into %s: %w", stagingTable, err)
		}
	}
	return nil
}

// rowString reads a string field from row, treating a missing key, nil value, or empty string as
// SQL NULL.
func rowString(row map[string]any, key string) any {
	v, ok := row[key]
	if !ok || v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		if s == "" {
			return nil
		}
		return s
	}
	return fmt.Sprintf("%v", v)
}

// parseTimestamp parses a source date/time field into a Go time.Time for a timestamptz column, or
// (nil, nil) if the field is absent/null/empty.
func parseTimestamp(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("timestamp field is not a string: %T", v)
	}
	if s == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return nil, fmt.Errorf("unparsable timestamp %q", s)
}

// geoJSONText returns the GeoJSON text for a source geometry field, or (nil, nil) if the field is
// absent/null.
func geoJSONText(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal geometry field: %w", err)
	}
	return string(b), nil
}
