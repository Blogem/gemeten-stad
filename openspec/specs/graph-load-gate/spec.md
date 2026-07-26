# graph-load-gate Specification

## Purpose
TBD - created by archiving change p8-modeling. Update Purpose after archive.
## Requirements
### Requirement: SHACL-gated graph writes

The system SHALL provide a Go load path in a `load/graph/` package (sibling of `load/geo` and
`load/bomen`) that, given a candidate graph, validates it against `ontology/shapes.ttl` via the
Fuseki SHACL endpoint over HTTP (no in-process JVM) and writes the graph to the store ONLY if
the validation report has `sh:conforms true`. The SHACL check SHALL be the load gate: a
non-conforming candidate SHALL NOT be written, and the load path SHALL fail loudly — returning
the validation report / violation detail to the caller so the pipeline exits non-zero — mirroring
the loud sanity gates of `load/geo`. The ontology, vocab, and shapes `.ttl` files SHALL be
embedded in the binary (`go:embed`) so it is self-contained. The runtime Fuseki dataset URL SHALL
be resolved from `GS_FUSEKI_URL` via a fail-loud `shared` env helper (as `shared.DatabaseURL`
does for Postgres).

#### Scenario: Conforming graph is written

- **WHEN** a well-formed candidate graph is submitted to the load path
- **THEN** validation reports `sh:conforms true`
- **AND** the graph is written to the store

#### Scenario: Non-conforming graph is rejected, not written

- **WHEN** a candidate graph missing a required structure or a confidence annotation is
  submitted
- **THEN** validation reports `sh:conforms false`
- **AND** the load path does not write the graph and returns the violation detail

### Requirement: Run-stamped named-graph writes with provenance

A conforming graph SHALL be written into a run-stamped named graph (`run:load-…`), and the load
path SHALL record a `prov:Activity` with `prov:generatedAtTime` for that run in the dedicated
`run:_provenance` named graph. The write path SHALL use the settled namespaces (`gs:`,
`http://gemetenstad.nl/id/`, `http://gemetenstad.nl/run/`) and SHALL reuse the Fuseki HTTP/
auth conventions established in `internal/testdb/fuseki.go`. The package SHALL expose a
`Load(ctx, …) error` orchestrator with a `Config{Reset}` (a reset rebuilds the graph from
scratch; absent it, writes are additive run graphs), matching the `load/geo` shape, callable
from the pipeline `load`/`derive` stages.

#### Scenario: Each run gets its own named graph and provenance triple

- **WHEN** a load run writes a conforming graph
- **THEN** the graph's triples are in a distinct `run:load-…` named graph
- **AND** a matching `prov:Activity` with `prov:generatedAtTime` exists in `run:_provenance`

#### Scenario: Reset rebuilds the graph

- **WHEN** the graph load runs with the reset flag
- **THEN** the run graphs are cleared and rebuilt; absent the flag, a conforming write adds a new
  run-stamped named graph without removing prior runs

### Requirement: Load-gate is verified against an isolated Fuseki

The load path's accept path and both reject paths (missing structure; missing confidence) SHALL
be exercised by integration tests against an isolated Fuseki dataset provisioned by the P5
test-DB harness, never against production dataset names.

#### Scenario: Accept and reject paths run against a live isolated store

- **WHEN** the integration test suite runs
- **THEN** it provisions an isolated Fuseki dataset via the P5 harness (`GS_TEST_FUSEKI_URL`,
  guarded against production names)
- **AND** asserts the well-formed graph is written and each malformed graph is rejected
- **AND** tears the dataset down afterward

