## Why

Phase 1's deterministic backbone (`IMPLEMENTATION_PLAN.md` §6) audits the tree-felling/replanting
obligation by joining the KOOP permit stream against the municipal tree registry. That registry —
`kapenherplant` (the felling→replanting lifecycle, 35,202 rows) + `stamgegevens` (per-tree master
data, 323,728 municipally-managed rows) — is the value-store half of the audit and the core
structured dataset the whole vertical is built around (`DATA_THREAD_TREES.md`). This is Phase 0 ·
**P7**: preload it full-city into PostGIS, in parallel with P6's geo backbone (both need only the
repo skeleton + dev compose).

## What Changes

- **Fetch + land `kapenherplant` and `stamgegevens` (bronze).** `ingest/bomen` pages through the
  Amsterdam Datapunt `bomen` DSO API (`api.data.amsterdam.nl/v1/bomen/{kapenherplant,stamgegevens}`)
  full-city, lands each page verbatim into the raw store with provenance (source URL, fetch
  timestamp, content hash), and treats a full re-page as an idempotent scheduled refresh (~weekly,
  per `IMPLEMENTATION_PLAN.md` §5's cadence table) — no daily-delta path, matching the source (no
  mutation feed is documented for this API).
- **Load both registries into typed PostGIS tables as-is (silver).** `load/bomen` stages each
  paged export into a `*_staging` table, then upserts into the target keyed by the source's natural
  row id: new rows insert, unchanged rows no-op, and a target row absent from the new export is
  **soft-deleted** (`source_deleted_at`), never physically dropped — the same non-destructive
  stage→upsert→soft-delete shape `load/geo` uses for BAG (P6), so no entity a graph node may
  reference (e.g. a felled tree's `boomId`) disappears on a routine refresh.
- **Materialize the `kapenherplant` → `stamgegevens` join at load time.** Per `DATA_SOURCES.md`
  §2a, `kapenherplant` carries no queryable geometry of its own; a felled tree's point comes only
  from joining `boomId` → `stamgegevens`, and that join alone resolves only ~71% of felled rows
  because a completed replant retires the old `boomId` and issues a `boomNieuwId` for the new tree.
  `load/bomen` resolves `boomId` first, falls back to `boomNieuwId` on a miss, and stores the
  resolved point + which key resolved it (or `unresolved`) alongside each `kapenherplant` row — no
  BAG dependency: nearest address and buurt are already carried on the source rows
  (`dichtstbijzijndeBagAdres`/`Postcode`, `gbdBuurtId`).
- **Wire the load path into `pipeline ingest`/`pipeline load`.** The no-op stage subcommands gain
  the bomen work items.
- **Test against the P5 harness.** Integration tests reuse `internal/testdb` (isolated schema +
  reserved-name guard), the `//go:build integration` tag, and `GS_TEST_DATABASE_URL`, running in the
  existing `go-integration` CI job with a checked-in real-shaped subset (no live API call in
  default CI); a full-corpus reproduction of the exact row counts is a manual/opt-in gate.

## Capabilities

### New Capabilities

- `bomen-ingest`: Fetch and land (bronze) the full-city `kapenherplant` and `stamgegevens`
  registries from the Datapunt `bomen` API verbatim, paged, with provenance; idempotent scheduled
  full reload.
- `bomen-load`: Load the landed bomen sources into typed PostGIS tables (silver) via non-destructive
  stage→upsert→soft-delete, including the `boomId`-then-`boomNieuwId` join that resolves each
  `kapenherplant` row to a point.

### Modified Capabilities

<!-- None — this is the first change to touch these packages; no existing specs. -->

## Impact

- **New code:** `ingest/bomen/`, `load/bomen/`; new subcommand behaviour in `cmd/pipeline`.
- **Dependencies:** `github.com/jackc/pgx/v5` (already in `go.mod` from P5) for DDL, staging,
  upsert/soft-delete, and the join; `testify` for the Go tests (already added for P6, or added
  here if this change lands first — the two Wave-C items are parallel, not sequenced). No new
  external dependency: the Datapunt API needs no `gdal`/`ogr2ogr` sidecar, just paged HTTP+JSON.
- **Shared plumbing:** if `ingest/shared`'s raw-landing/provenance helper (introduced by the
  parallel P6 change) has already landed, reuse it as-is; otherwise add the same minimal
  landing-store helper here and let the two converge (whichever change merges second dedupes it).
- **Infra:** relies on the P3 dev compose (`db`, `raw-data` volume). No compose service changes —
  no sidecar needed for this source.
- **Data:** populates PostGIS with the full `kapenherplant` (35,202 rows) and `stamgegevens`
  (323,728 rows) tables on the shared named volume; the later `pipeline dump` (P9) snapshots them.
- **Docs:** `DATA_SOURCES.md` §2a and `IMPLEMENTATION_PLAN.md` §5/§6 already carry the source's
  shape and quirks; this change implements against them. `PHASE_0_PLAN.md` P7 marked done on
  completion.
