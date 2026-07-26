## 1. Shared plumbing (`ingest/shared`, config)

- [x] 1.1 If `ingest/shared`'s raw-landing/provenance helper already exists (landed via the parallel P6 change), reuse it as-is. Otherwise, add the same minimal helper: land a source artifact under the `raw-data` volume path and write a history-preserving provenance sidecar (source URL, fetch timestamp, byte/row count, content hash); a refresh lands a new dated snapshot, never erasing prior provenance; expose an "already landed?" check for idempotency.
- [x] 1.2 Add (or reuse) the PostGIS connection helper over `github.com/jackc/pgx/v5`, reading `GS_DATABASE_URL` at runtime.
- [x] 1.3 Add an optional API key config value for the Datapunt `bomen` API (`X-Api-Key`, empty by default) so the loader tolerates the key becoming mandatory later without a code change.

## 2. Bomen ingest (`ingest/bomen`)

- [x] 2.1 Implement paged JSON fetch of `kapenherplant` from `api.data.amsterdam.nl/v1/bomen/kapenherplant/` (`_pageSize`/`page=`), landing each page verbatim into the raw store; no `[isnull]` query filter and no `geometrie[within]` spatial query against this endpoint.
- [x] 2.2 Implement paged JSON fetch of `stamgegevens` the same way.
- [x] 2.3 Write one provenance record per ingest run per dataset (source URL template incl. page size, fetch timestamp, total row count, content hash over the concatenated pages).
- [x] 2.4 Implement refresh as an idempotent full re-page (land a new dated snapshot); skip re-fetching an already-landed, unchanged snapshot.

## 3. Bomen load into PostGIS (`load/bomen`)

- [x] 3.1 Define the typed `kapenherplant` and `stamgegevens` target tables (all source columns retained) plus their `*_staging` counterparts, and a `source_deleted_at` column on each target.
- [x] 3.2 Stage each landed export into its `*_staging` table.
- [x] 3.3 Upsert each staging table into its target keyed by the source's natural `id`: insert new rows, no-op unchanged rows, and soft-delete rows absent from staging (`source_deleted_at = load_ts`) — never physically delete.
- [x] 3.4 Implement the `kapenherplant` → `stamgegevens` point resolution pass: look up by `boomId`, fall back to `boomNieuwId` on a miss, and store `resolvedGeom`/`resolvedVia` (`boomId` | `boomNieuwId` | `unresolved`) on each `kapenherplant` row. Never fail or skip a row on `unresolved`.
- [x] 3.5 Add a reset flag that drops + rebuilds both targets from the landed data (clean dev volume); absent the flag, the reload is the non-destructive upsert.
- [x] 3.6 Log whole-city row counts after load (target: 35,202 `kapenherplant`, 323,728 `stamgegevens`).

## 4. Pipeline wiring (`cmd/pipeline`)

- [x] 4.1 Wire the bomen ingest work items into `pipeline ingest`, and the bomen load (with the reset flag) into `pipeline load`, replacing the no-op stubs for these sources.

## 5. Tests

All Go tests use **testify** (`require`/`assert`), table-driven where it fits. Integration tests
use the P5 conventions: `//go:build integration`, `internal/testdb` random-schema isolation +
reserved-name guard, `GS_TEST_DATABASE_URL`; they run under the existing `task test:integration` /
`go-integration` CI job (no `ci.yml` change needed).

**Unit (no DB):**

- [x] 5.1 Paging/query builders for `kapenherplant` and `stamgegevens` (correct `_pageSize`/`page=` params; no `[isnull]`/`geometrie[within]` usage).
- [x] 5.2 Landing/provenance plumbing: lands + writes provenance on first run; reports "already landed" on second; a refresh preserves prior provenance history.
- [x] 5.3 The `boomId` → `boomNieuwId` → `unresolved` resolution order and the upsert/soft-delete diffing logic, as pure functions over fixtures.

**Integration (automatic, in CI — no live API call):**

- [x] 5.4 Add a checked-in real-shaped subset of both registries (a handful of `stamgegevens` rows, one reachable only via `boomNieuwId`; `kapenherplant` rows spanning a `boomId` hit, a `boomNieuwId` fallback, a genuinely unresolved case, and a null-lifecycle-date row) as SQL/`COPY` seed data; load it into `*_staging` tables in a fresh `internal/testdb` schema.
- [x] 5.5 Exercise the real load code end to end on the subset: upsert (new-row insert, re-run is a no-op, an absent row is soft-deleted with `source_deleted_at`) → join resolution (all three `resolvedVia` outcomes) → row-count assertions.

**Full-corpus (manual/opt-in — real API pull):**

- [x] 5.6 Document the opt-in full-corpus gate that runs the real paged pull against the live API and asserts the exact counts (35,202 `kapenherplant` / 323,728 `stamgegevens`) and the ~98% join-resolution rate; not run in default CI.

## 6. Docs & cleanup

- [x] 6.1 Add a `load/bomen`-or-`deploy` README note documenting the load recipe, the API key config knob, and how to run the integration gate.
- [x] 6.2 Mark P7 done in `docs/PHASE_0_PLAN.md`.
