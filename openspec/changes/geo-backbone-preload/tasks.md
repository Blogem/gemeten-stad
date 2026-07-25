## 1. Shared plumbing (`ingest/shared`, config)

- [x] 1.1 Add a raw landing-store helper: land a source artifact under the `raw-data` volume path and write a history-preserving provenance sidecar (source URL, fetch timestamp, byte size, content hash) — a refresh lands a new dated snapshot, never erasing prior provenance; expose an "already landed?" check for idempotency.
- [x] 1.2 Add a PostGIS connection helper over the existing `github.com/jackc/pgx/v5` dependency (already in `go.mod` from P5), reading `GS_DATABASE_URL` at runtime (same env the server uses).
- [x] 1.3 Add the GDAL-sidecar command-prefix config (env-driven, default `docker compose -f deploy/compose/compose.yaml exec -T gdal`) and a small `os/exec` runner that builds `<prefix> ogr2ogr <args>`.

## 2. BAG ingest (`ingest/bag`)

- [ ] 2.1 Fetch the *LV BAG 2.0 Extract* from the PDOK atom feed into the landing store; skip the download when already landed; record provenance.
- [ ] 2.2 Implement refresh as an idempotent full reload (replace the landed extract); no daily Mutatie-Levering path.

## 3. Boundary ingest (`ingest/gebieden`)

- [ ] 3.1 Harvest the whole-city `gebieden` buurt + wijk polygons (Datapunt API, GeoJSON, `Accept-Crs: EPSG:28992`) — all of gemeente `0363`, no stadsdeel filter — carrying `identificatie` (`gbdBuurtId`); land with provenance.
- [ ] 3.2 Land the CBS "wijken en buurten" WFS reference (`gemeentecode='GM0363'`) as a best-effort, logged cross-reference; skip when already landed.

## 4. Geo load into PostGIS (`load/geo`)

- [ ] 4.1 Ensure the `postgis` + `pg_trgm` extensions; stage the `lvbag`→PostGIS load for OPR/NUM/VBO/LIG/STA via the sidecar `ogr2ogr` into `*_staging` tables (municipality filter `'%.0363%'`, all voorkomens/columns, `AUTOCORRECT_INVALID_DATA=YES`, `GEOMETRY_NAME=geom`, `PROMOTE_TO_MULTI` for LIG/STA); omit `pand`/`woonplaats`.
- [ ] 4.2 Stage the whole-city `gebieden` buurt/wijk GeoJSON and the CBS reference (SRID 28992).
- [ ] 4.3 Upsert each staging table into its target keyed by the voorkomen identity (`identificatie` + `begingeldigheid` + `tijdstipregistratie`) via `pgx` `MERGE`: insert new voorkomens, no-op unchanged, and soft-delete rows absent from staging (`source_deleted_at = load_ts`) — never physically delete. Add a `source_deleted_at` column to the targets.
- [ ] 4.4 Create the resolver indexes: GIST on the PIP geometry columns, `pg_trgm` GIN on `lower(openbareruimte.naam)`, and the NUM/VBO/LIG/STA join + `(postcode, huisnummer)` b-tree indexes.
- [ ] 4.5 Add a reset flag that drops + rebuilds the targets from the landed data (clean dev volume); absent the flag, the reload is the non-destructive upsert.
- [ ] 4.6 Assert the sanity gates after load (single SRID 28992; the 69 Noord buurten + 15 Noord wijken present in the whole-city load) and exit non-zero on failure; log whole-city row counts.

## 5. Location resolver (`location`)

- [ ] 5.1 Implement `Resolve(Query{Street,Huisnummer,Postcode,Point,Date}) Result{PlaceLevel,Geom,Confidence,TimeMatch,Caveats}` with the address→postcode→buurt ladder and match preference (`(postcode,huisnummer)` → exact street → `pg_trgm` fuzzy).
- [ ] 5.2 Implement voorkomen selection: best-known (`eindregistratie IS NULL`), prefer valid-at-date, else any-time fallback recorded as `time_match`; never filter on `status`.
- [ ] 5.3 Reach the address point NUM → adresseerbaar object (VBO point / LIG·STA centroid) via `hoofdadresnummeraanduidingref`; union all three types.
- [ ] 5.4 Implement point-in-polygon against `gebieden_buurten` (keyed by `gbdBuurtId`), never CBS; attach `unresolvedLocation` on the buurt tier and `timeMismatch` on `any_time`. Do NOT snap the point to a nearer address/postcode — buurt is the floor when the text address does not resolve (avoids false precision).

## 6. Pipeline wiring (`cmd/pipeline`)

- [ ] 6.1 Wire the BAG + gebieden ingest work items into `pipeline ingest`, and the geo load (with the reset flag) into `pipeline load`, replacing the no-op stubs for these sources.

All Go tests use **testify** (`require`/`assert`), table-driven where it fits (already a direct dependency; the repo is uniformly testify). Integration tests use the P5 conventions: `//go:build integration`, `internal/testdb` random-schema isolation + reserved-name guard, `GS_TEST_DATABASE_URL`, the `requireEnv` fail-loud helper; they run under the existing `task test:integration` / `go-integration` CI job (which brings up only `db`+`fuseki` — no `ci.yml` change).

**Unit (no DB):**

- [ ] 7.1 The `ogr2ogr` argument builders: correct object-type filter, `'%.0363%'`, geometry/promote flags per BAG table and per polygon/WFS load.
- [x] 7.2 The landing/provenance plumbing: lands + writes provenance on first run; reports "already landed" (no re-download) on second; a refresh preserves prior provenance history.
- [ ] 7.3 The resolver SQL builders / place-tier ladder / `time_match` selection as pure functions.

**Integration (automatic, in CI — SQL-seeded real-shaped subset, no `gdal` sidecar):**

- [ ] 7.4 Add a checked-in real-*shaped* BAG subset (a handful of OPR/NUM/VBO/LIG/STA rows spanning voorkomens, incl. a withdrawn `status` and a valid-at-date vs any-time pair, + a few `gebieden` polygons) as SQL/`COPY` seed data; load it into a `*_staging` table in a fresh `internal/testdb` schema (`GS_TEST_DATABASE_URL`).
- [ ] 7.5 Exercise the real load code from staging onward on the subset: upsert (new voorkomen inserts, re-run is a no-op, an absent object is soft-deleted with `source_deleted_at`) → indexes → sanity gates.
- [ ] 7.6 Resolver against the seeded subset: address tier via `(postcode,huisnummer)` and via street; postcode-only fallback; buurt PIP fallback with `unresolvedLocation` (and not snapped to a nearer address); valid-at-date preferred; `any_time` fallback flagged; a since-withdrawn address still resolves; a point with a known `gbdBuurtId` places into that same buurt.

**Full-corpus (manual/opt-in — real `ogr2ogr`/`lvbag` execution):**

- [ ] 7.7 Document the opt-in full-corpus gate (full compose stack incl. `gdal` + the real landed extract) that runs the actual `ogr2ogr`/`lvbag` load and asserts the exact Spike D numbers (69/15, single SRID 28992, 90% address / 100% PIP distribution); not run in default CI.

## 8. Docs & cleanup

- [ ] 8.1 Add a `location/`-or-`deploy` README note documenting the load recipe, the sidecar prefix/env knobs, and how to run the integration gate.
- [ ] 8.2 Remove `spikes/spike-d/` (its behaviour now lives in the packages) and mark P6 done in `docs/PHASE_0_PLAN.md`.
