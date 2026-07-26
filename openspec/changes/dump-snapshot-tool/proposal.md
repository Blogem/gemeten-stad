## Why

The pipeline's expensive, hard-to-reproduce state lives in three places — the RDF graph
(Fuseki), the value/geometry store (PostGIS), and the durably-cached NER output — and today
nothing captures them together. Phase-2 NER extraction in particular is costly (LLM
count-binding) and must never be lost to a wiped dev volume or a fresh environment. P9 gives
the `pipeline dump` subcommand a real implementation: a single snapshot of all three stores and
a restore that reproduces them on a clean volume, so environments are reproducible and expensive
work is recoverable.

## What Changes

- Implement `pipeline dump export` — snapshot the graph, PostGIS, and the NER cache into one
  self-describing bundle (with a manifest), replacing the current no-op `dump` stub.
- Implement `pipeline dump restore` — rebuild all three stores from a bundle onto a clean
  volume, clearing existing content first so restore is deterministic.
- **Define the NER cache's on-disk contract now** (it is only *populated* in Phase 2): a
  directory of cache-entry files under the raw landing store, keyed by document id +
  model/prompt version. Dump treats an absent/empty cache as a valid empty snapshot, so P9 can
  land before Phase 2 exists.
- Graph export/restore over HTTP in N-Quads-star (preserving PROV named graphs and RDF-star
  confidence annotations); PostGIS via `pg_dump`/`pg_restore`; NER cache as a captured directory
  archive.
- Add `GS_NER_CACHE_PATH` to the dev-compose env contract (default under the raw landing store).

## Capabilities

### New Capabilities

- `data-snapshot`: Export and restore a consistent, self-describing snapshot of the pipeline's
  three durable stores (RDF graph, PostGIS value/geometry store, NER cache), including a manifest
  and a dump→restore round-trip that reproduces both stores on a clean volume.

### Modified Capabilities

<!-- None — no existing specs define store behaviour that changes at the requirement level. -->

## Impact

- **Code:** `dump/` (currently a `doc.go` stub) gains the export/restore implementation;
  `cmd/pipeline/main.go` `dump` subcommand is restructured into `dump export` / `dump restore`.
- **Config:** new `GS_NER_CACHE_PATH` env var; `deploy/compose/.env.example` documents it.
- **External tools:** requires `pg_dump`/`pg_restore` (PG18-compatible) on the machine running
  the tool; graph and NER-cache paths use HTTP + the filesystem only (no extra binaries).
- **Stores:** reads from and (on restore) destructively rewrites Fuseki + PostGIS + the NER
  cache directory. Tests must run against the isolated integration targets (`internal/testdb`),
  never production names.
- **Downstream:** establishes the NER cache location contract that Phase-2 `extract` will write
  into.
