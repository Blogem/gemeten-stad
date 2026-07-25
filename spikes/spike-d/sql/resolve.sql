-- Spike D — free-text/reference address -> local BAG resolution + the place-granularity
-- lift Spike B's confidence model awaits (buurt 0.50 -> postcode 0.70 -> address 0.90).
-- Runs entirely against the local PostGIS — no PDOK Locatieserver.
--
-- BAG is bitemporal and loaded with ALL voorkomens, so resolution is done AT THE PERMIT'S
-- VALID-TIME (its publication date), not against the current snapshot: we match a 2022
-- permit to the address state as it was in 2022. Voorkomen selection:
--   valid-time      : begingeldigheid <= D AND (eindgeldigheid IS NULL OR eindgeldigheid > D)
--   best-known      : eindregistratie IS NULL     (drop versions superseded by a correction)
-- Withdrawal/demolition is a `status` change on the latest voorkomen, not a dropped row.
--
-- Join chain: NUM(postcode,huisnummer) valid@D  ->  VBO.hoofdadresnummeraanduidingref = NUM.id
--   -> VBO.geom (the address POINT).   Street path: OPR.naam -> NUM.openbareruimteref.
\pset border 2
\set ON_ERROR_STOP on

-- ========================================================================= 1b
-- Registry nearest-address -> BAG. tree_points.bag_adres = "Ekangala 26" (+ postcode).
-- Best-known voorkomen, any valid-time (a felled tree's address may since be withdrawn).
\echo '=== POPULATION 1b: registry nearest-address -> BAG resolvable rate ==='
WITH t AS (
  SELECT kap_id, upper(replace(bag_postcode,' ','')) AS postcode,
         (substring(bag_adres from '([0-9]+)'))::int AS huisnummer
  FROM tree_points WHERE bag_adres IS NOT NULL AND bag_postcode IS NOT NULL)
SELECT count(*) AS trees_with_addr,
       count(*) FILTER (WHERE EXISTS (
         SELECT 1 FROM bag_nummeraanduiding n
         WHERE n.postcode=t.postcode AND n.huisnummer=t.huisnummer
           AND n.eindregistratie IS NULL)) AS resolved_to_bag_num,
       round(100.0*count(*) FILTER (WHERE EXISTS (
         SELECT 1 FROM bag_nummeraanduiding n
         WHERE n.postcode=t.postcode AND n.huisnummer=t.huisnummer
           AND n.eindregistratie IS NULL)) / nullif(count(*),0),1) AS pct
FROM t;

-- ========================================================================== 2
\echo '=== POPULATION 2: build permit_resolved (best place tier, at permit valid-time) ==='
DROP TABLE IF EXISTS permit_resolved;
CREATE TABLE permit_resolved AS
WITH p AS (
  SELECT gmb_id, marker, street, huisnummer,
         upper(replace(postcode,' ','')) AS postcode,
         nullif(available::text,'')::date AS d,
         geom AS permit_geom, vote_buurt
  FROM permit_points
),
-- The address POINT lives on the adresseerbaar object: verblijfsobject (point),
-- ligplaats / standplaats (polygon -> centroid). Union all three so houseboat and
-- standplaats addresses resolve, not just buildings.
adrespunt AS (
  SELECT hoofdadresnummeraanduidingref AS num_ref, geom
    FROM bag_verblijfsobject WHERE eindregistratie IS NULL
  UNION ALL
  SELECT hoofdadresnummeraanduidingref, ST_Centroid(geom)
    FROM bag_ligplaats     WHERE eindregistratie IS NULL
  UNION ALL
  SELECT hoofdadresnummeraanduidingref, ST_Centroid(geom)
    FROM bag_standplaats   WHERE eindregistratie IS NULL
),
-- All address candidates across the three match methods. We match ANY best-known voorkomen
-- (eindregistratie IS NULL) and RANK by whether it was valid at the permit date, so a permit
-- still links to a location even when the date doesn't line up (permit data can be unreliable) —
-- but we RECORD the time-match quality on the relation (time_match) so the link is flagged, not
-- silently trusted. Preference: valid-at-date over any-time; then pc > street > fuzzy; then nearest.
addr_cand AS (
  SELECT p.gmb_id, 'address_postcode' AS method, 0 AS mrank,
         (n.begingeldigheid <= p.d AND (n.eindgeldigheid IS NULL OR n.eindgeldigheid > p.d)) AS valid_at_date,
         v.geom AS bag_geom, ST_Distance(v.geom, p.permit_geom) AS dist
  FROM p
  JOIN bag_nummeraanduiding n ON n.postcode=p.postcode AND n.huisnummer=p.huisnummer
       AND n.eindregistratie IS NULL
  JOIN adrespunt v ON v.num_ref = n.identificatie
  WHERE p.postcode IS NOT NULL AND p.huisnummer IS NOT NULL
  UNION ALL
  SELECT p.gmb_id, 'address_street', 1,
         (n.begingeldigheid <= p.d AND (n.eindgeldigheid IS NULL OR n.eindgeldigheid > p.d)),
         v.geom, ST_Distance(v.geom, p.permit_geom)
  FROM p
  JOIN bag_openbareruimte  o ON lower(o.naam)=lower(p.street) AND o.eindregistratie IS NULL
  JOIN bag_nummeraanduiding n ON n.openbareruimteref=o.identificatie AND n.huisnummer=p.huisnummer
       AND n.eindregistratie IS NULL
  JOIN adrespunt v ON v.num_ref = n.identificatie
  WHERE p.street IS NOT NULL AND p.huisnummer IS NOT NULL
  UNION ALL
  SELECT p.gmb_id, 'address_fuzzy', 2,
         (n.begingeldigheid <= p.d AND (n.eindgeldigheid IS NULL OR n.eindgeldigheid > p.d)),
         v.geom, ST_Distance(v.geom, p.permit_geom)
  FROM p
  JOIN bag_openbareruimte  o ON o.eindregistratie IS NULL AND similarity(lower(o.naam),lower(p.street)) > 0.55
  JOIN bag_nummeraanduiding n ON n.openbareruimteref=o.identificatie AND n.huisnummer=p.huisnummer
       AND n.eindregistratie IS NULL
  JOIN adrespunt v ON v.num_ref = n.identificatie
  WHERE p.street IS NOT NULL AND p.huisnummer IS NOT NULL
),
addr AS (   -- one best address per permit, carrying the time-match quality
  SELECT DISTINCT ON (gmb_id) gmb_id, method, bag_geom, dist,
         CASE WHEN valid_at_date THEN 'valid_at_date' ELSE 'any_time' END AS time_match
  FROM addr_cand
  ORDER BY gmb_id, valid_at_date DESC, mrank, dist NULLS LAST
),
pc_area AS (   -- PC6 exists in BAG (any best-known voorkomen) even if the huisnummer didn't resolve
  SELECT DISTINCT p.gmb_id
  FROM p JOIN bag_nummeraanduiding n ON n.postcode=p.postcode AND n.eindregistratie IS NULL
  WHERE p.postcode IS NOT NULL AND p.gmb_id NOT IN (SELECT gmb_id FROM addr)
)
SELECT p.gmb_id, p.marker, p.postcode, p.huisnummer, p.d AS permit_date, p.vote_buurt,
       p.permit_geom, a.method, a.bag_geom, a.time_match,
  CASE WHEN a.gmb_id IS NOT NULL THEN 'address'
       WHEN pc.gmb_id IS NOT NULL THEN 'postcode'
       WHEN (SELECT b.identificatie FROM gebieden_buurten b
             WHERE ST_Contains(b.geom, p.permit_geom) LIMIT 1) IS NOT NULL THEN 'buurt'
       ELSE 'fail' END AS place_level,
  round(a.dist::numeric,1) AS dist_to_own_m,
  (SELECT b.identificatie FROM gebieden_buurten b
   WHERE ST_Contains(b.geom, p.permit_geom) LIMIT 1) AS pip_buurt
FROM p
LEFT JOIN addr a ON a.gmb_id = p.gmb_id
LEFT JOIN pc_area pc ON pc.gmb_id = p.gmb_id;

\echo '--- place-granularity distribution (the confidence lift Spike B awaits) ---'
SELECT place_level, count(*) AS permits,
       round(100.0*count(*)/sum(count(*)) OVER (),1) AS pct,
       CASE place_level WHEN 'address' THEN 0.90 WHEN 'postcode' THEN 0.70
                        WHEN 'buurt' THEN 0.50 ELSE NULL END AS spikeb_place_score
FROM permit_resolved GROUP BY place_level
ORDER BY min(CASE place_level WHEN 'address' THEN 1 WHEN 'postcode' THEN 2
                              WHEN 'buurt' THEN 3 ELSE 4 END);

\echo '--- address links by time-match quality (any_time = temporal fallback, flag on relation) ---'
SELECT time_match, count(*) AS permits,
       round(avg(dist_to_own_m),1) AS mean_m,
       round((percentile_cont(0.5) WITHIN GROUP (ORDER BY dist_to_own_m))::numeric,1) AS median_m
FROM permit_resolved WHERE place_level='address' GROUP BY time_match ORDER BY time_match;

\echo '--- address-tier method + distance to the permit own point (ground truth) ---'
SELECT coalesce(method,'(none)') AS method, count(*) AS permits,
       round(avg(dist_to_own_m),1) AS mean_m,
       round((percentile_cont(0.5) WITHIN GROUP (ORDER BY dist_to_own_m))::numeric,1) AS median_m,
       max(dist_to_own_m) AS max_m
FROM permit_resolved WHERE place_level='address' GROUP BY method ORDER BY permits DESC;

\echo '--- reference-marked ("nabij"/"t.h.v.") permits: how they resolve ---'
SELECT coalesce(marker,'(none: normal address)') AS marker, place_level, count(*)
FROM permit_resolved GROUP BY marker, place_level ORDER BY marker NULLS LAST, place_level;

\echo '--- local point-in-polygon buurt vs Spike B remote tree-vote (agreement) ---'
SELECT count(*) AS permits,
       count(*) FILTER (WHERE pip_buurt = vote_buurt) AS agree,
       count(*) FILTER (WHERE pip_buurt IS NULL) AS pip_outside_noord_polys,
       round(100.0*count(*) FILTER (WHERE pip_buurt = vote_buurt)
             / nullif(count(*) FILTER (WHERE pip_buurt IS NOT NULL),0),1) AS pct_agree_of_placed
FROM permit_resolved;

-- ================================================================ TEMPORAL
-- Why load ALL voorkomens: matching at the permit date vs the current snapshot differs.
\echo '=== TEMPORAL: address valid AT the 2022 permit date vs active NOW ==='
WITH p AS (
  SELECT gmb_id, upper(replace(postcode,' ','')) pc, huisnummer hn, nullif(available::text,'')::date d
  FROM permit_points WHERE postcode IS NOT NULL AND huisnummer IS NOT NULL)
SELECT
  count(*) AS permits_with_pc_hn,
  count(*) FILTER (WHERE EXISTS (SELECT 1 FROM bag_nummeraanduiding n
      WHERE n.postcode=p.pc AND n.huisnummer=p.hn
        AND n.begingeldigheid<=p.d AND (n.eindgeldigheid IS NULL OR n.eindgeldigheid>p.d)
        AND n.eindregistratie IS NULL)) AS valid_at_permit_date,
  count(*) FILTER (WHERE EXISTS (SELECT 1 FROM bag_nummeraanduiding n
      WHERE n.postcode=p.pc AND n.huisnummer=p.hn
        AND n.eindgeldigheid IS NULL AND n.status='Naamgeving uitgegeven')) AS active_now,
  count(*) FILTER (WHERE
        EXISTS (SELECT 1 FROM bag_nummeraanduiding n WHERE n.postcode=p.pc AND n.huisnummer=p.hn
          AND n.begingeldigheid<=p.d AND (n.eindgeldigheid IS NULL OR n.eindgeldigheid>p.d) AND n.eindregistratie IS NULL)
    AND NOT EXISTS (SELECT 1 FROM bag_nummeraanduiding n WHERE n.postcode=p.pc AND n.huisnummer=p.hn
          AND n.eindgeldigheid IS NULL AND n.status='Naamgeving uitgegeven')
    ) AS existed_at_permit_date_but_gone_now
FROM p;
