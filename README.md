# Gemeten Stad

De Gemeten Stad audits Amsterdam government interventions — starting with the tree
felling/replanting obligation — against public observation data, using a grounded,
uncertainty-aware agent. This repository holds vertical 1: the tree lifecycle in
stadsdeel Noord.

## Read the design docs first

- [`docs/VISION.md`](docs/VISION.md) — the overarching idea (auditing the city).
- [`docs/IMPLEMENTATION_PLAN.md`](docs/IMPLEMENTATION_PLAN.md) — vertical 1 architecture and phases.
- [`docs/DATA_SOURCES.md`](docs/DATA_SOURCES.md) — source catalog, working queries, data quirks.
- [`docs/DATA_THREAD_TREES.md`](docs/DATA_THREAD_TREES.md) — a live, worked end-to-end example.
- [`docs/PHASE_0_PLAN.md`](docs/PHASE_0_PLAN.md) — Phase 0 work items (foundations & modeling).
- [`docs/PHASE_1_PLAN.md`](docs/PHASE_1_PLAN.md) — Phase 1 work items (the deterministic backbone).
- [`docs/PHASE_2_PLAN.md`](docs/PHASE_2_PLAN.md) — Phase 2 work items (extraction, fulfilment, campaign matching).

## Layout

The pipeline follows a `raw → conformed → derived` (bronze/silver/gold) shape; packages are
organised `stage × source` with binaries under `cmd/` (see `IMPLEMENTATION_PLAN.md` §5).

```
cmd/
  pipeline/     # one binary; subcommands: ingest / extract / load / derive / dump
  server/       # the API + SPA host (own lifecycle)
ingest/         # BRONZE: raw data in, per source (koop, bomen, bag, gebieden, shared)
extract/        # OPTIONAL: unstructured only (permits); output cached durably
load/           # SILVER: map + resolve + assemble (koop, bomen, graph, geo)
location/       # preloaded BAG + gebieden + CBS; smallest-area resolver
derive/         # GOLD: AuditLink + computed progress (stored)
dump/           # export/snapshot: graph + PostGIS + the NER cache
ontology/       # .ttl: ontology, SKOS vocab, SHACL shapes
server/         # (logic) layered controller → service → repository
webapp/         # SvelteKit SPA (map + chat)
deploy/         # compose (dev) + k3s (prod)
```

## Build

Uses [Task](https://taskfile.dev):

```sh
task build    # build both Go binaries + the webapp
task test     # go test ./...
task lint     # golangci-lint run
```

The pipeline subcommands are currently no-ops; behaviour lands in later Phase-0/1 work items.

```sh
go run ./cmd/pipeline --help
```

## Dev environment

A `docker compose` stack under `deploy/compose/` brings up the two stores plus the server:

```sh
cp deploy/compose/.env.example deploy/compose/.env   # optional — defaults work as-is
task compose:up                                       # PostGIS + Fuseki + server + gdal sidecar
```

| Service | URL / port | Notes |
|---------|-----------|-------|
| PostGIS | `localhost:5433` | db `gemeten_stad`, user/pass `gs`/`gs` (value + geometry store) |
| Fuseki  | <http://localhost:3030> | RDF graph; default dataset `/ds`, SHACL enabled |
| server  | <http://localhost:8080/healthz> | returns `ok` |

Data lives on named volumes and survives `task compose:down`. To also drop the volumes:
`task compose:down -- -v`. The `gdal` sidecar is idle until the P6 BAG/geo load drives it.

## Running integration tests

Integration tests (`-tags=integration`) never touch the dev stack's own database/dataset — they
create an isolated, randomly-named Postgres schema and Fuseki dataset via `internal/testdb`, and a
guard aborts if a resolved name ever matches a production/dev name (`gemeten_stad`, `public`, `ds`).

```sh
task compose:up
GS_TEST_DATABASE_URL=postgres://gs:gs@localhost:5433/gemeten_stad \
GS_TEST_FUSEKI_URL=http://localhost:3030 \
FUSEKI_ADMIN_PASSWORD=admin \
  task test:integration
```
