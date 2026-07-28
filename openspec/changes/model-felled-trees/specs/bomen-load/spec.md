## ADDED Requirements

### Requirement: Project felled trees and their acts into the graph

The bomen load SHALL, in addition to its PostGIS output, project the registry's **felled** trees and
their acts into the RDF graph through the P12 `load/graph.Load` SHACL gate. For each `kapenherplant`
row with a felling date (`kapmaatregelDatumUitgevoerd`), it SHALL assemble:

- `data:tree/<boomId> a gs:Tree` — the felled tree (identity only; geometry stays in PostGIS).
- `data:felling/<id> a gs:Felling ; gs:felledTree data:tree/<boomId> ; gs:felledOn "<date>"^^xsd:date`.
- when the row carries a replant (`boomNieuwId` + `plantmaatregelDatumUitgevoerd`):
  `data:replanting/<id> a gs:Replanting ; gs:plantedTree data:tree/<boomNieuwId> ; gs:plantedOn
  "<date>"^^xsd:date ; gs:replaces data:felling/<id>`, plus a **bare** `data:tree/<boomNieuwId> a
  gs:Tree` identity for the replacement tree.

Only **felled** trees SHALL be loaded as full entities. Replacement trees (`boomNieuwId`) SHALL be
minted as bare `gs:Tree` identities only (they are planted, not felled — no attributes loaded), except
where a replacement is itself later felled, in which case its own felled row loads it fully. The
projection SHALL be idempotent (re-running an unchanged registry writes no new run graph) and SHALL run
before `derive`, so the coverage Observation's `gs:includesFelling` references pre-loaded fellings.

#### Scenario: A felled tree is projected as a felling event

- **WHEN** a `kapenherplant` row has a `kapmaatregelDatumUitgevoerd`
- **THEN** the graph gains a `gs:Tree`, a `gs:Felling` (`gs:felledTree` → that tree, `gs:felledOn` the
  date), written through the SHACL gate

#### Scenario: A replant is projected as a replanting event to a new tree

- **WHEN** a felled row also carries `boomNieuwId` + `plantmaatregelDatumUitgevoerd`
- **THEN** the graph gains a `gs:Replanting` (`gs:plantedTree` → a bare `gs:Tree` for `boomNieuwId`,
  `gs:plantedOn` the date, `gs:replaces` → the felling), and the replacement tree is a distinct
  `gs:Tree` identity from the felled tree

#### Scenario: A felled-not-replanted row has no replanting

- **WHEN** a felled row has no replant recorded
- **THEN** only the `gs:Tree` + `gs:Felling` are projected; no `gs:Replanting` references that felling

#### Scenario: Only felled trees are loaded as full entities

- **WHEN** the registry holds trees that were never felled
- **THEN** they do not enter the graph (except as bare `gs:Tree` identities when referenced as a
  replacement)

#### Scenario: Unchanged re-projection is a no-op

- **WHEN** the bomen graph projection runs twice on an unchanged registry
- **THEN** the second run mints no new run graph (idempotent via the load gate's SCD2 signature)
