# Dev compose — geo backbone load recipe

Operator notes for loading the BAG + `gebieden`/CBS geo backbone (Phase 0 · P6) into the dev
`compose.yaml` stack. See `openspec/changes/geo-backbone-preload/{proposal,design}.md` for the
design; this file is the how-to.

## 1. The load recipe

Two `pipeline` subcommands, run on the host (not through compose — see the env knobs below):

```
pipeline ingest [source ...]  # bronze: land raw sources verbatim + provenance
pipeline load [--reset]       # silver: stage via ogr2ogr, upsert, index, gate
```

- **`pipeline ingest`** downloads the national BAG *LV 2.0 Extract* from the PDOK atom feed, the
  whole-city `gebieden` buurt/wijk polygons (Datapunt GeoJSON), and the CBS "wijken en buurten" WFS
  reference, and lands all three verbatim under `GS_RAW_DATA_PATH` with a provenance sidecar.
  Idempotent: a source already landed is skipped, not re-fetched.
  - `pipeline ingest` with no arguments ingests all registered sources, in registration order.
  - `pipeline ingest bag` ingests only the BAG source — schedule this monthly.
  - `pipeline ingest gebieden` ingests only the `gebieden`/CBS boundaries — schedule this weekly.
  - An unknown source name exits non-zero with an error listing the valid source names, without
    ingesting anything.
- **`pipeline load`** stages each source into PostGIS via the `gdal` sidecar's `ogr2ogr`
  (`lvbag` for BAG, GeoJSON/WFS for the polygons) into `*_staging` tables, then upserts staging into
  the target tables keyed by voorkomen identity (`identificatie` + `begingeldigheid` +
  `tijdstipregistratie`): new voorkomens insert, unchanged ones no-op, and a target row absent from
  the fresh staging set is **soft-deleted** (`source_deleted_at`), never physically dropped. It then
  builds the resolver's indexes (GIST + `pg_trgm`) and asserts the post-load sanity gates (single
  SRID 28992; 69 Noord buurten / 15 Noord wijken present in the whole-city load).
  - **`--reset`** drops and rebuilds the target tables first, for a clean dev volume.
  - Without `--reset`, a reload is the non-destructive upsert described above — safe to re-run.

## 2. Env knobs (`.env.example`)

| Var | Meaning |
|---|---|
| `GS_RAW_DATA_PATH` | Host path for the raw landing store (bronze) — where `pipeline ingest` writes source artifacts + `.prov.jsonl` provenance. |
| `GS_DATABASE_URL` | The **host** pgx DSN the `pipeline` binary itself connects with, e.g. `postgres://gs:gs@localhost:5433/gemeten_stad` — the host-published port, **not** the in-network `db:5432` the `server` container uses. |
| `GS_GDAL_EXEC_PREFIX` | Command prefix used to shell into the `gdal` sidecar. Default: `docker compose -f deploy/compose/compose.yaml exec -T gdal`. |
| `GS_GDAL_PG_CONN` | The `ogr2ogr` Postgres connection string used **inside** the sidecar: `PG:host=db port=5432 dbname=gemeten_stad user=gs password=gs`. This is deliberately distinct from `GS_DATABASE_URL` — `ogr2ogr` runs inside the `gdal` container, on the compose network, so it addresses Postgres as `db`, not through whatever host/port the Go process's own DSN uses. |

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
3. `pipeline ingest` — lands the real BAG extract + `gebieden`/CBS.
4. `pipeline load --reset` — runs the actual `ogr2ogr`/`lvbag` load against the real extract.
5. Assert the Spike D sanity numbers: **69 Noord buurten / 15 Noord wijken**, a single SRID
   **28992**, and the **90% address-precision / 100% point-in-polygon** resolution distribution
   (see `docs/DATA_SOURCES.md` §8 for the reference figures and methodology — the throwaway spike
   that established them has since been retired in favor of these packages).
