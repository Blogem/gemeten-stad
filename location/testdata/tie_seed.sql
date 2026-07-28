-- Incremental fixture for the LIMIT-1 tie-break determinism regression test
-- (determinism_integration_test.go). Loaded via seedTie AFTER
-- testdata/target_seed.sql's tables and base rows (seedTargets), using
-- postcode/huisnummer/point values that do not collide with any row seeded
-- there.
--
-- Address tie: two DISTINCT best-known bag_nummeraanduiding objects
-- (tie-num-a, tie-num-b) share the SAME (postcode, huisnummer) and are BOTH
-- valid at the resolver's test date -- so `ORDER BY valid_at_date DESC NULLS
-- LAST` alone cannot separate them; only a final, unique identificatie
-- tie-break can. The rows are inserted in DESCENDING identificatie order
-- (tie-num-b before tie-num-a) so that a plan which -- absent an explicit
-- tie-break -- merely follows physical/insertion order would surface
-- tie-vbo-b's point: the WRONG candidate under the deterministic
-- (smallest-identificatie) rule the fix is expected to enforce.

INSERT INTO bag_nummeraanduiding
    (identificatie, voorkomenidentificatie, postcode, huisnummer, openbareruimteref, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status)
VALUES
    ('tie-num-b', 1, '1099TT', 50, NULL, '2020-01-01', NULL, NULL, '2020-01-01T00:00:00Z', 'Naamgeving uitgegeven'),
    ('tie-num-a', 1, '1099TT', 50, NULL, '2020-01-01', NULL, NULL, '2020-01-01T00:00:00Z', 'Naamgeving uitgegeven');

INSERT INTO bag_verblijfsobject
    (identificatie, voorkomenidentificatie, hoofdadresnummeraanduidingref, begingeldigheid, eindgeldigheid, eindregistratie, tijdstipregistratie, status, geom)
VALUES
    ('tie-vbo-b', 1, 'tie-num-b', '2020-01-01', NULL, NULL, '2020-01-01T00:00:00Z', 'Verblijfsobject in gebruik', ST_GeomFromText('POINT(129000 495000)', 28992)),
    ('tie-vbo-a', 1, 'tie-num-a', '2020-01-01', NULL, NULL, '2020-01-01T00:00:00Z', 'Verblijfsobject in gebruik', ST_GeomFromText('POINT(129100 495100)', 28992));

-- Buurt PIP tie: two congruent gebieden_buurten polygons both contain the
-- point (140000, 500000), far from every other seeded buurt (buurt-noord,
-- buurt-anders). Same insertion-order reasoning as above: tie-buurt-b
-- (larger identificatie) is inserted first, tie-buurt-a (smaller,
-- deterministically-correct winner) second.

INSERT INTO gebieden_buurten
    (identificatie, naam, code, ligtinwijkid, geom)
VALUES
    ('tie-buurt-b', 'Tie Buurt B', 'T02', 'wijk-noord',
     ST_GeomFromText('POLYGON((139900 499900, 140100 499900, 140100 500100, 139900 500100, 139900 499900))', 28992)),
    ('tie-buurt-a', 'Tie Buurt A', 'T01', 'wijk-noord',
     ST_GeomFromText('POLYGON((139900 499900, 140100 499900, 140100 500100, 139900 500100, 139900 499900))', 28992));
