-- Real-shaped BAG + gebieden TARGET tables for the load/koop integration suite (tasks 3.5, 6.3,
-- 8.1, 8.2), adapted from location/testdata/target_seed.sql (same DDL shape, since
-- location.Resolve -- which resolveBesluit calls -- reads exactly these unqualified target
-- tables). Unlike location's own fixture (whose two seeded buurten are BOTH "N"-prefixed), this
-- seed deliberately includes ONE Noord buurt (code "N01") and ONE non-Noord buurt (code "A01", a
-- disjoint polygon) so load/koop's Noord-scoping gate (gebieden_buurten.code LIKE 'N%') has a real
-- in/out case to exercise.
--
-- Executed against a connection whose search_path is the test's isolated schema (see
-- newSchemaPool in the harness test file) so every unqualified name below lands there, and
-- PostGIS/pg_trgm functions (ST_GeomFromText, ST_Contains, similarity, ...) still resolve from
-- public.
--
-- Coordinates (RD / EPSG:28992):
--   N01BUURT (code N01): polygon 120800..121150 x, 486800..487150 y.
--     - vbo-orehof-8 (Örehof 8, postcode 1024BB) sits at (121000, 487000) -- the address-tier
--       (0.90) resolution target for the Noord besluit fixture.
--     - (120850, 486850) sits inside the same polygon but at NO seeded address -- the point-only
--       floor (0.50) resolution target for the point-floor Noord besluit fixture; the point-floor
--       fixture's title carries no extractable postcode, so the address/postcode tiers never run
--       and this point can never "snap" to vbo-orehof-8.
--   A01BUURT (code A01): a disjoint polygon 130000..130100 x, 490000..490100 y.
--     - vbo-zuidstraat-3 (Zuidstraat 3, postcode 1077ZZ) sits at (130050, 490050) -- the
--       address-tier (0.90) resolution target for the out-of-Noord besluit fixture.
--   (999000, 999000) is far outside both polygons -- the unresolvable besluit fixture's own point,
--   which must PIP into neither buurt.

CREATE TABLE bag_openbareruimte (
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

CREATE TABLE bag_nummeraanduiding (
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

CREATE TABLE bag_verblijfsobject (
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

CREATE TABLE bag_ligplaats (
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

CREATE TABLE bag_standplaats (
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
    (identificatie, voorkomenidentificatie, naam, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status)
VALUES
    ('opr-orehof',      1, 'Örehof',     '2000-01-01', NULL, NULL, '2000-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('opr-zuidstraat',  1, 'Zuidstraat', '2000-01-01', NULL, NULL, '2000-01-01T00:00:00Z', 'Naamgeving uitgegeven');

-- -- nummeraanduiding -----------------------------------------------------------

INSERT INTO bag_nummeraanduiding
    (identificatie, voorkomenidentificatie, postcode, huisnummer, openbareruimteref, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status)
VALUES
    ('num-orehof-8',     1, '1024BB', 8, 'opr-orehof',     '2000-01-01', NULL, NULL, '2000-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('num-zuidstraat-3', 1, '1077ZZ', 3, 'opr-zuidstraat', '2000-01-01', NULL, NULL, '2000-01-01T00:00:00Z', 'Naamgeving uitgegeven');

-- -- verblijfsobject: point address objects ---------------------------------------
--
-- vbo-orehof-8 is the address-tier (0.90) target for the Noord besluit fixture (Örehof 8
-- 1024BB), inside N01BUURT. vbo-zuidstraat-3 is the address-tier (0.90) target for the
-- out-of-Noord besluit fixture (Zuidstraat 3 1077ZZ), inside A01BUURT.

INSERT INTO bag_verblijfsobject
    (identificatie, voorkomenidentificatie, hoofdadresnummeraanduidingref, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status, geom)
VALUES
    ('vbo-orehof-8',     1, 'num-orehof-8',     '2000-01-01', NULL, NULL, '2000-01-01T00:00:00Z', 'Verblijfsobject in gebruik', ST_GeomFromText('POINT(121000 487000)', 28992)),
    ('vbo-zuidstraat-3', 1, 'num-zuidstraat-3', '2000-01-01', NULL, NULL, '2000-01-01T00:00:00Z', 'Verblijfsobject in gebruik', ST_GeomFromText('POINT(130050 490050)', 28992));

-- -- gebieden buurten/wijken -------------------------------------------------------
--
-- N01BUURT (code N01, Noord) contains vbo-orehof-8 (121000, 487000) and the bare point
-- (120850, 486850) used by the point-floor fixture. A01BUURT (code A01, NOT Noord) is a disjoint
-- polygon containing vbo-zuidstraat-3 (130050, 490050). (999000, 999000) -- the unresolvable
-- fixture's point -- falls inside neither.

INSERT INTO gebieden_buurten
    (identificatie, naam, code, ligtinwijkid, geom)
VALUES
    ('N01BUURT', 'Testbuurt Noord', 'N01', 'N01WIJK',
     ST_GeomFromText('POLYGON((120800 486800, 121150 486800, 121150 487150, 120800 487150, 120800 486800))', 28992)),
    ('A01BUURT', 'Testbuurt Anders', 'A01', NULL,
     ST_GeomFromText('POLYGON((130000 490000, 130100 490000, 130100 490100, 130000 490100, 130000 490000))', 28992));

INSERT INTO gebieden_wijken
    (identificatie, naam, code, geom)
VALUES
    ('N01WIJK', 'Testwijk Noord', 'N01',
     ST_GeomFromText('POLYGON((120000 486000, 131000 486000, 131000 491000, 120000 491000, 120000 486000))', 28992));
