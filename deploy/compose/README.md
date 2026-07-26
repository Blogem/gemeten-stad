# Dev compose — geo backbone load recipe

Operator notes for loading the BAG + `gebieden`/CBS geo backbone (Phase 0 · P6) into the dev
`compose.yaml` stack. See `openspec/changes/geo-backbone-preload/{proposal,design}.md` for the
design; this file is the how-to.

## 1. The load recipe

Two `pipeline` subcommands, run on the host (not through compose — see the env knobs below):

```
pipeline ingest [source ...]  # bronze: land raw sources verbatim + provenance
pipeline load [source ...]    # silver: stage via ogr2ogr/rows, upsert, index, gate
```

- **`pipeline ingest`** downloads the national BAG *LV 2.0 Extract* from the PDOK atom feed, the
  whole-city `gebieden` buurt/wijk polygons (Datapunt GeoJSON), the CBS "wijken en buurten" WFS
  reference, and the Amsterdam `bomen` tree registry (`kapenherplant` + `stamgegevens`, paginated
  Datapunt DSO API), and lands all four verbatim under `GS_RAW_DATA_PATH` with a provenance
  sidecar.
  - `pipeline ingest` with no arguments ingests all registered sources, in registration order
    (`bag`, `gebieden`, `bomen`).
  - `pipeline ingest bag` ingests only the BAG source — schedule this monthly.
  - `pipeline ingest gebieden` ingests only the `gebieden`/CBS boundaries — schedule this weekly.
  - `pipeline ingest bomen` ingests only the tree registry.
  - An unknown source name exits non-zero with an error listing the valid source names, without
    ingesting anything.
  - **Landing behaviour differs by source.** BAG and `gebieden` are idempotent: a source already
    landed is skipped, not re-fetched. `bomen` instead **re-fetches every run and lands each run
    as a new version**, keeping all past versions — the tree registry changes over time (felled,
    replanted) and audit needs the history, not just the latest snapshot.
- **`pipeline load [source ...]`** stages each landed source into PostGIS and upserts it into the
  target tables:
  - `pipeline load` with no arguments loads all registered sources, in registration order (`geo`,
    `bomen`); `pipeline load geo` or `pipeline load bomen` loads just one; an unknown source name
    exits non-zero with an error listing the valid names, without loading anything.
  - **`geo`** stages BAG + `gebieden` via the `gdal` sidecar's `ogr2ogr` (`lvbag` for BAG,
    GeoJSON/WFS for the polygons) into `*_staging` tables, then upserts staging into the target
    tables keyed by voorkomen identity (`identificatie` + `begingeldigheid` +
    `tijdstipregistratie`): new voorkomens insert, unchanged ones no-op, and a target row absent
    from the fresh staging set is **soft-deleted** (`source_deleted_at`), never physically
    dropped. It then builds the resolver's indexes (GIST + `pg_trgm`) and asserts the post-load
    sanity gates (single SRID 28992; 69 Noord buurten / 15 Noord wijken present in the whole-city
    load).
  - **`bomen`** (`pipeline load bomen [--reset]`) reads the most recently landed
    `kapenherplant`/`stamgegevens` export, stages it, upserts it into its target tables the same
    soft-delete way, and materializes the kapenherplant → stamgegevens point resolution. Unlike
    `geo`, it runs directly against Postgres — no `gdal` sidecar involved.
  - **`--reset`** drops and rebuilds the target tables first, for a clean dev volume — applies to
    whichever source(s) are selected.
  - Without `--reset`, a reload is the non-destructive upsert described above — safe to re-run.

## 2. Env knobs (`.env.example`)

| Var | Meaning |
|---|---|
| `GS_RAW_DATA_PATH` | Host path for the raw landing store (bronze) — where `pipeline ingest` writes source artifacts + `.prov.jsonl` provenance. |
| `GS_DATABASE_URL` | The **host** pgx DSN the `pipeline` binary itself connects with, e.g. `postgres://gs:gs@localhost:5433/gemeten_stad` — the host-published port, **not** the in-network `db:5432` the `server` container uses. |
| `GS_GDAL_EXEC_PREFIX` | Command prefix used to shell into the `gdal` sidecar. Default: `docker compose -f deploy/compose/compose.yaml exec -T gdal`. |
| `GS_GDAL_PG_CONN` | The `ogr2ogr` Postgres connection string used **inside** the sidecar: `PG:host=db port=5432 dbname=gemeten_stad user=gs password=gs`. This is deliberately distinct from `GS_DATABASE_URL` — `ogr2ogr` runs inside the `gdal` container, on the compose network, so it addresses Postgres as `db`, not through whatever host/port the Go process's own DSN uses. |
| `GS_BOMEN_API_KEY` | Optional `X-Api-Key` header for the Amsterdam bomen (tree registry) API. No key is required today (docs/DATA_SOURCES.md §2a), but the API docs signal a mandatory key is coming; read only by `pipeline ingest bomen`. |
| `GS_NER_CACHE_PATH` | Directory holding the NER cache (bronze-layer cache of expensive Phase-2 extraction output). Default when unset: `${GS_RAW_DATA_PATH}/ner-cache`. Read by `pipeline dump`; written by `extract` (Phase 2). |
| `GS_FUSEKI_URL` | The **host** Fuseki dataset URL `pipeline dump` reads/writes, e.g. `http://localhost:3030/ds` — the host-published port, same host-vs-in-network distinction as `GS_DATABASE_URL` above. |

## 3. Host ↔ sidecar landing store (`GS_RAW_DATA_PATH`)

The host `pipeline` process lands files under `GS_RAW_DATA_PATH`, and the `gdal` sidecar reads them
at `/data`. `compose.yaml` bind-mounts the two together (`${GS_RAW_DATA_PATH}:/data`), so a fresh
`pipeline ingest` followed by `pipeline load` sees the same landed extract on both sides.

`GS_RAW_DATA_PATH` **must be an absolute host path** and is **required** to bring up the `gdal`
service — `compose config`/`up` fails fast with a clear message if it is unset, rather than mounting
an unexpected directory. (Bringing up only `db`+`fuseki` — as the CI integration job does — does not
start `gdal` and so does not need it set.) Create the directory before the first run:

```
export GS_RAW_DATA_PATH="$HOME/gemeten-stad-rawdata"   # absolute; ~10 GB free for the full extract
mkdir -p "$GS_RAW_DATA_PATH"
```

## 4. Integration gate (automatic, CI)

```
task compose:up
task test:integration    # or: go test -tags=integration ./...
```

Requires `GS_TEST_DATABASE_URL` (+ the P5 Fuseki vars). This SQL-seeds a small, real-*shaped* BAG
subset (a handful of OPR/NUM/VBO/LIG/STA rows spanning voorkomens, incl. a withdrawn `status` and a
valid-at-date vs any-time pair, plus a few `gebieden` polygons) directly into a `*_staging` table in
an isolated `internal/testdb` schema, then runs the real load-from-staging code (upsert →
soft-delete → indexes → gates) and the resolver end-to-end — no `gdal` sidecar involved, so it is
unaffected by the volume gap in §3. This is the tier that runs in default CI.

## 5. Full-corpus gate (manual/opt-in)

Reproduces Spike D's exact numbers against the real ~3.6 GB national extract, on the full compose
stack including `gdal`. Not run in default CI — run by hand when validating a real load:

1. Set `GS_RAW_DATA_PATH` to an absolute host path and `mkdir -p` it (see §3).
2. `docker compose -f deploy/compose/compose.yaml up -d` (full stack, incl. `gdal`).
3. `pipeline ingest bag gebieden` — lands the real BAG extract + `gebieden`/CBS.
4. `pipeline load geo --reset` — runs the actual `ogr2ogr`/`lvbag` load against the real extract.
5. Assert the Spike D sanity numbers: **69 Noord buurten / 15 Noord wijken**, a single SRID
   **28992**, and the **90% address-precision / 100% point-in-polygon** resolution distribution
   (see `docs/DATA_SOURCES.md` §8 for the reference figures and methodology, and `spikes/spike-d/`
   for the original empirical write-up these packages promoted into production).

## 6. Snapshot dump/restore (Phase 0 · P9)

`pipeline dump export`/`pipeline dump restore` snapshot and reproduce the pipeline's three durable
stores — the RDF graph (Fuseki), PostGIS, and the NER cache — as one self-describing bundle. See
`openspec/changes/dump-snapshot-tool/design.md` for the full design; this is the how-to.

```
pipeline dump export --out <bundle-dir> [--skip-bag]
pipeline dump restore --in <bundle-dir>
```

- **`dump export`** writes `<bundle-dir>/manifest.json` plus one artifact per store:
  `graph.nq.gz` (gzipped N-Quads-star, every named graph + the default graph), `postgis.dump` (a
  `pg_dump -Fc` custom-format archive), and `ner-cache.tar.gz` (a tar+gzip of `GS_NER_CACHE_PATH`,
  or a valid empty archive if the cache directory does not exist yet). Export never mutates a
  source store.
  - **`--skip-bag`** excludes the large `bag_*` tables from the PostGIS artifact — a
    space-constrained snapshot that still restores cleanly, since BAG is a cheap idempotent
    re-fetch (`pipeline ingest bag` + `pipeline load geo --reset`) rather than bundle content.
- **`dump restore`** rebuilds all three stores from a bundle, **additively**: each object, named
  graph, or cache entry the bundle carries is dropped/cleared and reloaded so it ends up exactly as
  the bundle holds it, while content in the target *outside* the bundle's scope (e.g. BAG, after a
  `--skip-bag` export) is left untouched. Restore is not a wholesale volume wipe.
- **Requires `pg_dump`/`pg_restore` on the host running the tool**, matching the target's Postgres
  major version (the dev-compose `db` image is PG18; `brew install libpq` on macOS gets a matching
  client without the full server). Export/restore fail fast with a clear error if the tools are
  missing or version-mismatched.
- Store connections come from the same env contract as the rest of the pipeline: `GS_FUSEKI_URL`,
  `GS_DATABASE_URL` (both the **host**-published endpoints — see §2), and `GS_NER_CACHE_PATH`.
- Run dumps when the pipeline is idle — the snapshot is not a single distributed transaction across
  the three stores.
- Covered by the integration suite (`task test:integration`): a full export → restore round-trip
  into fresh `internal/testdb`-isolated targets (a whole isolated Postgres **database**, since
  `pg_dump`/`pg_restore` operate at that granularity, not just a schema), asserting named graphs,
  RDF-star confidence annotations, PostGIS rows + geometry, and NER cache files all survive; and a
  `--skip-bag` round-trip asserting additive restore leaves non-bundled content untouched.
