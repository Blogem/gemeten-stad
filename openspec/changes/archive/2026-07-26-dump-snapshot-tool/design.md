## Context

Gemeten Stad keeps its durable pipeline state in three stores (`IMPLEMENTATION_PLAN.md` §5):
the **RDF graph** in Apache Jena Fuseki (Spike E — RDF-star confidence + PROV run-stamped named
graphs), the **PostGIS** value/geometry store (BAG, `kapenherplant`/`stamgegevens`, gebieden/CBS
polygons), and the **NER cache** — the bronze-layer cache of expensive extraction output. The
dev stack (`deploy/compose/compose.yaml`) runs these as containers with data on named volumes;
the `dump` package is currently a `doc.go` stub and `cmd/pipeline` wires `dump` as a no-op.

P9's job (`PHASE_0_PLAN.md`): make `pipeline dump` snapshot all three stores and restore them so
a dump→restore round-trip reproduces both stores on a clean volume. The NER cache is only
_populated_ in Phase 2, but its on-disk shape and the tool must be defined now. Constraints from
the project rules: build lean (no speculative knobs), no backwards-compat, and tests must run
against the isolated integration targets (`internal/testdb`) with the production-name guard.

## Goals / Non-Goals

**Goals:**

- One command produces one self-describing snapshot bundle of graph + PostGIS + NER cache.
- Restore rebuilds all three stores on a clean volume, deterministically (clear-then-load).
- Graph snapshot preserves **named graphs** (PROV) and **RDF-star** annotations (confidence).
- Establish the NER cache on-disk contract (keyed by doc id + model/prompt version) so Phase-2
  `extract` writes into an agreed location and dump already captures it.
- Round-trippable and verifiable in integration tests against isolated stores.

**Non-Goals:**

- No incremental/differential snapshots, no scheduling, no remote/object-store upload — a local
  bundle only (add later if a real need appears).
- No cross-version schema migration of snapshot contents; a bundle restores into the same
  store versions that produced it (POC, single environment).
- No selective/partial dump knobs beyond `--skip-bag` (D3): no "graph only", no arbitrary
  table excludes — the lean default is the whole of each store, with BAG the one earned exception
  (it is large and a cheap idempotent re-fetch).
- Not a backup rotation/retention system; producing and consuming a bundle is the whole scope.
- The **raw landing store is not snapshotted** — only the `ner-cache` subdirectory under it. The
  source downloads and `.prov.jsonl` provenance sidecars in the landing store
  (`ingest/shared/landing.go`) are reproducible by re-running `ingest`, so they stay out of the
  bundle (`IMPLEMENTATION_PLAN.md` §5 lists dump as graph + PostGIS + NER cache only).

## Decisions

### D1 — Bundle shape: a directory with a manifest + one artifact per store

A snapshot is a directory (the "bundle") containing:

- `manifest.json` — snapshot metadata: format version, created-at (passed in, not read from a
  clock inside pure code), tool/git version, and per-store descriptors (artifact filename,
  format, size, and a content checksum). Restore reads it to locate + validate artifacts.
- `graph.nq.gz` — the RDF graph as gzipped **N-Quads-star**.
- `postgis.dump` — the PostGIS database as a `pg_dump` custom-format archive.
- `ner-cache.tar.gz` — the NER cache directory, tarred + gzipped (empty tar if the cache is
  absent).

Rationale: three heterogeneous stores don't share one dump format; a directory + manifest keeps
each artifact in its native, individually-restorable form and makes the bundle self-describing
and checkable. _Alternative considered:_ a single opaque tar of everything — rejected because it
hides structure, prevents per-artifact validation, and forces full extraction to inspect.

### D2 — Graph: export/restore over HTTP as N-Quads-star

Talk to Fuseki over HTTP (the same seam `load` uses), not the TDB2 files on the volume. Use
Jena's **whole-dataset quads operation** — the exact route resolved for this design:

- **Export:** `GET {GS_FUSEKI_URL}` (the dataset URL, e.g. `.../ds`) with header
  `Accept: application/n-quads`. Jena serves the *entire* dataset (default graph + every named
  graph) as N-Quads; gzip the response to `graph.nq.gz`. This is the same operation Jena's
  `RDFConnection.fetchDataset()` performs — a dataset-level GET, not per-graph GSP. The format
  MUST be N-Quads(-star) so PROV named graphs _and_ RDF-star confidence annotations survive — a
  triple format (e.g. `CONSTRUCT` to Turtle) loses graph names and is rejected. Jena's N-Quads
  writer expands `{| … |}` confidence annotations into their quoted-triple quads, so nothing is
  lost on the wire.
- **Restore:** clear the graphs the bundle carries via the update endpoint
  (`POST {GS_FUSEKI_URL}/update`, `DROP GRAPH <g>` per named graph in the bundle + `CLEAR DEFAULT`
  if the bundle has default-graph quads — additive, see D6), then load the N-Quads back with
  `POST {GS_FUSEKI_URL}` and `Content-Type: application/n-quads` (Jena's dataset-level POST adds
  quads into their named graphs — the operation behind `RDFConnection.loadDataset()`).

Rationale: HTTP is store-location-agnostic (works from host or in-cluster, dev or CI) and avoids
coupling dump to the compose volume layout or a `compose exec` into the container. The Go tool
issues these HTTP calls directly (no Jena client needed). Jena is the reference implementation
for RDF-star/N-Quads-star, so fidelity is a non-issue. _Alternatives:_ `tdb2.tdbdump` on the
volume (couples to compose, needs container exec); Fuseki admin `/$/backup` (writes server-side
into the container, awkward to retrieve).

### D3 — PostGIS: `pg_dump`/`pg_restore` custom-format archive

Shell out to `pg_dump -Fc` for export and `pg_restore` for restore, against `GS_DATABASE_URL`.

- Custom format is compressed and lets restore recreate individual objects.
- **`--skip-bag` (export):** when set, exclude the BAG tables from the dump. P6 lands BAG **in
  `public` as prefixed `bag_*` tables — there is no dedicated `bag` schema** (`load/geo/schema.go`),
  so this is `pg_dump --exclude-table='public.bag_*'`, not `--exclude-schema`. Only the ~3.6 GB
  `bag_*` tables are skipped; the small `gebieden_*` / `cbs_*` geo-reference tables (same load,
  same schema) stay in. BAG is a cheap idempotent re-fetch (`IMPLEMENTATION_PLAN.md` §5), so on a
  space-constrained machine a snapshot without it is the useful default choice. The manifest
  records whether BAG was skipped.
- **Restore is additive (D6):** `pg_restore --clean --if-exists` drops-and-recreates only the
  objects *present in the archive*, so a BAG-less bundle restores the audit tables without
  touching an already-loaded BAG. The PostGIS extension must be present on the target (the
  dev-compose image provides it).

Rationale: `pg_dump` is the canonical, geometry-aware way to snapshot PostGIS and needs no
bespoke serialization. _Alternative:_ per-table `COPY` over pgx — reinvents `pg_dump`, must
hand-handle PostGIS types, extensions, and ordering. The trade-off is an external-binary
dependency (see R1).

### D4 — NER cache: a directory under the raw landing store, captured as a tar

Define the contract now (Phase 2 fills it):

- Location: `GS_NER_CACHE_PATH`, defaulting to `${GS_RAW_DATA_PATH}/ner-cache` (the raw landing
  store is already a bind-mounted host path in compose; `GS_RAW_DATA_PATH` is required with no
  code default — `ingest/shared/landing.go` fails loud if unset — so dump resolves the cache path
  the same way). Dump captures only this `ner-cache` subdirectory, not the rest of the landing
  store (see Non-Goals).
- Layout: one cache-entry file per key, where the key is **document id + model/prompt version**
  (`IMPLEMENTATION_PLAN.md` §5) — e.g. a content-addressed path so the same doc under a new
  model/prompt version is a distinct entry and re-runs never re-invoke the LLM.
- Dump captures the directory verbatim as `ner-cache.tar.gz`; restore extracts it. An absent
  directory yields a valid **empty** archive and an empty-cache descriptor in the manifest — so
  P9 lands and round-trips cleanly before any NER entries exist.

Rationale: the plan lists the NER cache as a _separate_ thing from PostGIS, so it is a
filesystem artifact in the bronze landing store, not a Postgres table — a plain directory of
files is the leanest durable, snapshot-able form and matches "cached at the landing layer".

### D5 — CLI shape: `dump export` / `dump restore`

Restructure the single no-op `dump` command into a parent with two subcommands:
`pipeline dump export --out <bundle-dir>` and `pipeline dump restore --in <bundle-dir>`. Store
connection details come from the existing env contract (`GS_FUSEKI_URL`, `GS_DATABASE_URL`, and
the new `GS_NER_CACHE_PATH`), consistent with the rest of the pipeline.

Rationale: export and restore are distinct verbs on one bundle; two subcommands read clearer than
a flag toggling destructive behaviour. _Alternative:_ a top-level `pipeline restore` — rejected
to keep snapshot concerns under one `dump` namespace.

### D6 — Restore is additive (bundle-scoped), not a volume wipe

Restore replaces exactly the objects the bundle carries and leaves everything else in the target
untouched — it does **not** wipe the target stores. This is what makes `--skip-bag` usable: dump
without BAG, restore into a store that already has BAG, and BAG survives.

- **PostGIS:** `pg_restore --clean --if-exists` — only tables in the archive are dropped and
  recreated; other tables (skipped BAG) are left as-is.
- **Graph:** DROP each named graph present in the bundle (+ `CLEAR DEFAULT` if it has
  default-graph quads), then load — graphs not in the bundle are preserved.
- **NER cache:** extract the archive over `GS_NER_CACHE_PATH` — same-key entries are overwritten,
  other entries kept (naturally additive).

Each object/graph present in the bundle ends up *exactly* as the bundle holds it (drop-then-load,
not merge-rows), so restore is still deterministic per object; only content outside the bundle's
scope is preserved. On a clean volume this equals a full restore (the round-trip requirement).
_Alternative:_ a full `DROP DATABASE`/`CLEAR ALL` wipe — rejected: it destroys deliberately
un-dumped data (the whole point of `--skip-bag`) and forces a costly BAG re-load after every
restore.

## Risks / Trade-offs

- **[External binary dependency]** `pg_dump`/`pg_restore` must be present and PG18-compatible on
  the machine running the tool → document the requirement; the dev-compose `db` image already
  ships matching client tools, and the tool fails fast with a clear message if they're missing or
  version-mismatched.
- **[Snapshot size — BAG is ~3.6 GB]** a full PostGIS dump is large and space-constrained
  machines can't hold it → `--skip-bag` (D3) omits BAG from the dump, and additive restore (D6)
  leaves an already-loaded BAG in place, so the common snapshot is just the audit tables + graph +
  NER cache. BAG is re-obtained by re-running its idempotent load, not from the bundle.
- **[Consistency across three stores]** the snapshot is not a single global transaction across
  Fuseki + PostGIS + filesystem → run dumps when the pipeline is idle (the batch model already
  implies no concurrent writers); document this rather than build distributed-snapshot
  machinery.
- **[RDF-star fidelity]** a wrong serialization would silently drop confidence annotations or
  graph names → mandate N-Quads-star and assert graph/quad/annotation survival in the round-trip
  integration test.
- **[Restore overwrites bundled objects]** restore drop-then-loads each object/graph the bundle
  carries (additive to the rest, D6), so it still overwrites live data for those objects → guard
  tests behind `internal/testdb` isolated targets with the production-name guard; restore is
  documented as dev-only for this phase.

## Migration Plan

Additive: `dump` is a no-op today, so implementing it breaks nothing. Steps: (1) add
`GS_NER_CACHE_PATH` to `.env.example` with its default; (2) implement `dump/` export+restore;
(3) restructure the `cmd/pipeline` `dump` subcommand into `export`/`restore`. No data migration.
Rollback is deletion of the new code — no persisted format is depended on by other stages yet.

## Open Questions

- _(Resolved — see D2)_ Fuseki's whole-dataset N-Quads route: `GET`/`POST` on the dataset URL
  with `application/n-quads` (Jena's dataset-level GSP, behind `RDFConnection.fetchDataset()`/
  `loadDataset()`). Implementation confirms the exact status codes against Fuseki 5.5.0.
- Precise NER cache key encoding (hash vs. `docid__modelversion` path) — Phase 2 finalizes it;
  P9 only needs "a directory of per-key files under `GS_NER_CACHE_PATH`" to capture and restore.
