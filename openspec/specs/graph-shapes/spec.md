# graph-shapes Specification

## Purpose
TBD - created by archiving change p8-modeling. Update Purpose after archive.
## Requirements
### Requirement: Structural shapes in core SHACL

`ontology/shapes.ttl` SHALL define SHACL `NodeShape`s that enforce the structural invariants of
the TBox using core SHACL (`sh:property`, `sh:path`, `sh:minCount`, `sh:maxCount`, `sh:class`,
`sh:datatype`). At minimum, a `gs:Intervention` SHALL have at least one `gs:locatedAt` edge to a node
of class `gs:Place`, and SHALL carry exactly one `dct:available` publication date typed `xsd:date`
(`sh:minCount 1`, `sh:maxCount 1`, `sh:datatype xsd:date`). Each shape's constraints SHALL carry an
`sh:message`.

#### Scenario: Well-formed instance conforms

- **WHEN** a well-formed instance (an `Intervention` with a `gs:Place` `locatedAt` edge carrying a
  confidence annotation and a `dct:available` `xsd:date`) is validated against `shapes.ttl`
- **THEN** the report has `sh:conforms true`

#### Scenario: Missing structure is rejected

- **WHEN** an `Intervention` with no `gs:locatedAt` edge is validated
- **THEN** the report has `sh:conforms false` and names the offending node with the shape's
  `sh:message`

#### Scenario: A missing or non-date publication date is rejected

- **WHEN** an `Intervention` has no `dct:available`, or a `dct:available` value that is not an
  `xsd:date`
- **THEN** the report has `sh:conforms false` with the shape's `sh:message`

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

### Requirement: Coverage anchor shape

`shapes.ttl` SHALL add a `gs:AuditLinkShape` (`sh:targetClass gs:AuditLink`) gating the stable
coverage anchor with **core SHACL**: `gs:coversIntervention` present (`sh:minCount 1`) and pointing
at an Intervention **IRI in the intervention namespace** (`sh:nodeKind sh:IRI` +
`sh:pattern "^http://gemetenstad.nl/id/intervention/"`). It SHALL NOT use `sh:class gs:Intervention`:
the load gate validates each candidate merged with ontology+vocab only — never the already-stored
data — so a `sh:class` check on a **cross-load reference** (the derive references an Intervention
that koop loaded earlier, it does not re-declare it) is unsatisfiable without the derive fragilely
re-serializing another load's Intervention. A structural IRI+namespace gate is the correct SHACL
form for such a reference.

#### Scenario: An anchor without its permit is rejected

- **WHEN** a `gs:AuditLink` node has no `gs:coversIntervention`
- **THEN** the shapes report it as non-conforming

#### Scenario: A well-formed anchor conforms

- **WHEN** a `gs:AuditLink` node has `gs:coversIntervention` an IRI in the
  `http://gemetenstad.nl/id/intervention/` namespace
- **THEN** it conforms (without the referenced Intervention needing to be present in the same
  candidate)

#### Scenario: An out-of-namespace coversIntervention is rejected

- **WHEN** a `gs:AuditLink` node's `gs:coversIntervention` is an IRI outside the intervention
  namespace (or a literal/blank node)
- **THEN** the shapes report it as non-conforming

### Requirement: Coverage-period shape in core SHACL

`shapes.ttl` SHALL add a `gs:CoveragePeriodShape` (`sh:targetClass gs:CoveragePeriod`) gating a dated
coverage period with **core SHACL** (no `sh:sparql` — the values are plain properties on the node).
It SHALL require: `gs:versionOf` present (`sh:minCount 1`) and pointing at a `gs:AuditLink`
(`sh:class gs:AuditLink`); `gs:validFrom` present; `gs:evidence` present; and **exactly one**
(`sh:xone`) of the two outcome branches —

- **matched:** `gs:linksObservation` present (`sh:minCount 1`, `sh:class gs:Observation`) **and**
  `gs:confidence` present, with `gs:noSourceFound` absent; or
- **no-source:** `gs:noSourceFound` = `true` (`sh:hasValue true`), with `gs:linksObservation` /
  `gs:confidence` absent.

`gs:confidence`, where present, SHALL be a decimal in `[0,1]` (`sh:minInclusive 0`,
`sh:maxInclusive 1`); `gs:granularity`, where present, SHALL be one of
`gs:address`/`gs:postcode`/`gs:buurt` (`sh:in`). Before this change only `gs:locatedAt` was gated, so
derived coverage passed the shapes vacuously.

#### Scenario: A well-formed matched period conforms

- **WHEN** a `gs:CoveragePeriod` has `gs:versionOf` an anchor, `gs:validFrom`, `gs:evidence`,
  `gs:linksObservation` an Observation, `gs:confidence 0.7`, and `gs:granularity gs:address`
- **THEN** it conforms

#### Scenario: A well-formed no-source period conforms

- **WHEN** a `gs:CoveragePeriod` has `gs:versionOf`, `gs:validFrom`, `gs:evidence`, and
  `gs:noSourceFound true`, with no `gs:linksObservation`/`gs:confidence`
- **THEN** it conforms

#### Scenario: A period matching neither branch is rejected

- **WHEN** a `gs:CoveragePeriod` carries `gs:linksObservation` but no `gs:confidence` (nor
  `gs:noSourceFound`)
- **THEN** the shapes report it as non-conforming (it satisfies neither `sh:xone` branch)

#### Scenario: An out-of-range confidence is rejected

- **WHEN** a matched `gs:CoveragePeriod` carries `gs:confidence` outside `[0,1]`
- **THEN** the shapes report it as non-conforming

#### Scenario: A period missing its anchor or evidence is rejected

- **WHEN** a `gs:CoveragePeriod` lacks `gs:versionOf` (or lacks `gs:evidence`)
- **THEN** the shapes report it as non-conforming

