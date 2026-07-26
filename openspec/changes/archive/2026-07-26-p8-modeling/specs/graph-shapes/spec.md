## ADDED Requirements

### Requirement: Structural shapes in core SHACL

`ontology/shapes.ttl` SHALL define SHACL `NodeShape`s that enforce the structural invariants of
the TBox using core SHACL (`sh:property`, `sh:path`, `sh:minCount`, `sh:class`, `sh:datatype`).
At minimum, a `gs:Intervention` SHALL have at least one `gs:locatedAt` edge to a node of class
`gs:Place`. Each shape's constraints SHALL carry an `sh:message`.

#### Scenario: Well-formed instance conforms

- **WHEN** a well-formed instance (an `Intervention` with a `gs:Place` `locatedAt` edge
  carrying a confidence annotation) is validated against `shapes.ttl`
- **THEN** the report has `sh:conforms true`

#### Scenario: Missing structure is rejected

- **WHEN** an `Intervention` with no `gs:locatedAt` edge is validated
- **THEN** the report has `sh:conforms false` and names the offending node with the shape's
  `sh:message`

### Requirement: Confidence-presence shape in SPARQL-star

`shapes.ttl` SHALL enforce that every `gs:locatedAt` edge carries a `gs:confidence` annotation
using a `sh:sparql` constraint with a SPARQL-star `<< … >>` `FILTER NOT EXISTS` pattern (core
SHACL cannot path into a quoted triple — Spike E Finding 2). The same mechanism SHALL bound
`gs:confidence` to the range [0,1].

#### Scenario: Missing confidence annotation is rejected

- **WHEN** an `Intervention` has a `gs:locatedAt` edge written without a `gs:confidence`
  annotation (a fuzzy edge written as if exact)
- **THEN** the report has `sh:conforms false` with a `sh:SPARQLConstraintComponent` violation

#### Scenario: RDF-star presence does not mask a real violation

- **WHEN** a graph mixes a valid RDF-star edge with a separate structural violation
- **THEN** validation still reports `sh:conforms false` and pinpoints the offending node
  (Jena #3503 does not affect this pattern on 5.5.0)

### Requirement: Controlled value sets backed by SKOS

`shapes.ttl` SHALL enforce that controlled-vocabulary properties (species, activity/measure,
status, and caveat values) resolve to concepts in the `domain-vocabulary` ConceptScheme, so
out-of-vocabulary values do not enter the graph.

#### Scenario: Out-of-vocab value is rejected

- **WHEN** an instance uses a species or status value that is not a concept in the vocab
- **THEN** the report has `sh:conforms false`

#### Scenario: In-vocab value conforms

- **WHEN** an instance uses a species value that is a known vocab concept
- **THEN** that constraint conforms

### Requirement: Caveat-presence rules

`shapes.ttl` SHALL enforce that an edge marked as approximate or unresolved carries an explicit
caveat — a resolved-location edge that is not an exact match SHALL carry a caveat flag (e.g.
`gs:unresolvedLocation`, `gs:timeMismatch`, `gs:weakLink`) rather than being written as if
exact, so no half-broken data enters the graph.

#### Scenario: Unresolved location without a caveat is rejected

- **WHEN** a `locatedAt` edge resolved via fallback carries no caveat flag
- **THEN** the report has `sh:conforms false`
