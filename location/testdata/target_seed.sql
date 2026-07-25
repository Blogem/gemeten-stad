-- Real-shaped BAG + gebieden TARGET tables for the location resolver integration
-- tests (P6 7.6). This mirrors load/geo/schema.go's ensureSchema DDL (the target
-- shape location.Resolve actually reads) but is seeded directly here — the
-- location package never drives load/geo's staging/upsert seam (that would need
-- the gdal sidecar or load/geo's unexported functions); the resolver only ever
-- reads the target tables, so seeding them directly keeps this test
-- self-contained.
--
-- Executed against a connection whose search_path is the test's isolated schema
-- (see location_integration_test.go newSchemaPool) so every unqualified name below
-- lands there, and PostGIS functions (ST_GeomFromText, ST_Contains, ...) still
-- resolve from public.
--
-- The address graph mirrors load/geo/testdata/staging_seed.sql (same
-- identificatie values, same coordinates) so both integration suites describe one
-- consistent fixture, even though they seed independently.

CREATE TABLE bag_openbareruimte (
    identificatie        text        NOT NULL,
    naam                 text,
    begingeldigheid      date        NOT NULL,
    eindgeldigheid       date,
    eindregistratie      timestamptz,
    tijdstipregistratie  timestamptz NOT NULL,
    status               text,
    source_deleted_at    timestamptz,
    PRIMARY KEY (identificatie, begingeldigheid, tijdstipregistratie)
);

CREATE TABLE bag_nummeraanduiding (
    identificatie        text        NOT NULL,
    postcode             text,
    huisnummer           integer,
    openbareruimteref    text,
    begingeldigheid      date        NOT NULL,
    eindgeldigheid       date,
    eindregistratie      timestamptz,
    tijdstipregistratie  timestamptz NOT NULL,
    status               text,
    source_deleted_at    timestamptz,
    PRIMARY KEY (identificatie, begingeldigheid, tijdstipregistratie)
);

CREATE TABLE bag_verblijfsobject (
    identificatie                   text        NOT NULL,
    hoofdadresnummeraanduidingref   text,
    begingeldigheid                 date        NOT NULL,
    eindgeldigheid                  date,
    eindregistratie                 timestamptz,
    tijdstipregistratie             timestamptz NOT NULL,
    status                          text,
    geom                            geometry(Geometry, 28992),
    source_deleted_at               timestamptz,
    PRIMARY KEY (identificatie, begingeldigheid, tijdstipregistratie)
);

CREATE TABLE bag_ligplaats (
    identificatie                   text        NOT NULL,
    hoofdadresnummeraanduidingref   text,
    begingeldigheid                 date        NOT NULL,
    eindgeldigheid                  date,
    eindregistratie                 timestamptz,
    tijdstipregistratie             timestamptz NOT NULL,
    status                          text,
    geom                            geometry(Geometry, 28992),
    source_deleted_at               timestamptz,
    PRIMARY KEY (identificatie, begingeldigheid, tijdstipregistratie)
);

CREATE TABLE bag_standplaats (
    identificatie                   text        NOT NULL,
    hoofdadresnummeraanduidingref   text,
    begingeldigheid                 date        NOT NULL,
    eindgeldigheid                  date,
    eindregistratie                 timestamptz,
    tijdstipregistratie             timestamptz NOT NULL,
    status                          text,
    geom                            geometry(Geometry, 28992),
    source_deleted_at               timestamptz,
    PRIMARY KEY (identificatie, begingeldigheid, tijdstipregistratie)
);

CREATE TABLE gebieden_buurten (
    identificatie      text PRIMARY KEY,
    naam               text,
    code               text,
    ligtinwijkid       text,
    geom               geometry(Geometry, 28992),
    source_deleted_at  timestamptz
);

CREATE TABLE gebieden_wijken (
    identificatie      text PRIMARY KEY,
    naam               text,
    code               text,
    geom               geometry(Geometry, 28992),
    source_deleted_at  timestamptz
);

-- -- openbareruimte ------------------------------------------------------------

INSERT INTO bag_openbareruimte
    (identificatie, naam, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status)
VALUES
    ('opr-teststraat', 'Teststraat', '2000-01-01', NULL, NULL, '2000-01-01T00:00:00Z', 'Naamgeving uitgegeven');

-- -- nummeraanduiding -----------------------------------------------------------
--
-- num-a: two voorkomens of the same object; v1 superseded (eindregistratie set),
--        v2 best-known and valid at the test date 2024-06-01 -> "valid_at_date".
-- num-b: single best-known voorkomen starting AFTER the test date -> no voorkomen
--        valid at date -> "any_time" fallback + timeMismatch caveat.
-- num-c: best-known voorkomen has a withdrawn status but is valid at date ->
--        must still resolve (status is never filtered).
-- num-d: reached only via the exact-street-match tier in the resolver tests
--        (queried by street name, not postcode).
-- num-e: reached through a ligplaats polygon (ST_Centroid path).
-- num-f: reached through a standplaats polygon (ST_Centroid path).

INSERT INTO bag_nummeraanduiding
    (identificatie, postcode, huisnummer, openbareruimteref, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status)
VALUES
    ('num-a', '1000AB', 1, 'opr-teststraat', '2000-01-01', '2020-01-01', '2020-01-01T00:00:00Z', '2000-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('num-a', '1000AB', 1, 'opr-teststraat', '2020-01-01', NULL,         NULL,                    '2020-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('num-b', '1000AC', 2, 'opr-teststraat', '2025-01-01', NULL,         NULL,                    '2025-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('num-c', '1000AD', 3, 'opr-teststraat', '2020-01-01', NULL,         NULL,                    '2023-01-01T00:00:00Z', 'Naamgeving ingetrokken'),
    ('num-d', '1000AF', 4, 'opr-teststraat', '2020-01-01', NULL,         NULL,                    '2020-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('num-e', '1000AG', 6, 'opr-teststraat', '2020-01-01', NULL,         NULL,                    '2020-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('num-f', '1000AH', 7, 'opr-teststraat', '2020-01-01', NULL,         NULL,                    '2020-01-01T00:00:00Z', 'Naamgeving uitgegeven');

-- -- verblijfsobject: point address objects ---------------------------------------

INSERT INTO bag_verblijfsobject
    (identificatie, hoofdadresnummeraanduidingref, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status, geom)
VALUES
    ('vbo-1', 'num-a', '2020-01-01', NULL, NULL, '2020-01-01T00:00:00Z', 'Verblijfsobject in gebruik', ST_GeomFromText('POINT(121000 487000)', 28992)),
    ('vbo-2', 'num-b', '2025-01-01', NULL, NULL, '2025-01-01T00:00:00Z', 'Verblijfsobject in gebruik', ST_GeomFromText('POINT(121010 487010)', 28992)),
    ('vbo-3', 'num-c', '2020-01-01', NULL, NULL, '2023-01-01T00:00:00Z', 'Verblijfsobject in gebruik', ST_GeomFromText('POINT(121020 487020)', 28992)),
    ('vbo-4', 'num-d', '2020-01-01', NULL, NULL, '2020-01-01T00:00:00Z', 'Verblijfsobject in gebruik', ST_GeomFromText('POINT(121030 487030)', 28992));

-- -- ligplaats: polygon address object (centroid path) -----------------------------

INSERT INTO bag_ligplaats
    (identificatie, hoofdadresnummeraanduidingref, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status, geom)
VALUES
    ('lig-1', 'num-e', '2020-01-01', NULL, NULL, '2020-01-01T00:00:00Z', 'Plaats aangewezen',
     ST_GeomFromText('POLYGON((121100 487100, 121110 487100, 121110 487110, 121100 487110, 121100 487100))', 28992));

-- -- standplaats: polygon address object (centroid path) --------------------------

INSERT INTO bag_standplaats
    (identificatie, hoofdadresnummeraanduidingref, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status, geom)
VALUES
    ('sta-1', 'num-f', '2020-01-01', NULL, NULL, '2020-01-01T00:00:00Z', 'Plaats aangewezen',
     ST_GeomFromText('POLYGON((121200 487200, 121210 487200, 121210 487210, 121200 487210, 121200 487200))', 28992));

-- -- gebieden buurten/wijken -------------------------------------------------------
--
-- buurt-noord contains vbo-1 (121000, 487000) and lig-1's polygon. buurt-anders is
-- disjoint, far away, and contains no seeded address -- used to prove a buurt PIP
-- match is keyed by ST_Contains against the query's own point, not by proximity to
-- any address.

INSERT INTO gebieden_buurten
    (identificatie, naam, code, ligtinwijkid, geom)
VALUES
    ('buurt-noord', 'Testbuurt Noord', 'N01', 'wijk-noord',
     ST_GeomFromText('POLYGON((120800 486800, 121150 486800, 121150 487150, 120800 487150, 120800 486800))', 28992)),
    ('buurt-anders', 'Testbuurt Anders', 'N02', 'wijk-noord',
     ST_GeomFromText('POLYGON((130000 490000, 130100 490000, 130100 490100, 130000 490100, 130000 490000))', 28992));

INSERT INTO gebieden_wijken
    (identificatie, naam, code, geom)
VALUES
    ('wijk-noord', 'Testwijk Noord', 'N01',
     ST_GeomFromText('POLYGON((120000 486000, 131000 486000, 131000 491000, 120000 491000, 120000 486000))', 28992));
