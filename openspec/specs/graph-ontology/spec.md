# graph-ontology Specification

## Purpose
TBD - created by archiving change p8-modeling. Update Purpose after archive.
## Requirements
### Requirement: Intervention–Place–Claim–Observation TBox

The ontology SHALL define, in `ontology/ontology.ttl` under the `gs:`
(`http://gemetenstad.nl/ns#`) namespace, the core classes and relations of the graph model
from `IMPLEMENTATION_PLAN.md` §3: `gs:Intervention`, `gs:Place`, `gs:Project`, `gs:Claim`,
`gs:Observation`, `gs:AuditLink`, and `gs:Assessment`, with the object properties
`gs:locatedAt` (Intervention → Place), `gs:partOfProject` (Intervention → Project),
`gs:claims` (Intervention → Claim), and `gs:testedAgainst` (Claim → Observation). The besluit
**publication date** SHALL be carried on the `gs:Intervention` as `dct:available` (`xsd:date`),
reusing Dublin Core Terms (reuse-first: no minted `gs:` term where a standard vocabulary fits).
**Quantitative and geometric values** (tree counts, polygons, replant fractions, coordinates) SHALL
NOT be modeled as graph literals — they live in the Postgres/PostGIS value store. The graph carries
identity, relations, provenance, confidence, and **descriptive-metadata literals only** — dates via
`dct:`, and the `gs:confidence`/`gs:evidence` and `gs:validFrom`/`gs:validTo` structural literals.

#### Scenario: Ontology loads without error

- **WHEN** `ontology/ontology.ttl` is parsed and loaded into Fuseki
- **THEN** it loads with no syntax or consistency error
- **AND** all seven classes and the four object properties above are present with `rdfs:label`
  and `rdfs:comment`

#### Scenario: partOfProject is optional and sparse

- **WHEN** an `Intervention` with no `gs:partOfProject` edge is validated against the shapes
- **THEN** it conforms, because `partOfProject` is a non-required relation (present only for
  renewal projects)

#### Scenario: The Intervention carries its publication date as a descriptive-metadata literal

- **WHEN** a besluit `gs:Intervention` is inspected
- **THEN** it carries a `dct:available` `xsd:date` publication-date literal (Dublin Core Terms, not a
  minted `gs:` term), and that date is not valid-time-stamped

#### Scenario: Quantitative values are not in the graph

- **WHEN** the ontology is inspected for tree counts, geometries, or replant fractions
- **THEN** no class or property models those as RDF literals; they are represented only by
  reference/identity that resolves to the value store

### Requirement: RDF-star uncertainty convention

Fuzzy edges — `gs:locatedAt` and `gs:AuditLink` relations — SHALL carry a `gs:confidence`
(decimal in [0,1]) and `gs:evidence` (string) as an RDF-star statement annotation written with
the `{| … |}` form, so the base edge is asserted (structural SHACL sees it) and the annotation
is stored (readable via SPARQL-star). The ontology SHALL define `gs:confidence` and
`gs:evidence` as the annotation terms. Confidence SHALL NOT be written with the bare
`<< … >>` form (which does not assert the base edge).

#### Scenario: Confidence read back via SPARQL-star

- **WHEN** an intervention's `locatedAt` edge is written as
  `:i gs:locatedAt :p {| gs:confidence 0.7 ; gs:evidence "…" |}`
- **THEN** a SPARQL-star query `<< :i gs:locatedAt :p >> gs:confidence ?c` returns `0.7`
- **AND** a plain triple pattern `:i gs:locatedAt :p` also matches (the base edge is asserted)

#### Scenario: Confidence range

- **WHEN** a `gs:confidence` value outside [0,1] is written
- **THEN** the shapes report it as non-conforming

### Requirement: Bitemporal and PROV temporal convention

The model SHALL separate stable identity from time-varying state. Each entity (permit/
intervention, tree, place, project, claim) SHALL have one stable IRI that is never changed or
deleted. Immutable facts (e.g. a publication date, a felling date) SHALL NOT be time-stamped.
Evolving state (legal force, fulfilment assessments, resolved-location links) SHALL carry
valid time (`gs:validFrom`/`gs:validTo`) via one of two mechanisms: (a) an RDF-star statement
annotation for a single evolving fact, or (b) a state/period node (`gs:Assessment`,
`gs:LegalStatusPeriod`) when several attributes move together. Transaction time SHALL be
represented as PROV: each run writes into a run-stamped named graph and records a
`prov:Activity` with `prov:generatedAtTime` in a dedicated `run:_provenance` named graph.

#### Scenario: Assessment is superseded, never overwritten

- **WHEN** a later derive run finds the fulfilment state changed (e.g. 9→18 replanted)
- **THEN** the current `Assessment` is closed by stamping its `gs:validTo`
- **AND** a new `Assessment` node is opened with a new `gs:validFrom`
- **AND** the prior `Assessment` and its closed valid interval remain in the graph (history is
  retained)

#### Scenario: Immutable facts stay un-stamped

- **WHEN** a felling date or publication date is written
- **THEN** it carries no `gs:validFrom`/`gs:validTo` (recording when it happened is not the
  fact evolving)

#### Scenario: Transaction-time lives in the provenance graph

- **WHEN** a load run writes an intervention
- **THEN** the intervention triples are in a `run:load-…` named graph
- **AND** a `prov:Activity` with `prov:generatedAtTime` for that run is in the
  `run:_provenance` named graph (not the default graph, which `unionDefaultGraph` shadows)

### Requirement: Place active-status is evolving state

The ontology (`ontology/ontology.ttl`) SHALL define a `gs:active` property expressing whether a
`gs:Place` is currently an active area in the source `gebieden` register (a boolean;
`rdfs:domain gs:Place`), with `rdfs:label` and `rdfs:comment`. A Place's active-status SHALL be
modeled as **evolving state**, not an immutable fact: the `gs:active` statement SHALL carry valid
time via the RDF-star annotation mechanism already defined for evolving facts
(`<< :place gs:active true >> gs:validFrom … ; gs:validTo …`), so that an area going inactive
(deprecated / soft-deleted upstream) is a superseded version — the prior `gs:active true` interval
is closed with a `gs:validTo` and a `gs:active false` interval is opened — and history is retained.
The area's identity (its IRI) SHALL remain stable and timeless across the transition; only the
active-status is stamped. Geometry SHALL remain in PostGIS — `gs:active` is a status flag, not a
value.

#### Scenario: gs:active is defined on Place

- **WHEN** `ontology/ontology.ttl` is loaded into Fuseki
- **THEN** `gs:active` is present with `rdfs:domain gs:Place`, `rdfs:label`, and `rdfs:comment`
- **AND** it loads with no syntax or consistency error

#### Scenario: Active-status carries valid time, identity does not

- **WHEN** a `gs:Place`'s `gs:active` statement is written
- **THEN** it carries a `gs:validFrom` via the `{| … |}` RDF-star annotation (it is evolving state)
- **AND** the Place's IRI, `rdfs:label`, and `gs:within` remain the stable identity/skeleton the
  active-status hangs off

### Requirement: Coverage anchor and coverage-period TBox

The ontology (`ontology/ontology.ttl`, `gs:` namespace) SHALL model the derived permit↔registry
coverage as a stable **anchor** plus time-versioned **period** nodes:

- **`gs:AuditLink`** (the class P8 already declares) SHALL be the stable per-permit **anchor** — "the
  coverage audit of this permit," one per permit, timeless — carrying `gs:coversIntervention`
  (`owl:ObjectProperty`, `gs:AuditLink` → `gs:Intervention`). Its comment SHALL be updated from
  "confidence-bearing link" to this anchor role (the confidence now lives on the periods).
- **`gs:CoveragePeriod`** (new class) SHALL be a dated coverage state: it points at its anchor via
  `gs:versionOf` (added by `state-node-versioning`) and carries `gs:validFrom`/`gs:validTo`,
  `gs:evidence`, and — when matched — `gs:linksObservation` (`owl:ObjectProperty`,
  `gs:CoveragePeriod` → `gs:Observation`), `gs:confidence`, `gs:granularity`
  (`gs:address`/`gs:postcode`/`gs:buurt`), and optional `gs:caveat`; or — when unmatched —
  `gs:noSourceFound` (boolean `true`). `gs:confidence`/`gs:evidence` are **plain properties** on the
  period (not RDF-star annotations).

The ontology SHALL define the new terms `gs:CoveragePeriod`, `gs:coversIntervention`,
`gs:linksObservation`, `gs:granularity`, and `gs:noSourceFound`, each with `rdfs:label` and
`rdfs:comment`. These complete the attachment the P8 TBox left out.

#### Scenario: The coverage terms are present and typed

- **WHEN** `ontology/ontology.ttl` is loaded into Fuseki
- **THEN** `gs:AuditLink` documents the anchor role with `gs:coversIntervention` → `gs:Intervention`;
  `gs:CoveragePeriod` is defined; and `gs:linksObservation` (range `gs:Observation`),
  `gs:granularity`, and `gs:noSourceFound` are defined, each with `rdfs:label` + `rdfs:comment`

#### Scenario: A matched coverage period is a plain-property node

- **WHEN** a matched period is written as `data:auditlink/Z/<key> a gs:CoveragePeriod ; gs:versionOf
  data:auditlink/Z ; gs:validFrom "…" ; gs:linksObservation data:observation/Z ; gs:confidence 0.7 ;
  gs:granularity gs:address ; gs:evidence "…"`, alongside `data:auditlink/Z a gs:AuditLink ;
  gs:coversIntervention data:intervention/Z`
- **THEN** a plain triple query `?p a gs:CoveragePeriod ; gs:confidence ?c` returns `0.7` (no
  SPARQL-star needed) and `?p gs:versionOf ?anchor . ?anchor gs:coversIntervention ?permit` reaches
  the permit

#### Scenario: A no-source coverage period records the attempt

- **WHEN** a period is written as `data:auditlink/Z/<key> a gs:CoveragePeriod ; gs:versionOf
  data:auditlink/Z ; gs:validFrom "…" ; gs:noSourceFound true ; gs:evidence "searched …"`
- **THEN** it is a valid coverage period recording that a match was attempted and none found, with no
  `gs:Observation` and no `gs:linksObservation`

