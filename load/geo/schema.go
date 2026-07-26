package geo

import (
	"context"
	"fmt"

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
func dropTargets(ctx context.Context, pool *pgxpool.Pool) error {
	for _, table := range targetTables {
		stmt := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE;", table)
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
func ensureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	const stmt = `
CREATE TABLE IF NOT EXISTS bag_openbareruimte (
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

CREATE TABLE IF NOT EXISTS bag_nummeraanduiding (
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

CREATE TABLE IF NOT EXISTS bag_verblijfsobject (
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

CREATE TABLE IF NOT EXISTS bag_ligplaats (
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

CREATE TABLE IF NOT EXISTS bag_standplaats (
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

CREATE TABLE IF NOT EXISTS gebieden_buurten (
    identificatie      text PRIMARY KEY,
    naam               text,
    code               text,
    ligtinwijkid       text,
    geom               geometry(Geometry, 28992),
    source_deleted_at  timestamptz
);

CREATE TABLE IF NOT EXISTS gebieden_wijken (
    identificatie      text PRIMARY KEY,
    naam               text,
    code               text,
    geom               geometry(Geometry, 28992),
    source_deleted_at  timestamptz
);

CREATE TABLE IF NOT EXISTS cbs_buurten (
    identificatie      text PRIMARY KEY,
    geom               geometry(Geometry, 28992),
    source_deleted_at  timestamptz
);
`
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("geo: ensure schema: %w", err)
	}
	return nil
}
