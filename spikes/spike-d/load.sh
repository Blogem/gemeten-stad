#!/usr/bin/env bash
# Spike D — load the geo bulk backbone into PostGIS, entirely in containers.
#
#   BAG LV 2.0 Extract (national) --lvbag--> PostGIS, filtered to gemeente 0363
#   gebieden buurt/wijk polygons (harvest.py GeoJSON, RD) --> PostGIS  [PRIMARY]
#   CBS "wijken en buurten" WFS (RD) --> PostGIS                       [x-ref]
#   tree points + permit points (harvest.py GeoJSON, RD) --> PostGIS
#
# Incremental & resumable: the ~3.6 GB extract is downloaded once (see README /
# the curl in the repo), unzipped once, and each table is skipped if already
# populated — so a second run is a fast no-op. `./load.sh --reset` drops the
# schema and re-loads (the downloaded extract is still reused).
#
# Host needs only Docker + unzip; ogr2ogr/psql run inside the containers.
set -euo pipefail
cd "$(dirname "$0")"

EXTRACT="data/lvbag-extract-nl.zip"
BAGDIR="data/bag"                 # inner per-object-type zips land here
PG="PG:host=db dbname=geo user=geo password=geo"
CBS_WFS="https://service.pdok.nl/cbs/wijkenbuurten/2024/wfs/v1_0"

psql()  { docker compose exec -T db psql -U geo -d geo -v ON_ERROR_STOP=1 "$@"; }
ogr()   { docker compose exec -T gdal ogr2ogr "$@"; }
count() { psql -tAc "SELECT count(*) FROM $1" 2>/dev/null || echo 0; }
have()  { [ "$(count "$1")" -gt 0 ]; }

if [ "${1:-}" = "--reset" ]; then
  echo "== reset: dropping schema =="
  docker compose up -d db >/dev/null
  psql -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" || true
fi

echo "== bring up containers =="
docker compose up -d
# wait for the healthcheck
until [ "$(docker compose ps db --format '{{.Health}}')" = "healthy" ]; do sleep 2; done
psql -c "CREATE EXTENSION IF NOT EXISTS postgis; CREATE EXTENSION IF NOT EXISTS pg_trgm;" >/dev/null

# ---------------------------------------------------------------- BAG (lvbag)
if [ ! -f "$EXTRACT" ]; then
  echo "!! $EXTRACT missing — download it first (see README), then re-run." >&2
  exit 1
fi
if [ -z "$(ls -A "$BAGDIR" 2>/dev/null || true)" ]; then
  echo "== unzip outer extract -> the object-type inner zips we load =="
  mkdir -p "$BAGDIR"
  unzip -o -j "$EXTRACT" '9999OPR*' '9999NUM*' '9999VBO*' '9999LIG*' '9999STA*' -d "$BAGDIR" >/dev/null
fi

# Load BAG as-is, filtered ONLY on municipality (0363). No column projection, no other
# row filter. We load every object type that carries an ADDRESS or a STREET NAME:
#   OPR openbareruimte  (street names, 11 MB)                 — street match
#   NUM nummeraanduiding (postcode+huisnummer, 353 MB)        — the strong address key
#   VBO verblijfsobject  (address POINT, 1.2 GB)              — adresseerbaar object
#   LIG ligplaats        (houseboat berth, addressed, 1.7 MB) — adresseerbaar object
#   STA standplaats      (mobile-home plot, addressed, 6 MB)  — adresseerbaar object
# VBO+LIG+STA are the three "adresseerbaar object" types, so this is COMPLETE address-point
# coverage. Only whole tables we don't need are left out (allowed): PND pand (2.0 GB — building
# FOOTPRINTS only; every address point already lives on VBO/LIG/STA, so no address is lost),
# WPL woonplaats (city names), and the auxiliary Inactief/InOnderzoek/NietBag/GEM-WPL files
# (withdrawn objects are already present in the main files as status='… ingetrokken').
load_bag() {  # $1=code $2=table $3..=extra ogr flags
  local code="$1" table="$2"; shift 2
  if have "$table"; then echo "   $table already loaded ($(count "$table") rows) — skip"; return; fi
  local zip; zip=$(cd "$BAGDIR" && ls -1 9999${code}*.zip 2>/dev/null | head -1 || true)
  if [ -z "$zip" ]; then echo "!! no inner zip matching 9999$code*.zip in $BAGDIR" >&2; return 1; fi
  echo "== lvbag $code -> $table  (from $zip, filtered 0363, ALL voorkomens) =="
  # identificatie is the IMBAG-URI form NL.IMBAG.<Type>.<0363...>, so match the
  # gemeente as '%.0363%'. We load ALL voorkomens (temporal versions), NOT just the
  # current one: BAG is bitemporal and our audit works with backdated permits, so a
  # location must be matched against the BAG state VALID AT the permit date, not the
  # current snapshot. Resolution selects the right voorkomen (see sql/resolve.sql):
  #   valid-time = begingeldigheid <= D < eindgeldigheid ; best-known = eindregistratie IS NULL.
  # Withdrawal/demolition is a `status` change (Naamgeving/Verblijfsobject ingetrokken)
  # on the latest voorkomen — the object stays resolvable, it is not dropped.
  ogr -f PostgreSQL "$PG" -oo AUTOCORRECT_INVALID_DATA=YES \
      "/vsizip//data/bag/$zip" \
      -where "identificatie LIKE '%.0363%'" \
      -nln "$table" -lco GEOMETRY_NAME=geom -lco FID=ogc_fid -lco SPATIAL_INDEX=NONE \
      -overwrite "$@"
  echo "   -> $(count "$table") rows"
}

load_bag OPR bag_openbareruimte
load_bag NUM bag_nummeraanduiding
load_bag VBO bag_verblijfsobject
load_bag LIG bag_ligplaats    -nlt PROMOTE_TO_MULTI
load_bag STA bag_standplaats  -nlt PROMOTE_TO_MULTI

# ---------------------------------------------------------------- polygons + points
load_geojson() {  # $1=file $2=table $3..=extra flags
  local file="$1" table="$2"; shift 2
  if have "$table"; then echo "   $table already loaded — skip"; return; fi
  echo "== $file -> $table =="
  ogr -f PostgreSQL "$PG" "/data/$file" -a_srs EPSG:28992 \
      -nln "$table" -lco GEOMETRY_NAME=geom -lco FID=ogc_fid -overwrite "$@"
  echo "   -> $(count "$table") rows"
}
load_geojson noord_buurten.geojson gebieden_buurten -nlt PROMOTE_TO_MULTI
load_geojson noord_wijken.geojson  gebieden_wijken  -nlt PROMOTE_TO_MULTI
load_geojson tree_points.geojson   tree_points
load_geojson permit_points.geojson permit_points

if ! have cbs_buurten; then
  echo "== CBS wijkenbuurten WFS -> cbs_buurten (RD, GM0363) =="
  ogr -f PostgreSQL "$PG" WFS:"$CBS_WFS" wijkenbuurten:buurten \
      -where "gemeentecode='GM0363'" \
      -nln cbs_buurten -lco GEOMETRY_NAME=geom -lco FID=ogc_fid \
      -nlt PROMOTE_TO_MULTI -overwrite || echo "!! CBS WFS load failed (non-fatal)"
  echo "   -> $(count cbs_buurten) rows"
fi

# ---------------------------------------------------------------- indexes
echo "== indexes =="
psql >/dev/null <<'SQL'
CREATE INDEX IF NOT EXISTS ix_vbo_geom      ON bag_verblijfsobject USING gist (geom);
CREATE INDEX IF NOT EXISTS ix_buurt_geom    ON gebieden_buurten    USING gist (geom);
CREATE INDEX IF NOT EXISTS ix_tree_geom     ON tree_points         USING gist (geom);
CREATE INDEX IF NOT EXISTS ix_permit_geom   ON permit_points       USING gist (geom);
CREATE INDEX IF NOT EXISTS ix_opr_naam_trgm ON bag_openbareruimte  USING gin  (lower(naam) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS ix_num_pc_hn     ON bag_nummeraanduiding (postcode, huisnummer);
CREATE INDEX IF NOT EXISTS ix_num_ident     ON bag_nummeraanduiding (identificatie);       -- NUM<-VBO join
CREATE INDEX IF NOT EXISTS ix_vbo_ref       ON bag_verblijfsobject  (hoofdadresnummeraanduidingref);
CREATE INDEX IF NOT EXISTS ix_lig_ref       ON bag_ligplaats        (hoofdadresnummeraanduidingref);
CREATE INDEX IF NOT EXISTS ix_sta_ref       ON bag_standplaats      (hoofdadresnummeraanduidingref);
CREATE INDEX IF NOT EXISTS ix_opr_ident     ON bag_openbareruimte   (identificatie);
CREATE INDEX IF NOT EXISTS ix_num_opr_ref   ON bag_nummeraanduiding (openbareruimteref);
SQL

# ---------------------------------------------------------------- sanity gates
echo ""
echo "== SANITY GATES =="
psql <<'SQL'
\pset border 2
SELECT 'gebieden_buurten (expect 69)' AS gate, count(*) FROM gebieden_buurten
UNION ALL SELECT 'gebieden_wijken (expect 15)', count(*) FROM gebieden_wijken
UNION ALL SELECT 'bag_nummeraanduiding 0363 (all voorkomens)', count(*) FROM bag_nummeraanduiding
UNION ALL SELECT 'bag_verblijfsobject 0363 (all voorkomens)',  count(*) FROM bag_verblijfsobject
UNION ALL SELECT 'bag_openbareruimte 0363 (all voorkomens)',   count(*) FROM bag_openbareruimte
UNION ALL SELECT 'bag_ligplaats 0363 (all voorkomens)',        count(*) FROM bag_ligplaats
UNION ALL SELECT 'bag_standplaats 0363 (all voorkomens)',      count(*) FROM bag_standplaats
UNION ALL SELECT '  ^ distinct nummeraanduiding objects', count(DISTINCT identificatie) FROM bag_nummeraanduiding
UNION ALL SELECT 'tree_points',               count(*) FROM tree_points
UNION ALL SELECT 'permit_points',             count(*) FROM permit_points;

-- every loaded geometry must share SRID 28992 or ST_Contains is silently empty
SELECT 'distinct SRIDs (expect only 28992)' AS gate,
       string_agg(DISTINCT srid::text, ',') AS srids FROM (
  SELECT ST_SRID(geom) srid FROM gebieden_buurten
  UNION ALL SELECT ST_SRID(geom) FROM bag_verblijfsobject
  UNION ALL SELECT ST_SRID(geom) FROM tree_points
  UNION ALL SELECT ST_SRID(geom) FROM permit_points
) s;
SQL
echo "== done =="
