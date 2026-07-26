-- Real-shaped BAG + gebieden subset for the load/geo integration tests (P6 7.4).
--
-- This is executed against a connection whose search_path is set to the test's
-- isolated schema (see load_integration_test.go newSchemaPool) so every unqualified
-- table name below lands in that schema, never in public.
--
-- Column types match what ogr2ogr's own GML-schema-driven field-type inference
-- produces for lvbag (date/timestamptz/integer, not text) — the MERGE's ON
-- clause (upsert.go's mergeSQL) compares t.col = s.col with NO cast, so the
-- staging and target column types must already be comparable; the ::date /
-- ::timestamptz / ::integer casts in the INSERT ... VALUES list are extra
-- defensive insurance on the value side, not a substitute for this.
--
-- The BAG voorkomen identity is (identificatie, voorkomenidentificatie): a single
-- object (identificatie) has one row per voorkomen (temporal version), each with a
-- distinct integer voorkomenidentificatie. begingeldigheid + tijdstipregistratie are
-- NOT part of the key — a correction can produce two voorkomens sharing both, so the
-- integer voorkomenidentificatie is what distinguishes them (see num-a below).
--
-- Identificatie values are short mnemonic strings, not real IMBAG URIs — the
-- '%.0363%' municipality filter is ogr2ogr's job at staging time (already covered
-- by TestBAGStagingArgs in geo_test.go) and is bypassed here since this seed is
-- inserted directly into the *_staging tables.

CREATE TABLE bag_openbareruimte_staging (
    identificatie          text,
    voorkomenidentificatie integer,
    naam                   text,
    begingeldigheid        date,
    eindgeldigheid         date,
    eindregistratie        timestamptz,
    tijdstipregistratie    timestamptz,
    status                 text
);

CREATE TABLE bag_nummeraanduiding_staging (
    identificatie          text,
    voorkomenidentificatie integer,
    postcode               text,
    huisnummer             integer,
    openbareruimteref      text,
    begingeldigheid        date,
    eindgeldigheid         date,
    eindregistratie        timestamptz,
    tijdstipregistratie    timestamptz,
    status                 text
);

CREATE TABLE bag_verblijfsobject_staging (
    identificatie                  text,
    voorkomenidentificatie         integer,
    hoofdadresnummeraanduidingref  text,
    begingeldigheid                date,
    eindgeldigheid                 date,
    eindregistratie                timestamptz,
    tijdstipregistratie            timestamptz,
    status                         text,
    geom                           geometry
);

CREATE TABLE bag_ligplaats_staging (
    identificatie                  text,
    voorkomenidentificatie         integer,
    hoofdadresnummeraanduidingref  text,
    begingeldigheid                date,
    eindgeldigheid                 date,
    eindregistratie                timestamptz,
    tijdstipregistratie            timestamptz,
    status                         text,
    geom                           geometry
);

CREATE TABLE bag_standplaats_staging (
    identificatie                  text,
    voorkomenidentificatie         integer,
    hoofdadresnummeraanduidingref  text,
    begingeldigheid                date,
    eindgeldigheid                 date,
    eindregistratie                timestamptz,
    tijdstipregistratie            timestamptz,
    status                         text,
    geom                           geometry
);

CREATE TABLE gebieden_buurten_staging (
    identificatie   text,
    naam            text,
    code            text,
    ligtinwijkid    text,
    geom            geometry
);

CREATE TABLE gebieden_wijken_staging (
    identificatie   text,
    naam            text,
    code            text,
    geom            geometry
);

CREATE TABLE cbs_buurten_staging (
    identificatie   text,
    geom            geometry
);

-- -- openbareruimte: one street, shared by every nummeraanduiding below ----------

INSERT INTO bag_openbareruimte_staging
    (identificatie, voorkomenidentificatie, naam, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status)
VALUES
    ('opr-teststraat', 1, 'Teststraat', '2000-01-01', NULL, NULL, '2000-01-01T00:00:00Z', 'Naamgeving uitgegeven');

-- -- nummeraanduiding -----------------------------------------------------------
--
-- num-a: two voorkomens of the SAME object (superseded v1, best-known v2),
--        distinguished by voorkomenidentificatie (1, 2); v2 is valid at the resolver
--        test date (2024-06-01) -> "valid_at_date" tier.
-- num-b: single best-known voorkomen whose begingeldigheid (2025-01-01) is AFTER
--        the resolver test date -> no voorkomen valid at date -> "any_time" fallback.
-- num-c: best-known voorkomen has a withdrawn status ("... ingetrokken") but is
--        valid at date -> must still resolve (no status filter).
-- num-d: distinct address used only via the exact-street-match tier.
-- num-e: address reached through a ligplaats polygon (centroid path).
-- num-f: address reached through a standplaats polygon (centroid path).
-- num-g: unused by the resolver tests; dedicated to the soft-delete scenario in
--        load_integration_test.go (deleted from staging, re-upserted).

INSERT INTO bag_nummeraanduiding_staging
    (identificatie, voorkomenidentificatie, postcode, huisnummer, openbareruimteref, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status)
VALUES
    ('num-a', 1, '1000AB', '1', 'opr-teststraat', '2000-01-01', '2020-01-01', '2020-01-01T00:00:00Z', '2000-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('num-a', 2, '1000AB', '1', 'opr-teststraat', '2020-01-01', NULL,         NULL,                    '2020-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('num-b', 1, '1000AC', '2', 'opr-teststraat', '2025-01-01', NULL,         NULL,                    '2025-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('num-c', 1, '1000AD', '3', 'opr-teststraat', '2020-01-01', NULL,         NULL,                    '2023-01-01T00:00:00Z', 'Naamgeving ingetrokken'),
    ('num-d', 1, '1000AF', '4', 'opr-teststraat', '2020-01-01', NULL,         NULL,                    '2020-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('num-e', 1, '1000AG', '6', 'opr-teststraat', '2020-01-01', NULL,         NULL,                    '2020-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('num-f', 1, '1000AH', '7', 'opr-teststraat', '2020-01-01', NULL,         NULL,                    '2020-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('num-g', 1, '1000AJ', '8', 'opr-teststraat', '2020-01-01', NULL,         NULL,                    '2020-01-01T00:00:00Z', 'Naamgeving uitgegeven');

-- -- verblijfsobject: point address objects ---------------------------------------

INSERT INTO bag_verblijfsobject_staging
    (identificatie, voorkomenidentificatie, hoofdadresnummeraanduidingref, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status, geom)
VALUES
    ('vbo-1', 1, 'num-a', '2020-01-01', NULL, NULL, '2020-01-01T00:00:00Z', 'Verblijfsobject in gebruik', ST_GeomFromText('POINT(121000 487000)', 28992)),
    ('vbo-2', 1, 'num-b', '2025-01-01', NULL, NULL, '2025-01-01T00:00:00Z', 'Verblijfsobject in gebruik', ST_GeomFromText('POINT(121010 487010)', 28992)),
    ('vbo-3', 1, 'num-c', '2020-01-01', NULL, NULL, '2023-01-01T00:00:00Z', 'Verblijfsobject in gebruik', ST_GeomFromText('POINT(121020 487020)', 28992)),
    ('vbo-4', 1, 'num-d', '2020-01-01', NULL, NULL, '2020-01-01T00:00:00Z', 'Verblijfsobject in gebruik', ST_GeomFromText('POINT(121030 487030)', 28992));

-- -- ligplaats: polygon address object (centroid path) -----------------------------

INSERT INTO bag_ligplaats_staging
    (identificatie, voorkomenidentificatie, hoofdadresnummeraanduidingref, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status, geom)
VALUES
    ('lig-1', 1, 'num-e', '2020-01-01', NULL, NULL, '2020-01-01T00:00:00Z', 'Plaats aangewezen',
     ST_GeomFromText('POLYGON((121100 487100, 121110 487100, 121110 487110, 121100 487110, 121100 487100))', 28992));

-- -- standplaats: polygon address object (centroid path) --------------------------

INSERT INTO bag_standplaats_staging
    (identificatie, voorkomenidentificatie, hoofdadresnummeraanduidingref, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status, geom)
VALUES
    ('sta-1', 1, 'num-f', '2020-01-01', NULL, NULL, '2020-01-01T00:00:00Z', 'Plaats aangewezen',
     ST_GeomFromText('POLYGON((121200 487200, 121210 487200, 121210 487210, 121200 487210, 121200 487200))', 28992));

-- -- gebieden buurten/wijken -------------------------------------------------------
--
-- buurt-noord contains vbo-1 (121000, 487000) and lig-1's polygon -- used both for
-- the "point carries a known gbdBuurtId" scenario and the "not snapped to a nearer
-- address" scenario (a bare point at vbo-1's own coordinates, no text address,
-- still resolves to buurt-noord, never to vbo-1's address).
-- buurt-anders is a disjoint polygon far away, containing no seeded address.

INSERT INTO gebieden_buurten_staging
    (identificatie, naam, code, ligtinwijkid, geom)
VALUES
    ('buurt-noord', 'Testbuurt Noord', 'N01', 'wijk-noord',
     ST_GeomFromText('POLYGON((120800 486800, 121150 486800, 121150 487150, 120800 487150, 120800 486800))', 28992)),
    ('buurt-anders', 'Testbuurt Anders', 'N02', 'wijk-noord',
     ST_GeomFromText('POLYGON((130000 490000, 130100 490000, 130100 490100, 130000 490100, 130000 490000))', 28992));

INSERT INTO gebieden_wijken_staging
    (identificatie, naam, code, geom)
VALUES
    ('wijk-noord', 'Testwijk Noord', 'N01',
     ST_GeomFromText('POLYGON((120000 486000, 131000 486000, 131000 491000, 120000 491000, 120000 486000))', 28992));

INSERT INTO cbs_buurten_staging
    (identificatie, geom)
VALUES
    ('buurt-noord', ST_GeomFromText('POLYGON((120800 486800, 121150 486800, 121150 487150, 120800 487150, 120800 486800))', 28992));
