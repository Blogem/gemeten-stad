## ADDED Requirements

### Requirement: Registry tree and felling/replanting event TBox

The ontology (`ontology/ontology.ttl`, `gs:` namespace) SHALL model the registry's felled trees and
their acts as first-class entities, so the coverage `gs:Observation` can link the actual fellings it
rests on and Phase 2 can reason over replant obligations. It SHALL define:

- **`gs:Tree`** (`owl:Class`) — an enduring physical tree, identity only (`data:tree/<boomId>`); its
  geometry lives in PostGIS, never as a graph literal.
- **`gs:Felling`** (`owl:Class`) — the event of a tree being felled, with `gs:felledTree`
  (`owl:ObjectProperty`, `gs:Felling` → `gs:Tree`) and `gs:felledOn` (`owl:DatatypeProperty`, range
  `xsd:date`, the felling date as a descriptive-metadata literal).
- **`gs:Replanting`** (`owl:Class`) — the event of planting a replacement tree, with `gs:plantedTree`
  (`owl:ObjectProperty`, `gs:Replanting` → `gs:Tree`), `gs:plantedOn` (`owl:DatatypeProperty`, range
  `xsd:date`), and `gs:replaces` (`owl:ObjectProperty`, `gs:Replanting` → `gs:Felling`) — the felling
  whose herplantplicht it discharges.
- **`gs:includesFelling`** (`owl:ObjectProperty`, `gs:Observation` → `gs:Felling`) — the members of an
  Observation: the fellings assigned to a permit.

Each new term SHALL carry `rdfs:label` + `rdfs:comment`. The replant SHALL be modelled as the
`gs:Replanting` **event** (carrying its own `gs:plantedOn` date and `gs:replaces` link), NOT as an
annotated `replantedBy` edge — a dated real-world act with its own participants is an n-ary relation,
which RDF models as a node (RDF-star is reserved for uncertainty/provenance about assertions, per
`docs/RDF_MODELING.md` §6). A felled tree with no replant is simply a `gs:Felling` with no
`gs:Replanting` referencing it.

#### Scenario: The tree/felling/replanting terms are present and typed

- **WHEN** `ontology/ontology.ttl` is loaded into Fuseki
- **THEN** `gs:Tree`, `gs:Felling`, `gs:Replanting` are defined classes, and `gs:felledTree`,
  `gs:plantedTree`, `gs:felledOn`, `gs:plantedOn`, `gs:replaces`, `gs:includesFelling` are defined
  properties, each with `rdfs:label` + `rdfs:comment`

#### Scenario: A felling and its replant are a two-event chain

- **WHEN** a felled tree `data:tree/A` is replaced by a new tree `data:tree/B`
- **THEN** the graph asserts `data:felling/X a gs:Felling ; gs:felledTree data:tree/A ; gs:felledOn "…"`
  and `data:replanting/X a gs:Replanting ; gs:plantedTree data:tree/B ; gs:plantedOn "…" ; gs:replaces
  data:felling/X`, and `data:tree/A` and `data:tree/B` are distinct `gs:Tree` identities

#### Scenario: An unmet obligation is a felling with no replanting

- **WHEN** a tree is felled but not yet replanted
- **THEN** its `gs:Felling` exists with no `gs:Replanting` `gs:replaces`-ing it (the absence is the
  unmet herplantplicht — no sentinel or flag is invented)

#### Scenario: Tree geometry is not in the graph

- **WHEN** the ontology is inspected for tree coordinates
- **THEN** `gs:Tree` carries identity and relations only; the point geometry is referenced via the
  PostGIS value store, never as an RDF literal
