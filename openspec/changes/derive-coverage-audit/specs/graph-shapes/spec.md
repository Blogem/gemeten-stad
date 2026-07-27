## ADDED Requirements

### Requirement: Coverage anchor shape

`shapes.ttl` SHALL add a `gs:AuditLinkShape` (`sh:targetClass gs:AuditLink`) gating the stable
coverage anchor with **core SHACL**: `gs:coversIntervention` present (`sh:minCount 1`) and pointing
at a `gs:Intervention` (`sh:class gs:Intervention`).

#### Scenario: An anchor without its permit is rejected

- **WHEN** a `gs:AuditLink` node has no `gs:coversIntervention`
- **THEN** the shapes report it as non-conforming

#### Scenario: A well-formed anchor conforms

- **WHEN** a `gs:AuditLink` node has `gs:coversIntervention` an `gs:Intervention`
- **THEN** it conforms

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
