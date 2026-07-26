## Why

Spike D (`spikes/spike-d/`) proved the geo backbone — bulk BAG + `gebieden`/CBS polygons in local
PostGIS — loads and resolves locally at 90% address precision / 100% point-in-polygon, with **no
PDOK Locatieserver**. That proof lives in throwaway shell (`load.sh`, `resolve.sql`, `harvest.py`).
Phase 1's deterministic backbone needs this promoted into the real `stage × source` packages so
every location on both sides of the audit resolves against our own copy. This is Phase 0 · **P6**.

## What Changes

- **Fetch + land the BAG extract (bronze).** `ingest/bag` downloads the national *LV BAG 2.0
  Extract* from the PDOK atom feed into the raw landing store with provenance, idempotently (a
  monthly full reload; skip if already present — `lvbag` has no daily-mutatie path).
- **Fetch + land the boundary geometries (bronze).** `ingest/gebieden` harvests Amsterdam
  whole-city `gebieden` buurt/wijk polygons (Datapunt GeoJSON, RD) and lands the CBS "wijken en
  buurten" WFS reference, with provenance.
- **Load the geo sources into PostGIS as-is (silver).** `load/geo` orchestrates the `lvbag`→PostGIS
  load via the GDAL sidecar — municipality filter only (`'%.0363%'`), all voorkomens, all columns —
  plus the polygon/WFS loads, the GIST + `pg_trgm` indexes, and the SRID/row-count sanity gates. The
  reload is **non-destructive** (stage → upsert by voorkomen identity; rows absent from a new extract
  are soft-deleted with `source_deleted_at`, never dropped), so no entity a graph node may reference
  is lost; a reset flag rebuilds a clean dev volume.
- **Port the resolver into `location/` (Go).** The valid-time-aware, smallest-area resolver
  (`address 0.90 → postcode 0.70 → buurt 0.50`) from `resolve.sql`/`pip.sql` becomes a reusable Go
  component: best-known voorkomen selection (`eindregistratie IS NULL`), valid-at-date preference
  with an any-time fallback recorded as `time_match`, point-in-polygon against the `gebieden`
  polygons, and the `timeMismatch`/`unresolvedLocation` caveats the Phase-1 `AuditLink` consumes.
- **Wire the load path into `pipeline ingest`/`pipeline load`.** The no-op stage subcommands gain
  the geo work items.
- **Test against the P5 harness.** Integration tests reuse `internal/testdb` (isolated schema +
  reserved-name guard), the `//go:build integration` tag, and `GS_TEST_DATABASE_URL`, running in the
  existing `go-integration` CI job — the automatic tier SQL-seeds a real-shaped subset (no `gdal`
  sidecar); the real `ogr2ogr`/`lvbag` execution is a manual full-corpus gate.
- Retire `spikes/spike-d/` as the source of truth once the packages reproduce its sanity gates.

## Capabilities

### New Capabilities

- `geo-ingest`: Fetch and land (bronze) the BAG bulk extract and the `gebieden`/CBS boundary
  geometries verbatim into the raw store, with provenance; idempotent monthly full reload.
- `geo-load`: Load the landed geo sources into PostGIS as-is (municipality filter only, all
  voorkomens, all columns) with SRID/row-count sanity gates and the resolver's indexes; idempotent
  and incremental (silver).
- `location-resolver`: Resolve an address/point at a given valid-time to the smallest place it can
  be confidently placed in (address → postcode → buurt) with a confidence and the
  `timeMismatch`/`unresolvedLocation` caveats, entirely against local PostGIS.

### Modified Capabilities

<!-- None — this is the first change to touch these packages; no existing specs. -->

## Impact

- **New code:** `ingest/bag/`, `ingest/gebieden/`, `ingest/shared/` (http/geo/raw-landing plumbing),
  `load/geo/`, `location/`; new subcommand behaviour in `cmd/pipeline`.
- **Dependencies:** reuse the existing `github.com/jackc/pgx/v5` (already in `go.mod` from P5) for
  DDL, upsert/soft-delete, indexes, sanity gates, and the resolver's parameterized SQL; add
  `testify` for the Go tests. The `lvbag`/WFS/GeoJSON bulk loads stay on `ogr2ogr` in the GDAL
  sidecar (already in `deploy/compose`).
- **Infra:** relies on the P3 dev compose (`db`, `gdal`, `raw-data` volume). May add a
  landing-path/target-name env knob; no compose service changes expected.
- **Data:** populates PostGIS with the `0363` BAG tables + `gebieden`/CBS polygons on the shared
  named volume; the later `pipeline dump` (P9) snapshots them.
- **Docs:** `DATA_SOURCES.md` §8 / §0 and `IMPLEMENTATION_PLAN.md` §4 already carry Spike D's
  corrections; this change implements against them. `PHASE_0_PLAN.md` P6 marked done on completion.
