## MODIFIED Requirements

### Requirement: Run-stamped named-graph writes with provenance

A conforming graph SHALL be written into a run-stamped named graph (`run:load-…`), and the load
path SHALL record a `prov:Activity` with `prov:generatedAtTime` for that run in the dedicated
`run:_provenance` named graph. Writes SHALL be **idempotent by stable subject IRI**, not additive:
a run SHALL write only the entities that are genuinely new or changed relative to what the graph
already holds (per the "Idempotent SCD2 upsert on re-run" requirement). A run whose computed delta
is empty — nothing new or changed — SHALL leave **no trace**: it SHALL NOT mint a `run:load-…`
graph and SHALL NOT record a `prov:Activity`, so PROV records transaction time only for runs that
actually changed the graph. The write path SHALL use the settled namespaces (`gs:`,
`http://gemetenstad.nl/id/`, `http://gemetenstad.nl/run/`) and SHALL reuse the Fuseki HTTP/auth
conventions established in `internal/testdb/fuseki.go`. The package SHALL expose a
`Load(ctx, …) error` orchestrator with a `Config{Reset}` (a reset rebuilds the graph from scratch;
absent it, writes are idempotent upserts), matching the `load/geo` shape, callable from the pipeline
`load`/`derive` stages.

#### Scenario: A changing run gets its own named graph and provenance triple

- **WHEN** a load run writes a non-empty delta (new or changed entities)
- **THEN** the delta's triples are written into a distinct `run:load-…` named graph
- **AND** a matching `prov:Activity` with `prov:generatedAtTime` exists in `run:_provenance`

#### Scenario: A no-op run leaves no trace

- **WHEN** a load run is submitted a candidate whose entities are all already present and unchanged
- **THEN** no `run:load-…` named graph is created for that run
- **AND** no `prov:Activity` is recorded in `run:_provenance`
- **AND** no triples are added to the store

#### Scenario: Reset rebuilds the graph

- **WHEN** the graph load runs with the reset flag
- **THEN** the run graphs are cleared and rebuilt; absent the flag, a conforming write upserts only
  new or changed entities without removing prior runs

## ADDED Requirements

### Requirement: Idempotent SCD2 upsert on re-run

The load path SHALL detect change **per entity**, keyed on the stable subject IRI, by comparing the
candidate's content for that entity against what the graph already holds, **excluding the valid-time
stamps** (`gs:validFrom`/`gs:validTo`) from the comparison. An entity whose non-temporal content
matches the currently-open version in the graph SHALL be treated as **unchanged** and SHALL NOT be
rewritten. Change detection SHALL NOT use whole-IRI delete-insert, which would destroy prior
versions.

An entity is **evolving** if the candidate asserts a `gs:validFrom` for it (an RDF-star annotation on
one of its edges, or a state/period node); otherwise it is **immutable**.

- For an **immutable** entity (stable identity + un-stamped facts, e.g. the Place skeleton, the
  `Claim`): the writer SHALL insert it if absent and SHALL skip it if already present (write-once).
  If a re-asserted immutable entity's non-temporal content **differs** from what is stored, the
  writer SHALL NOT overwrite the stored version and SHALL emit a warning diagnostic identifying the
  subject IRI, so the anomaly is surfaced for later investigation rather than silently dropped.
- For an **evolving** entity whose tracked content has changed: the writer SHALL **open** a new
  version (carrying the candidate's `gs:validFrom`) and SHALL **close** the prior open version by
  stamping its `gs:validTo` equal to the new version's `gs:validFrom` (contiguous intervals), and
  SHALL NOT overwrite or delete the prior version — history is retained.

The writer SHALL take the turtle it is given and SHALL NOT construct the `{| … |}` confidence
annotations itself, and SHALL remain free of any value-store (Postgres) access.

#### Scenario: Re-running unchanged input is a true no-op

- **WHEN** `Load` runs twice on the same candidate with no `Reset`
- **THEN** the second run adds no triples, creates no `run:load-…` graph, and records no
  `prov:Activity`

#### Scenario: A changed tracked field opens a new version and closes the prior

- **WHEN** an evolving entity (e.g. a `locatedAt` edge with a new resolved place or confidence) is
  re-loaded with changed content
- **THEN** a new version is written carrying the candidate's `gs:validFrom`
- **AND** the prior version is closed by stamping its `gs:validTo` equal to the new version's
  `gs:validFrom` (contiguous intervals), leaving exactly one open version
- **AND** the prior version's triples remain in the store (history is not overwritten)

#### Scenario: An unchanged immutable fact is skipped

- **WHEN** a candidate re-asserts an immutable entity (no `gs:validFrom`) that is already present
  with identical content
- **THEN** the writer skips it and writes nothing for that entity, with no warning

#### Scenario: A conflicting immutable fact is skipped and surfaced

- **WHEN** a candidate re-asserts an immutable entity (no `gs:validFrom`) already present but with
  differing non-temporal content
- **THEN** the writer does not overwrite the stored version
- **AND** it emits a warning diagnostic naming the subject IRI so the anomaly can be investigated

### Requirement: The upsert path preserves the SHACL gate

The idempotent-upsert layer SHALL NOT weaken the SHACL load gate: a non-conforming candidate SHALL
be rejected with its violation detail and SHALL cause no partial writes — no closed prior versions,
no run graph, no provenance — regardless of how many of its entities would otherwise be new or
changed. Validation SHALL run before any store mutation.

#### Scenario: A malformed candidate is rejected with no partial writes

- **WHEN** a candidate that fails SHALL-conform validation is submitted to `Load`
- **THEN** `Load` returns the violation detail as an error
- **AND** no `run:load-…` graph is created, no prior version is closed, and no `prov:Activity` is
  recorded
