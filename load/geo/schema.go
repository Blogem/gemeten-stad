package geo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// targetTables lists every table the geo load owns, in dependency order (no FKs are declared
// between them — the resolver joins in Go — so this order only matters for the reset drop).
var targetTables = []string{
	"bag_openbareruimte",
	"bag_nummeraanduiding",
	"bag_verblijfsobject",
	"bag_ligplaats",
	"bag_standplaats",
	"gebieden_buurten",
	"gebieden_wijken",
	"cbs_buurten",
}

// qualify renders table as an explicitly schema-qualified, properly quoted identifier
// (schema.table). Every DDL/DML statement Load issues names its target this way, so it never
// relies on the connection's search_path to resolve to the right schema: under an integration
// harness with search_path = <test_schema>, public (public holding the real tables), an
// unqualified name would resolve to whichever schema comes first on the path, not necessarily the
// caller's own.
func qualify(schema, table string) string {
	return pgx.Identifier{schema, table}.Sanitize()
}

// loadSchema resolves the schema unqualified names would resolve against — the first schema on
// the connection's search_path — once per Load, so every subsequent DDL/DML statement can be
// qualified against it explicitly instead of depending on search_path at each call site. In
// production (search_path defaults to "public") this resolves to "public", leaving prod behavior
// unchanged.
func loadSchema(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var s string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&s); err != nil {
		return "", fmt.Errorf("geo: resolve current schema: %w", err)
	}
	if s == "" {
		return "", fmt.Errorf("geo: current_schema() is empty (no schema on search_path)")
	}
	return s, nil
}

// ensureExtensions creates the extensions the load and the resolver depend on: postgis for
// geometry, pg_trgm for the fuzzy street-name index.
func ensureExtensions(ctx context.Context, pool *pgxpool.Pool) error {
	const stmt = `
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
`
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("geo: ensure extensions: %w", err)
	}
	return nil
}

// dropTargets drops the target tables (--reset only), so ensureSchema rebuilds them clean from
// the next load's staged data. It does NOT touch *_staging tables — those are dropped and
// recreated per-object-type as part of staging regardless of --reset.
func dropTargets(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	for _, table := range targetTables {
		stmt := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE;", qualify(schema, table))
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("geo: reset: drop %s: %w", table, err)
		}
	}
	return nil
}

// ensureSchema creates the target tables (CREATE TABLE IF NOT EXISTS) if they are not already
// present. Column names/types match the pinned load/geo <-> location resolver contract; the
// voorkomen identity (identificatie, voorkomenidentificatie) is the BAG upsert key — the real BAG
// voorkomen key, because a correction can produce two voorkomens sharing begingeldigheid AND
// tijdstipregistratie, distinguished only by voorkomenidentificatie. identificatie alone is the
// gebieden/CBS upsert key. source_deleted_at is nullable soft-delete provenance — set only when a
// target row goes absent from a reload, never a resolver filter.
func ensureSchema(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	stmt := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    identificatie          text        NOT NULL,
    voorkomenidentificatie integer     NOT NULL,
    naam                   text,
    begingeldigheid        date        NOT NULL,
    eindgeldigheid         date,
    eindregistratie        timestamptz,
    tijdstipregistratie    timestamptz NOT NULL,
    status                 text,
    source_deleted_at      timestamptz,
    PRIMARY KEY (identificatie, voorkomenidentificatie)
);

CREATE TABLE IF NOT EXISTS %s (
    identificatie          text        NOT NULL,
    voorkomenidentificatie integer     NOT NULL,
    postcode               text,
    huisnummer             integer,
    openbareruimteref      text,
    begingeldigheid        date        NOT NULL,
    eindgeldigheid         date,
    eindregistratie        timestamptz,
    tijdstipregistratie    timestamptz NOT NULL,
    status                 text,
    source_deleted_at      timestamptz,
    PRIMARY KEY (identificatie, voorkomenidentificatie)
);

CREATE TABLE IF NOT EXISTS %s (
    identificatie                   text        NOT NULL,
    voorkomenidentificatie          integer     NOT NULL,
    hoofdadresnummeraanduidingref   text,
    begingeldigheid                 date        NOT NULL,
    eindgeldigheid                  date,
    eindregistratie                 timestamptz,
    tijdstipregistratie             timestamptz NOT NULL,
    status                          text,
    geom                            geometry(Geometry, 28992),
    source_deleted_at               timestamptz,
    PRIMARY KEY (identificatie, voorkomenidentificatie)
);

CREATE TABLE IF NOT EXISTS %s (
    identificatie                   text        NOT NULL,
    voorkomenidentificatie          integer     NOT NULL,
    hoofdadresnummeraanduidingref   text,
    begingeldigheid                 date        NOT NULL,
    eindgeldigheid                  date,
    eindregistratie                 timestamptz,
    tijdstipregistratie             timestamptz NOT NULL,
    status                          text,
    geom                            geometry(Geometry, 28992),
    source_deleted_at               timestamptz,
    PRIMARY KEY (identificatie, voorkomenidentificatie)
);

CREATE TABLE IF NOT EXISTS %s (
    identificatie                   text        NOT NULL,
    voorkomenidentificatie          integer     NOT NULL,
    hoofdadresnummeraanduidingref   text,
    begingeldigheid                 date        NOT NULL,
    eindgeldigheid                  date,
    eindregistratie                 timestamptz,
    tijdstipregistratie             timestamptz NOT NULL,
    status                          text,
    geom                            geometry(Geometry, 28992),
    source_deleted_at               timestamptz,
    PRIMARY KEY (identificatie, voorkomenidentificatie)
);

CREATE TABLE IF NOT EXISTS %s (
    identificatie      text PRIMARY KEY,
    naam               text,
    code               text,
    ligtinwijkid       text,
    geom               geometry(Geometry, 28992),
    source_deleted_at  timestamptz
);

CREATE TABLE IF NOT EXISTS %s (
    identificatie      text PRIMARY KEY,
    naam               text,
    code               text,
    geom               geometry(Geometry, 28992),
    source_deleted_at  timestamptz
);

CREATE TABLE IF NOT EXISTS %s (
    buurtcode          text PRIMARY KEY,
    geom               geometry(Geometry, 28992),
    source_deleted_at  timestamptz
);
`,
		qualify(schema, "bag_openbareruimte"),
		qualify(schema, "bag_nummeraanduiding"),
		qualify(schema, "bag_verblijfsobject"),
		qualify(schema, "bag_ligplaats"),
		qualify(schema, "bag_standplaats"),
		qualify(schema, "gebieden_buurten"),
		qualify(schema, "gebieden_wijken"),
		qualify(schema, "cbs_buurten"),
	)
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("geo: ensure schema: %w", err)
	}
	return nil
}
