package location

// The queries below port spikes/spike-d/sql/resolve.sql (address/postcode ladder) and pip.sql
// (buurt point-in-polygon). They are parameterized ($1, $2, ...) — no user value is ever
// interpolated into the SQL text.
//
// Rules ported verbatim from the spike (see design.md D4):
//   - Best-known voorkomen: eindregistratie IS NULL. Never filter on status.
//   - Valid-at-date: begingeldigheid <= D AND (eindgeldigheid IS NULL OR eindgeldigheid > D).
//   - The address point is reached NUM.identificatie = *.hoofdadresnummeraanduidingref: VBO gives
//     a point directly, LIG/STA give a polygon whose ST_Centroid stands in for the address point.

// adrespuntCTE is the "reach the adresseerbaar object" join, shared by all three address-tier
// queries: union the address point across verblijfsobject (point), ligplaats and standplaats
// (polygon -> centroid), keyed by hoofdadresnummeraanduidingref.
const adrespuntCTE = `adrespunt AS (
  SELECT hoofdadresnummeraanduidingref AS num_ref, geom
    FROM bag_verblijfsobject WHERE eindregistratie IS NULL
  UNION ALL
  SELECT hoofdadresnummeraanduidingref, ST_Centroid(geom)
    FROM bag_ligplaats WHERE eindregistratie IS NULL
  UNION ALL
  SELECT hoofdadresnummeraanduidingref, ST_Centroid(geom)
    FROM bag_standplaats WHERE eindregistratie IS NULL
)`

// addressByPostcodeHuisnummerSQL is the finest address-tier match: nummeraanduiding on
// (postcode, huisnummer). $1 = postcode, $2 = huisnummer, $3 = valid-time date.
//
// Tie-break: n.identificatie is appended as the final ORDER BY key. Multiple nummeraanduiding
// best-known voorkomen (eindregistratie IS NULL, so at most one row per object) can share the
// same postcode+huisnummer and the same valid_at_date, e.g. a LIG/STA and a VBO both reachable
// via the same address point. Without a unique tie-break, Postgres may return either row across
// runs — a genuine idempotency bug, since re-running `load koop` could then move a permit's
// resolved point. n.identificatie is the nummeraanduiding object's own stable id and is unique
// among matching rows, so ordering by it makes the pick deterministic.
const addressByPostcodeHuisnummerSQL = `WITH ` + adrespuntCTE + `
SELECT ST_AsEWKT(v.geom) AS geom,
       (n.begingeldigheid <= $3 AND (n.eindgeldigheid IS NULL OR n.eindgeldigheid > $3)) AS valid_at_date
FROM bag_nummeraanduiding n
JOIN adrespunt v ON v.num_ref = n.identificatie
WHERE n.postcode = $1 AND n.huisnummer = $2 AND n.eindregistratie IS NULL
ORDER BY valid_at_date DESC NULLS LAST, n.identificatie
LIMIT 1`

// addressByStreetExactSQL is the second address-tier match: an exact openbareruimte.naam match.
// $1 = street, $2 = huisnummer, $3 = valid-time date.
//
// Tie-break: n.identificatie is appended as the final ORDER BY key, for the same reason as
// addressByPostcodeHuisnummerSQL — it is the unique, stable id of the matching nummeraanduiding
// object, guaranteeing the same candidate is picked under a valid_at_date tie on every run.
const addressByStreetExactSQL = `WITH ` + adrespuntCTE + `
SELECT ST_AsEWKT(v.geom) AS geom,
       (n.begingeldigheid <= $3 AND (n.eindgeldigheid IS NULL OR n.eindgeldigheid > $3)) AS valid_at_date
FROM bag_openbareruimte o
JOIN bag_nummeraanduiding n ON n.openbareruimteref = o.identificatie
    AND n.huisnummer = $2 AND n.eindregistratie IS NULL
JOIN adrespunt v ON v.num_ref = n.identificatie
WHERE lower(o.naam) = lower($1) AND o.eindregistratie IS NULL
ORDER BY valid_at_date DESC NULLS LAST, n.identificatie
LIMIT 1`

// addressByStreetFuzzySQL is the last address-tier match: a pg_trgm fuzzy street match
// (similarity(lower(naam), lower(street)) > 0.55), ranked by similarity then valid-at-date.
// $1 = street, $2 = huisnummer, $3 = valid-time date.
//
// Tie-break: n.identificatie is appended as the final ORDER BY key, after similarity and
// valid_at_date, for the same idempotency reason as the other address-tier queries — it is the
// unique, stable id of the matching nummeraanduiding object.
const addressByStreetFuzzySQL = `WITH ` + adrespuntCTE + `
SELECT ST_AsEWKT(v.geom) AS geom,
       (n.begingeldigheid <= $3 AND (n.eindgeldigheid IS NULL OR n.eindgeldigheid > $3)) AS valid_at_date
FROM bag_openbareruimte o
JOIN bag_nummeraanduiding n ON n.openbareruimteref = o.identificatie
    AND n.huisnummer = $2 AND n.eindregistratie IS NULL
JOIN adrespunt v ON v.num_ref = n.identificatie
WHERE similarity(lower(o.naam), lower($1)) > 0.55 AND o.eindregistratie IS NULL
ORDER BY similarity(lower(o.naam), lower($1)) DESC, valid_at_date DESC NULLS LAST, n.identificatie
LIMIT 1`

// postcodeExistsSQL checks the postcode tier: the PC6 exists in bag_nummeraanduiding (any
// best-known voorkomen), independent of whether a huisnummer resolved to an adresseerbaar object.
// $1 = postcode.
const postcodeExistsSQL = `SELECT EXISTS (
  SELECT 1 FROM bag_nummeraanduiding n WHERE n.postcode = $1 AND n.eindregistratie IS NULL
)`

// buurtPIPSQL is the buurt-tier floor: point-in-polygon against gebieden_buurten (never
// cbs_buurten). $1 = point X (RD), $2 = point Y (RD).
//
// Tie-break: ORDER BY b.identificatie makes the pick deterministic when a point falls in an
// overlap between two buurt polygons (adjacent buurten sharing a boundary, or overlapping source
// data) — otherwise re-running the resolver over the same point could return a different buurt.
// identificatie is the buurt object's own stable id and is unique per row.
const buurtPIPSQL = `SELECT b.identificatie, ST_AsEWKT(b.geom)
FROM gebieden_buurten b
WHERE ST_Contains(b.geom, ST_SetSRID(ST_MakePoint($1, $2), 28992))
ORDER BY b.identificatie
LIMIT 1`
