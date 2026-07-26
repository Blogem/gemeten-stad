## ADDED Requirements

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
