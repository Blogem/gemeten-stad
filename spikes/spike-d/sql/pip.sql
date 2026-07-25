-- Spike D — point-in-polygon accuracy (needs only the polygons + points, no BAG).
-- Proves the gebieden polygons + CRS are correct by reproducing known buurt ids.
\pset border 2
\echo '=== POPULATION 1 (registry): tree point-in-polygon vs gbdBuurtId ground truth ==='
-- Each felled tree's stamgegevens point placed into a gebieden buurt locally;
-- compared to the row's own gbdBuurtId. Expect ~100% — any gap = polygon/CRS bug.
WITH hit AS (
  SELECT t.kap_id, t.gbd_buurt_id,
         (SELECT b.identificatie FROM gebieden_buurten b
          WHERE ST_Contains(b.geom, t.geom) LIMIT 1) AS pip_buurt
  FROM tree_points t
)
SELECT count(*)                                                   AS trees,
       count(pip_buurt)                                           AS placed_in_a_buurt,
       count(*) FILTER (WHERE pip_buurt = gbd_buurt_id)           AS correct,
       round(100.0 * count(*) FILTER (WHERE pip_buurt = gbd_buurt_id)
             / nullif(count(*), 0), 1)                            AS pct_correct
FROM hit;

\echo '=== POPULATION 2 (permits): own-geometry point-in-polygon vs Spike B tree-vote ==='
-- Replaces Spike B's remote "vote nearest trees' gbdBuurtId" hack with a local
-- spatial join against the gebieden polygons; cross-checks the two agree.
WITH hit AS (
  SELECT p.gmb_id, p.vote_buurt,
         (SELECT b.identificatie FROM gebieden_buurten b
          WHERE ST_Contains(b.geom, p.geom) LIMIT 1) AS pip_buurt
  FROM permit_points p
  WHERE p.geom IS NOT NULL
)
SELECT count(*)                                                   AS permits_with_geom,
       count(pip_buurt)                                           AS placed_in_a_noord_buurt,
       count(*) FILTER (WHERE pip_buurt = vote_buurt)             AS agree_with_spikeb_vote,
       round(100.0 * count(*) FILTER (WHERE pip_buurt = vote_buurt)
             / nullif(count(pip_buurt), 0), 1)                    AS pct_agree_of_placed
FROM hit;
