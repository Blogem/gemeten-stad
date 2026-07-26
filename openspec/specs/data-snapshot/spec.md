# data-snapshot Specification

## Purpose
TBD - created by syncing change dump-snapshot-tool. Update Purpose after archive.

## Requirements

### Requirement: Snapshot export

The `pipeline dump export` command SHALL write a single self-describing snapshot bundle
capturing the RDF graph, the PostGIS value/geometry store, and the NER cache. The bundle SHALL
contain a manifest plus one artifact per store, and export SHALL NOT mutate any source store.

#### Scenario: Export writes a complete bundle

- **WHEN** `pipeline dump export --out <dir>` runs against populated stores
- **THEN** the bundle directory contains a `manifest.json`, a gzipped N-Quads-star graph
  artifact, a `pg_dump` PostGIS artifact, and a gzipped NER-cache archive
- **AND** the source graph, PostGIS, and NER cache are left unchanged

#### Scenario: Manifest describes and checksums every artifact

- **WHEN** an export completes
- **THEN** `manifest.json` records the format version and, for each store, the artifact
  filename, format, size, and a content checksum

#### Scenario: Export fails fast when a required tool is missing

- **WHEN** `pg_dump` is unavailable or version-incompatible with the target database
- **THEN** export aborts with a clear error naming the missing or mismatched tool
- **AND** no partial bundle is left presented as complete

### Requirement: Skip BAG on export for space-constrained snapshots

Export SHALL accept a `--skip-bag` option that excludes the large BAG reference tables from the
PostGIS artifact. The manifest SHALL record whether BAG was skipped. Combined with additive
restore, a BAG-less bundle SHALL restore without disturbing BAG data already present in the target.

#### Scenario: Export without BAG omits the BAG tables

- **WHEN** `pipeline dump export --skip-bag --out <dir>` runs
- **THEN** the PostGIS artifact contains the audit tables but not the BAG tables
- **AND** the manifest records that BAG was skipped

#### Scenario: Restoring a BAG-less bundle keeps existing BAG

- **WHEN** a `--skip-bag` bundle is restored into a target that already holds BAG data
- **THEN** the audit tables are restored from the bundle
- **AND** the pre-existing BAG data remains present and unchanged

### Requirement: Graph fidelity across named graphs and RDF-star

The snapshot SHALL preserve the graph losslessly, including all named graphs (PROV run-stamped
graphs) and RDF-star statement annotations (confidence). The graph artifact SHALL be serialized
in a quad format that carries both.

#### Scenario: Named graphs survive a round-trip

- **WHEN** a graph with data in multiple named graphs is exported and then restored
- **THEN** every named graph and its triples are present in the restored dataset, unchanged

#### Scenario: RDF-star confidence annotations survive a round-trip

- **WHEN** a graph containing RDF-star annotated edges (e.g. a location edge with a confidence)
  is exported and then restored
- **THEN** the restored graph retains the annotated statements with their confidence values

### Requirement: NER cache capture with a defined on-disk contract

The NER cache SHALL be a directory of per-key entry files under `GS_NER_CACHE_PATH` (defaulting
to `${GS_RAW_DATA_PATH}/ner-cache`), keyed by document id plus model/prompt version. Export SHALL
capture this directory as an archive artifact in the bundle; an absent cache SHALL be captured as
a valid empty archive, not an error.

#### Scenario: Populated cache is captured and restored

- **WHEN** the NER cache directory contains entry files and the bundle is exported then restored
- **THEN** the restored `GS_NER_CACHE_PATH` directory contains the same entry files with the same
  contents

#### Scenario: Absent cache yields a valid empty snapshot

- **WHEN** `GS_NER_CACHE_PATH` does not exist at export time
- **THEN** export succeeds, writes an empty NER-cache archive, and records an empty-cache
  descriptor in the manifest

### Requirement: Additive snapshot restore

The `pipeline dump restore` command SHALL rebuild the graph, PostGIS, and NER cache from a bundle.
Restore SHALL be additive: each object, named graph, or cache entry present in the bundle SHALL be
replaced so it exactly matches the bundle, while content in the target that is not covered by the
bundle SHALL be left untouched. Restore SHALL NOT wipe the target stores wholesale.

#### Scenario: Restore reproduces both stores on a clean volume

- **WHEN** `pipeline dump restore --in <dir>` runs against empty target stores
- **THEN** the graph, PostGIS database, and NER cache match the state captured in the bundle

#### Scenario: Restore replaces bundled objects exactly

- **WHEN** restore runs against a target where a bundled table/graph/cache entry already holds
  different content
- **THEN** that object ends up exactly as the bundle holds it (drop-then-load, not row merge)

#### Scenario: Restore preserves content outside the bundle's scope

- **WHEN** restore runs against a target that also holds tables, named graphs, or cache entries
  the bundle does not contain
- **THEN** that non-bundled content remains present and unchanged after restore

### Requirement: Dump→restore round-trip fidelity

A dump followed by a restore into fresh stores SHALL reproduce the graph, PostGIS data, and NER
cache equivalently to the originals.

#### Scenario: Full round-trip preserves all three stores

- **WHEN** populated graph + PostGIS + NER cache are exported, and the bundle is restored into
  fresh isolated stores
- **THEN** the restored graph (including named graphs and RDF-star annotations), the restored
  PostGIS data and geometry, and the restored NER cache are equivalent to the originals

### Requirement: Test isolation and production-name guard

Snapshot tests SHALL run only against the isolated integration targets provided by
`internal/testdb`, and SHALL NOT operate against the production database, schema, or Fuseki
dataset names.

#### Scenario: Round-trip test uses isolated targets

- **WHEN** the dump/restore integration test runs
- **THEN** it exports from and restores into `internal/testdb`-provisioned isolated stores
- **AND** the production-name guard aborts if a resolved target name is a reserved production name
