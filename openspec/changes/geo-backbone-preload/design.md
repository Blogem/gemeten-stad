## Context

Spike D (`spikes/spike-d/`, verified 2026-07-25) settled the geo backbone empirically: the national
_LV BAG 2.0 Extract_ loads via GDAL `lvbag` into PostGIS (`0363` filter, all voorkomens), the
`gebieden`/CBS polygons load in RD, and a valid-time-aware local resolver reaches 90% address
precision / 100% point-in-polygon — with no PDOK Locatieserver. The artifacts are throwaway:
`load.sh` (shell + `ogr2ogr` + `psql`), `resolve.sql`, `pip.sql`, `harvest.py` (stdlib GETs). P3
already lifted the container shapes into `deploy/compose` (`db`, `fuseki`, `gdal` sidecar,
`raw-data` volume). This change promotes the _behaviour_ into `ingest/bag`, `ingest/gebieden`,
`ingest/shared`, `load/geo`, and `location/`, wired into `pipeline ingest`/`pipeline load`.

The corrections Spike D fed back are already in the docs: `DATA_SOURCES.md` §8/§0 (national ~3.6 GB
extract, `lvbag` ST-only → monthly full reload, bitemporal load-all-voorkomens, `'%.0363%'` filter,
CBS WFS + `gebieden` GeoJSON endpoints) and `IMPLEMENTATION_PLAN.md` §4/§5. We implement against
those, not ad-hoc.

## Goals / Non-Goals

**Goals:**

- Land BAG + `gebieden` + CBS verbatim in the raw store (bronze) with provenance, idempotently.
  BAG loads for the whole municipality (`0363`); `gebieden`/CBS are harvested whole-city to match
  (BAG has no sub-municipal filter, so a Noord-only polygon set would be an avoidable asymmetry).
- Load them into PostGIS as-is (municipality filter only, all voorkomens/columns) via a
  non-destructive upsert (soft-delete absent rows, never physically drop) with the SRID and
  row-count sanity gates and the resolver's indexes (silver).
- A reusable Go `location` resolver: valid-time smallest-area resolution + point-in-polygon with
  confidence, `time_match`, and `timeMismatch`/`unresolvedLocation` caveats.
- Reproduce Spike D's sanity gates (69 buurten, 15 wijken, single SRID 28992) on a clean volume and
  the resolver's place-tier / PIP behaviour on fixtures.

**Non-Goals:**

- Ingesting permits or trees. Spike D's `harvest.py` also produced `tree_points`/`permit_points`,
  but those were the resolver's _validation inputs_ and belong to P7 (`ingest/bomen`) and the KOOP
  lane (`ingest/koop`) — not the geo backbone. P6 ingests **only** BAG + `gebieden` + CBS.
- The Phase-1 `AuditLink`, its confidence re-score, or `derive`. The resolver returns the tier +
  caveats; consuming them is later work.
- Applying daily Mutatie-Levering (ML) deltas, a PDOK Locatieserver path, `pand` footprints,
  `woonplaats`, or any reprojection (all sources are requested/loaded in RD / EPSG:28992).

## Decisions

### D1 — ogr2ogr stays for bulk loads; Go owns DDL, gates, and the resolver

The `lvbag` parse and the WFS/GeoJSON loads are genuinely easier and already-proven as `ogr2ogr`; a
Go rewrite of the driver buys nothing (plan steer: "a Go orchestrator shelling to the GDAL sidecar,
not a rewrite"). So:

- **Bulk load → `ogr2ogr` in the `gdal` sidecar**, invoked from the Go `load/geo` orchestrator via
  `os/exec`. The exact BAG/WFS/GeoJSON `ogr2ogr` invocations are ported verbatim from `load.sh`
  (same flags: `-oo AUTOCORRECT_INVALID_DATA=YES`, `-where "identificatie LIKE '%.0363%'"`,
  `-lco GEOMETRY_NAME=geom`, `PROMOTE_TO_MULTI` for LIG/STA/polygons).
- **DDL, indexes, sanity gates, and the resolver → Go + `pgx`**, connecting to PostGIS directly.
  The resolver is core project work we own (fuzzy permit-address → BAG matching); it belongs in Go
  as parameterized queries, not a `.sql` script piped through `psql`.

### D2 — how the Go orchestrator reaches the sidecar and the DB

The sidecar command prefix is a single injected value (default `docker compose -f deploy/compose/…
exec -T gdal`), so `load/geo` builds `<prefix> ogr2ogr <args>`. This keeps the compose coupling in
config, not code, and lets a test or a future k3s Job supply a different prefix (e.g. run `ogr2ogr`
in-process where GDAL is on PATH). Postgres access reuses the **existing `github.com/jackc/pgx/v5`**
dependency (already in `go.mod` from P5) via a connection string — `GS_DATABASE_URL` at runtime
(already in compose), `GS_TEST_DATABASE_URL` in integration tests (P5 convention). Both are read
from env, matching the server's config style.

### D3 — the raw landing contract (bronze), in `ingest/shared`

`ingest/shared` provides the landing-store plumbing the plan's §5 bronze layer requires: land a
source artifact as a file under the `raw-data` volume path plus a provenance sidecar (source URL,
fetch timestamp, byte size, content hash). `ingest/bag` uses it for the extract; `ingest/gebieden`
for the GeoJSON + a cached CBS WFS response. Idempotency = "present in the landing store → reuse".
Landing is **history-preserving**: a refresh lands a new dated snapshot rather than erasing the
prior provenance — we do not silently drop what we once ingested. The one pragmatic exception is
the 3.6 GB BAG blob itself: only the latest snapshot's bytes need be kept (it is a cheap,
reproducible re-fetch — the plan already flags BAG as the candidate to exempt from keep-all-raw),
but its provenance record is retained. This is the same plumbing the KOOP/bomen lanes will reuse,
so keep it minimal but shared.

### D4 — BAG loaded as-is, resolved at valid-time, reloaded non-destructively

Load rule (data rule, non-negotiable): full tables, all columns, **all voorkomens**, municipality
filter (`'%.0363%'`) only; load OPR/NUM/VBO/LIG/STA; omit `pand`/`woonplaats`. Never filter on
`status` (withdrawal is a status change, not a dropped row). The resolver selects the best-known
voorkomen (`eindregistratie IS NULL`) and prefers valid-at-date, recording `valid_at_date` vs
`any_time` in `time_match`. The address point is reached NUM → adresseerbaar object
(`hoofdadresnummeraanduidingref`); VBO gives a point, LIG/STA a polygon centroid — union all three.

**Reload is non-destructive, not `-overwrite`.** A monthly reload must never remove an entity a
downstream graph node might reference (the bitemporal rule: never overwrite history). So instead of
the spike's `ogr2ogr -overwrite` (which drops the table), `load/geo` stages each object type via
`ogr2ogr` into a `*_staging` table, then a `pgx` upsert (`MERGE`) into the target keyed by the
voorkomen identity (`identificatie` + `begingeldigheid` + `tijdstipregistratie`): new voorkomens
insert, unchanged ones no-op, and a target row absent from the staging set is **soft-deleted**
(`source_deleted_at = load_ts`), not deleted. Within BAG the absent case is rare — withdrawal is
already in-band — so this mainly guards the residual "expunged upstream" case, but it makes the
mirror safe to point at. `source_deleted_at` is provenance metadata, not a resolver filter (a
backdated audit may still want a since-expunged voorkomen). `--reset` still drops + rebuilds for a
clean dev volume.

### D5 — resolver shape

`location.Resolve(ctx, Query{Street, Huisnummer, Postcode, Point, Date}) (Result, error)` where
`Result{PlaceLevel: address|postcode|buurt, Geom, Confidence, TimeMatch, Caveats}`. It ladders
address (`(postcode,huisnummer)` → exact street → `pg_trgm` fuzzy) → postcode → buurt
(`ST_Contains` against `gebieden_buurten`, keyed by `gbdBuurtId`), mirroring `resolve.sql`. PIP
scores against `gebieden`, never CBS.

### D6 — three testing tiers, aligned to the P5 harness

The full 3.6 GB extract can't gate CI, but a real-shaped subset can. Three tiers, all reusing the
merged P5 conventions (`internal/testdb` random-schema isolation + reserved-name guard, the
`//go:build integration` tag, `GS_TEST_DATABASE_URL`, `task test:integration` /
`go test -tags=integration ./...`, and the `requireEnv` fail-loud pattern):

- **Unit (no DB):** the `ogr2ogr` argument builders (correct flags/filter per object type), the
  landing/provenance plumbing, the resolver's SQL builders, `time_match` selection, and the
  place-tier ladder logic — pure functions, fast.
- **Integration (automatic, in CI — no new services):** the existing `go-integration` job brings up
  only `db`+`fuseki` and runs host-side `go test`. So the automatic tier does **not** shell to the
  `gdal` sidecar; instead it **SQL-seeds a tiny, real-*shaped* BAG subset** (a handful of
  OPR/NUM/VBO/LIG/STA rows spanning voorkomens — incl. a withdrawn `status` and a valid-at-date vs
  any-time pair — plus a few `gebieden` polygons) directly into a `*_staging` table in a fresh
  `internal/testdb` schema, then exercises the **real** load code from staging onward
  (upsert → soft-delete → indexes → gates) and the resolver/PIP end-to-end. This is exactly the
  seam to cut: the project's own logic runs for real; only `ogr2ogr`-populates-staging (thin, fixed
  flags) is stubbed by the seed. No `ci.yml` change is needed — new `//go:build integration` tests
  are picked up automatically.
- **Full-corpus (manual/opt-in):** on the full compose stack (incl. `gdal`) against the real landed
  extract, run the **actual** `ogr2ogr`/`lvbag` execution and assert the Spike D numbers — 69/15,
  single SRID 28992, and the 90% address / 100% PIP distribution. Documented, not in default CI.

**testify:** the repo standardizes on testify (`require`/`assert`), table-driven where it fits —
`github.com/stretchr/testify` is a direct dependency and the existing tests (`cmd/*`,
`internal/testdb`) were converted from bare `testing`, so there is one uniform style to follow.

## Risks / Trade-offs

- **The 3.6 GB download / ~8-min all-NL parse can't live in default CI.** Mitigated by D6's tiers:
  units are DB-free; the automatic integration tier SQL-seeds a real-shaped subset into an isolated
  `internal/testdb` schema and runs the real load-from-staging + resolver path (no new CI services).
  Only the full-corpus reproduction of the exact Spike D numbers (69/15, 90%/100%) is manual/opt-in.
- **The `ogr2ogr`-populates-staging seam is not covered automatically** (the automatic tier seeds
  staging via SQL). Accepted trade-off to keep CI on the P5 `db`+`fuseki`-only model: the seam is
  thin and deterministic (fixed `ogr2ogr` flags, unit-tested at the arg-builder level) and is run
  for real in the manual full-corpus tier. If that seam ever regresses, promote it by adding `gdal`
  to the `go-integration` job and driving `ogr2ogr` via the D2 prefix.
- **Shelling to the sidecar couples the load path to a command prefix.** Mitigated by D2 (prefix
  injected via env/config, not hard-coded); the trade-off is a `docker compose exec` dependency in
  dev, which the plan already accepts.
- **CBS WFS is an external live service** (the spike marked its load non-fatal). Keep it best-effort
  and logged — CBS is a cross-reference only; the sanity gates depend on `gebieden`, not CBS, so a
  CBS outage does not fail the load.
- **The integration subset is frozen, so a brand-new monthly BAG shape change won't be caught until
  it hits prod.** Accepted: the subset is real (not synthetic), so it reflects a genuine shape and
  catches regressions in our code; but it can't anticipate an upstream shape the extract has never
  had. A new-shape break surfaces first in prod (or the manual full-corpus run) — we treat that as
  the signal to refresh the subset, rather than trying to predict BAG's evolution in a fixture.
- **`pgx` is a new dependency.** Justified — it is the load-bearing store driver the rest of `load`
  and `server` will use anyway; introducing it here is not speculative.
