## ADDED Requirements

### Requirement: Registry tree and felling-event TBox

The ontology (`ontology/ontology.ttl`, `gs:` namespace) SHALL model the registry's felled trees and
the felling event as first-class entities, so the coverage `gs:Observation` can link the actual
fellings it rests on. It SHALL define:

- **`gs:Tree`** (`owl:Class`) — an enduring physical tree, identity only (`data:tree/<boomId>`); its
  geometry lives in PostGIS, never as a graph literal.
- **`gs:Felling`** (`owl:Class`) — the event of a tree being felled, with `gs:felledTree`
  (`owl:ObjectProperty`, `gs:Felling` → `gs:Tree`) and `gs:felledOn` (`owl:DatatypeProperty`, range
  `xsd:date`, the felling date as a descriptive-metadata literal).
- **`gs:includesFelling`** (`owl:ObjectProperty`, `gs:Observation` → `gs:Felling`) — the members of an
  Observation: the fellings assigned to a permit.

Each new term SHALL carry `rdfs:label` + `rdfs:comment`. The felling SHALL be modelled as the
`gs:Felling` **event** (not a felling flag on the tree), because it carries its own `gs:felledOn` date
and is the observed fact the audit links to.

**Deferred to Phase 2 (herplantplicht fulfilment), explicitly out of scope here:** the replant model —
`gs:Replanting`, `gs:plantedTree`/`gs:plantedOn`, the felled→replacement tree lineage
(`dct:isReplacedBy`), replacement-tree identities, and the fulfilment `Assessment`. A replant event is
useless without a link to the felling it discharges, and the Phase-1 coverage audit consumes fellings
only — so the replant layer lands as one coherent Phase-2 unit, additively (the replant data stays in
PostGIS until then).

#### Scenario: The tree and felling terms are present and typed

- **WHEN** `ontology/ontology.ttl` is loaded into Fuseki
- **THEN** `gs:Tree` and `gs:Felling` are defined classes, and `gs:felledTree`, `gs:felledOn`,
  `gs:includesFelling` are defined properties, each with `rdfs:label` + `rdfs:comment`

#### Scenario: A felling references the tree it felled and its date

- **WHEN** a felled registry row for tree `data:tree/A` is projected
- **THEN** the graph asserts `data:felling/X a gs:Felling ; gs:felledTree data:tree/A ; gs:felledOn
  "YYYY-MM-DD"^^xsd:date`

#### Scenario: Tree geometry is not in the graph

- **WHEN** the ontology is inspected for tree coordinates
- **THEN** `gs:Tree` carries identity and relations only; the point geometry is referenced via the
  PostGIS value store, never as an RDF literal
