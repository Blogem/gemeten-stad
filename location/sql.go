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
const addressByPostcodeHuisnummerSQL = `WITH ` + adrespuntCTE + `
SELECT ST_AsEWKT(v.geom) AS geom,
       (n.begingeldigheid <= $3 AND (n.eindgeldigheid IS NULL OR n.eindgeldigheid > $3)) AS valid_at_date
FROM bag_nummeraanduiding n
JOIN adrespunt v ON v.num_ref = n.identificatie
WHERE n.postcode = $1 AND n.huisnummer = $2 AND n.eindregistratie IS NULL
ORDER BY valid_at_date DESC NULLS LAST
LIMIT 1`

// addressByStreetExactSQL is the second address-tier match: an exact openbareruimte.naam match.
// $1 = street, $2 = huisnummer, $3 = valid-time date.
const addressByStreetExactSQL = `WITH ` + adrespuntCTE + `
SELECT ST_AsEWKT(v.geom) AS geom,
       (n.begingeldigheid <= $3 AND (n.eindgeldigheid IS NULL OR n.eindgeldigheid > $3)) AS valid_at_date
FROM bag_openbareruimte o
JOIN bag_nummeraanduiding n ON n.openbareruimteref = o.identificatie
    AND n.huisnummer = $2 AND n.eindregistratie IS NULL
JOIN adrespunt v ON v.num_ref = n.identificatie
WHERE lower(o.naam) = lower($1) AND o.eindregistratie IS NULL
ORDER BY valid_at_date DESC NULLS LAST
LIMIT 1`

// addressByStreetFuzzySQL is the last address-tier match: a pg_trgm fuzzy street match
// (similarity(lower(naam), lower(street)) > 0.55), ranked by similarity then valid-at-date.
// $1 = street, $2 = huisnummer, $3 = valid-time date.
const addressByStreetFuzzySQL = `WITH ` + adrespuntCTE + `
SELECT ST_AsEWKT(v.geom) AS geom,
       (n.begingeldigheid <= $3 AND (n.eindgeldigheid IS NULL OR n.eindgeldigheid > $3)) AS valid_at_date
FROM bag_openbareruimte o
JOIN bag_nummeraanduiding n ON n.openbareruimteref = o.identificatie
    AND n.huisnummer = $2 AND n.eindregistratie IS NULL
JOIN adrespunt v ON v.num_ref = n.identificatie
WHERE similarity(lower(o.naam), lower($1)) > 0.55 AND o.eindregistratie IS NULL
ORDER BY similarity(lower(o.naam), lower($1)) DESC, valid_at_date DESC NULLS LAST
LIMIT 1`

// postcodeExistsSQL checks the postcode tier: the PC6 exists in bag_nummeraanduiding (any
// best-known voorkomen), independent of whether a huisnummer resolved to an adresseerbaar object.
// $1 = postcode.
const postcodeExistsSQL = `SELECT EXISTS (
  SELECT 1 FROM bag_nummeraanduiding n WHERE n.postcode = $1 AND n.eindregistratie IS NULL
)`

// buurtPIPSQL is the buurt-tier floor: point-in-polygon against gebieden_buurten (never
// cbs_buurten). $1 = point X (RD), $2 = point Y (RD).
const buurtPIPSQL = `SELECT b.identificatie, ST_AsEWKT(b.geom)
FROM gebieden_buurten b
WHERE ST_Contains(b.geom, ST_SetSRID(ST_MakePoint($1, $2), 28992))
LIMIT 1`
