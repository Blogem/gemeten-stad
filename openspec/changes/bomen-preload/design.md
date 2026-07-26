## Context

`kapenherplant` and `stamgegevens` are two sub-datasets of the Amsterdam Datapunt `bomen` DSO API
(`DATA_SOURCES.md` §2a): `stamgegevens` is per-tree master data (species, plant year, geometry,
`gbdBuurtId`) for ~323,728 municipally-managed trees; `kapenherplant` is the full felling→replanting
lifecycle (permit dates, felling/replanting execution dates, nearest BAG address, buurt, the
species owed) for 35,202 rows city-wide (Spike C's full pull). Neither dataset is bitemporal like
BAG (P6) — no voorkomens, just a flat row per tree/lifecycle — but `kapenherplant` carries no
geometry of its own and must be joined to `stamgegevens` via `boomId`, with a documented
`boomNieuwId` fallback (a completed replant retires the felled tree's id and issues a new one for
the replacement), reaching ~98% resolution instead of ~71% via `boomId` alone. This change is P7
in `PHASE_0_PLAN.md`, in Wave C alongside P6 (geo backbone) — both depend only on the repo skeleton
(P1) and dev compose (P3), so they proceed in parallel, not sequentially. The staging→upsert→
soft-delete shape below deliberately mirrors P6's `load/geo` design (same non-destructive-reload
rationale, same `internal/testdb`/P5 test harness) so the two `load/*` packages stay consistent,
without this change depending on P6's code landing first.

## Goals / Non-Goals

**Goals:**

- Land both registries verbatim in the raw store (bronze) with provenance, idempotently, as a
  paged full reload (no documented delta/mutation feed for this API).
- Load them into typed PostGIS tables as-is (silver) via a non-destructive upsert (soft-delete rows
  absent from a refresh, never physically drop) keyed by each source's natural row id.
- Materialize the `kapenherplant` → `stamgegevens` point resolution (`boomId` then `boomNieuwId`
  fallback) at load time, recording which key resolved it (or that it stayed unresolved).
- Reproduce Spike C's city-wide counts (35,202 `kapenherplant` / 323,728 `stamgegevens`) on a clean
  volume, and let a spot-checked tree resolve to a point + buurt + nearest address purely from the
  loaded columns.

**Non-Goals:**

- BAG-backed address/buurt resolution. Nearest address (`dichtstbijzijndeBagAdres`/`Postcode`) and
  buurt (`gbdBuurtId`) are already columns on the source rows — this change does not touch
  `location/` or BAG (P6's concern).
- Species/project extraction from permit prose (Phase 2 NER) — out of scope; the registry's own
  `soortnaam`/`toeTePassenBoomsoort` columns are loaded as-is, null where the source is null.
- `derive` (the `AuditLink`, coverage/fulfilment computation) — this change populates the value
  store `derive` will later read; it does not compute audit outcomes.
- Bomenboekhouding reconciliation (Phase 4) or any permit-document linkage (Phase 1's `load koop`).
- A live daily-delta ingest path — the API has no documented mutation feed for this source (unlike
  EP-Online's daily files); refresh is a full re-page, same posture as BAG's monthly full reload.

## Decisions

### D1 — Paged JSON via the DSO API, not CSV

The API offers `_format=json|csv|geojson`. JSON is chosen: it keeps one typed parser shared across
both sub-datasets and avoids CSV quoting/type-inference edge cases (the plan text's "paged CSV/API
export" is read as "paged export via the API", not a CSV format mandate). Pagination uses the
documented `_pageSize`/`page=` params; page size is chosen to bound request count for 323,728 rows
without hitting response-size limits — an implementation detail, not a spec-level requirement.

### D2 — Raw landing: versioned, keep-all-versions via the shared `RawStore`

`ingest/bomen` lands each dataset (`kapenherplant`, `stamgegevens`) as **one artifact per run** — a
JSONL file whose lines are the raw page bodies, verbatim — through the shared
`RawStore.LandVersion` helper (added to `ingest/shared` in this change, alongside the existing
overwrite `Land`). Unlike BAG, which overwrites its single ~3.6 GB blob to save space, bomen
**keeps every landed version** (`<name>/<runKey>` + a per-name `provenance.jsonl`): the tree
registry changes over time (felled → replanted) and audit needs the history, not just the latest
snapshot. Each version records provenance (source URL incl. `_pageSize`, fetch timestamp, byte
size, content hash, version). Landing is content-addressed-idempotent — a run whose bytes hash
identically to the latest version is a no-op (no duplicate version) — so a scheduled re-run on
unchanged data costs nothing. `load/bomen` reads the newest version via `RawStore.LatestVersion`.

### D3 — Two independently-reloadable targets; the join is a materialized third pass

`kapenherplant` and `stamgegevens` each load into their own typed PostGIS table via the same
stage→upsert→soft-delete shape (D4), keyed by the source's natural `id` field (no voorkomens here,
so "identity" is just the row id — unlike BAG, there is no valid-time versioning to preserve).
After both targets are upserted, `load/bomen` runs one more pass over `kapenherplant`: look up
`stamgegevens` by `boomId`; on a miss, retry via `boomNieuwId`; store the resolved geometry plus
`resolvedVia` (`boomId` | `boomNieuwId` | `unresolved`) as columns on the `kapenherplant` target
row. This keeps a `stamgegevens`-only refresh (faster, smaller) independent of a full
`kapenherplant` reload, while still materializing the join once so `derive`/the server never
re-run it per query.

### D4 — Non-destructive reload (never lose a referenced `boomId`)

Mirrors P6's `load/geo` D4: stage the full paged export into a `*_staging` table, then upsert into
the target keyed by `id` — new rows insert, unchanged rows no-op, and a target row absent from the
new export is **soft-deleted** (`source_deleted_at = load_ts`), never physically removed. A
`kapenherplant.boomId` may already be referenced by a `derive`d `AuditLink` or by another
`kapenherplant` row's resolved point (D3); dropping it on a routine weekly refresh would break that
reference. `--reset` still drops + rebuilds both targets from the landed data for a clean dev
volume; absent the flag, the reload is the non-destructive upsert.

### D5 — Reproduce the documented API quirks rather than assume a clean feed

`DATA_SOURCES.md` §2a records three load-bearing quirks this change must not silently break on:
the `[isnull]` filter operator returns an empty response with no error, so any null-lifecycle-date
handling is done client-side after fetch, never via a query filter; `kapenherplant` rejects spatial
filters (`geometrie[within]` → HTTP 403), so the loader never attempts to query it spatially — the
`boomId`/`boomNieuwId` join (D3) is the only placement path; and the API's "mandatory free key
coming soon" is accommodated by an optional `X-Api-Key`, injected via `GS_BOMEN_API_KEY` in the `cmd/pipeline`
HTTP getter (empty by default), so enforcement later needs only that env var set, not a code change.

### D7 — Conforms to the P6 geo/bag leading pattern

This change follows the bag/location backbone conventions rather than parallel ones: the shared
`ingest/shared` for landing (`RawStore.LandVersion`) and Postgres (`ConnectPostgres`/`DatabaseURL`);
`load/bomen` mirrors `load/geo`'s file layout and idioms (`Load(ctx, pool, store, Config)`, pure
`mergeSQL` + single-transaction `upsertAll`, `ensureExtensions`/`ensureSchema`/`dropTargets`), with
no per-package DB or config helpers; `cmd/pipeline` registers bomen in the ingest registry and a
symmetric per-source **load registry** (`pipeline ingest|load [source ...]`); env is
`GS_RAW_DATA_PATH` / `GS_DATABASE_URL` / `GS_BOMEN_API_KEY`; docs live in `deploy/compose/README.md`.
The `mergeSQL` MERGE additionally refreshes matched rows whose content changed (`WHEN MATCHED AND
t.raw IS DISTINCT FROM s.raw`) because bomen rows are keyed by a mutable `id`, unlike BAG's immutable
voorkomens.

### D6 — Three testing tiers, aligned to the P5/P6 harness

- **Unit (no DB):** the paging/query builders, the landing/provenance plumbing, the
  `boomId`→`boomNieuwId`→`unresolved` resolution order (`resolvePoint`) as a pure function over
  fixtures, and the `mergeSQL` upsert/soft-delete/refresh SQL builder.
- **Integration (automatic, in CI — no live API call):** a checked-in real-shaped subset of both
  registries — a handful of `stamgegevens` rows (including one reachable only via `boomNieuwId`),
  and `kapenherplant` rows spanning a `boomId` hit, a `boomNieuwId` fallback, and a genuinely
  unresolved case, plus a null-lifecycle-date row — seeded into a fresh `internal/testdb` schema
  (`GS_TEST_DATABASE_URL`), then the real load code runs end to end: stage → upsert → soft-delete →
  join resolution. No new CI service; the existing `go-integration` job picks up the
  `//go:build integration` tests automatically.
- **Full-corpus (manual/opt-in):** the real paged pull against the live API, asserting the exact
  Spike C counts (35,202 `kapenherplant` / 323,728 `stamgegevens`) and the ~98% join-resolution
  rate; not run in default CI (network-dependent, and slow relative to the seeded subset).

## Risks / Trade-offs

- **Full reload cost.** 323,728 + 35,202 rows paged over HTTP is far cheaper than BAG's 3.6 GB
  extract, but still O(hundreds) of requests at a reasonable page size. Mitigated by keeping the
  refresh idempotent per page (a re-fetched page upserts to the same no-op) so a failed run can
  resume rather than restart from page 1 — an implementation detail, not a spec requirement here.
- **The join never reaches 100%.** ~2% of `kapenherplant` rows stay genuinely unresolved even after
  the `boomNieuwId` fallback. Accepted and made a first-class outcome (`resolvedVia = unresolved`),
  matching the project's stance that an unresolved/pending state is a reportable result, not an
  error to hide (`IMPLEMENTATION_PLAN.md` §"Fulfilment").
- **Pagination against a live, non-snapshotted registry.** Rows can shift between pages mid-refresh
  (this API has no snapshot/cursor guarantee). Mitigated by the upsert being idempotent on `id` — a
  duplicate row read twice is a no-op — and by treating a missed row as recoverable at the next
  scheduled refresh rather than something to retry synchronously.
- **`ingest/shared` may not exist yet when this change is implemented** (P6 is a parallel, not a
  prerequisite, change). Mitigated by D2's "reuse if present, else add the same minimal helper" —
  a small, acceptable duplication risk versus blocking P7 on P6's merge order.
- **API key not yet required.** Mitigated by D5's optional config value — no code change needed
  when the key becomes mandatory, only a config update.
