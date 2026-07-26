## 1. Config & CLI scaffolding

- [x] 1.1 Add `GS_NER_CACHE_PATH` to `deploy/compose/.env.example` (default
  `${GS_RAW_DATA_PATH}/ner-cache`) with an explanatory comment.
- [x] 1.2 Restructure the `cmd/pipeline` `dump` subcommand from the current no-op stub into a
  parent command with `export` (`--out <dir>`) and `restore` (`--in <dir>`) subcommands wired to
  the `dump` package.
- [x] 1.3 Resolve store config from the env contract (`GS_FUSEKI_URL`, `GS_DATABASE_URL`,
  `GS_NER_CACHE_PATH`) and pass it into the dump package.

## 2. Bundle & manifest

- [x] 2.1 Define the bundle layout and `manifest.json` schema (format version, created-at passed
  in, tool/git version, per-store descriptor: filename, format, size, checksum).
- [x] 2.2 Implement manifest write on export and manifest read + artifact validation
  (checksum/size) on restore.

## 3. Graph export/restore (Fuseki, N-Quads-star)

- [x] 3.1 Implement graph export over HTTP: fetch the whole dataset (all named graphs) as
  N-Quads-star, gzip to `graph.nq.gz`.
- [x] 3.2 Implement graph restore (additive): `DROP GRAPH` each named graph in the bundle
  (+ `CLEAR DEFAULT` if it has default-graph quads) via the update endpoint, then load the
  N-Quads-star back over HTTP, recreating every bundled named graph and RDF-star annotation while
  leaving non-bundled graphs intact.

## 4. PostGIS export/restore

- [x] 4.1 Implement PostGIS export via `pg_dump -Fc` against `GS_DATABASE_URL` to
  `postgis.dump`, with a fast-fail check that `pg_dump` exists and is version-compatible.
- [x] 4.2 Add the `--skip-bag` export flag: exclude the `bag_*` tables from the dump via
  `pg_dump --exclude-table='public.bag_*'` (BAG lives in `public` with a `bag_` prefix — no
  dedicated schema; keep `gebieden_*`/`cbs_*`) and record the skip in the manifest.
- [x] 4.3 Implement PostGIS restore via `pg_restore --clean --if-exists` (additive — drops and
  recreates only the archived objects, leaving non-bundled tables such as BAG intact), requiring
  the PostGIS extension on the target.

## 5. NER cache export/restore

- [x] 5.1 Implement NER cache capture: tar+gzip the `GS_NER_CACHE_PATH` directory to
  `ner-cache.tar.gz`; an absent directory produces a valid empty archive + empty-cache manifest
  descriptor.
- [x] 5.2 Implement NER cache restore: extract the archive into `GS_NER_CACHE_PATH`.
- [x] 5.3 Document the NER cache on-disk contract (directory of per-key files keyed by doc id +
  model/prompt version) in the `dump` package doc and/or `ingest`/`extract` docs so Phase 2 writes
  into the agreed location.

## 6. Top-level orchestration

- [x] 6.1 Wire `dump export` to snapshot all three stores into one bundle in sequence, with clear
  progress/error reporting.
- [x] 6.2 Wire `dump restore` to rebuild all three stores from a bundle additively (replace only
  bundled objects/graphs/entries, preserve the rest), and validate against the manifest.

## 7. Tests

- [x] 7.1 Unit-test the manifest read/write + artifact validation (checksum mismatch is rejected)
  using testify.
- [x] 7.2 Unit-test config resolution and the `GS_NER_CACHE_PATH` default derivation.
- [x] 7.3 Unit-test NER cache capture/restore including the absent-directory → empty-archive path.
- [x] 7.4 Integration test (build-tagged) using `internal/testdb` isolated targets: export a
  populated graph + PostGIS + NER cache, restore into fresh isolated stores, and assert the
  round-trip reproduces all three — including named graphs, RDF-star confidence annotations,
  PostGIS rows + geometry, and cache files.
- [x] 7.5 Assert the production-name guard aborts the test path if a resolved target resolves to a
  reserved production name.
- [x] 7.6 Integration test for `--skip-bag`: export with `--skip-bag`, restore into a target that
  already holds BAG (or a stand-in reference table) + a named graph not in the bundle, and assert
  the bundled objects are restored exactly while the non-bundled BAG table and graph are preserved
  unchanged.

## 8. Docs & validation

- [x] 8.1 Update `deploy/compose/README.md`/docs where dump/restore usage and the `pg_dump`
  requirement should be recorded.
- [x] 8.2 Run `task lint:go` and `task test:go`; run the integration test per `test:integration`;
  run `openspec validate dump-snapshot-tool --strict` and fix any issues.
