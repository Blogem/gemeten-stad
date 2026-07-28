## ADDED Requirements

### Requirement: Project felled trees into the graph

The bomen load SHALL, in addition to its PostGIS output, project the registry's **felled** trees and
their felling events into the RDF graph through the P12 `load/graph.Load` SHACL gate. For each
`kapenherplant` row with a felling date (`kapmaatregelDatumUitgevoerd`), it SHALL assemble:

- `data:tree/<boomId> a gs:Tree` — the felled tree (identity only; geometry stays in PostGIS).
- `data:felling/<id> a gs:Felling ; gs:felledTree data:tree/<boomId> ; gs:felledOn "<date>"^^xsd:date`.

Only **felled** trees SHALL be loaded (trees never felled do not enter the graph). The projection
SHALL be idempotent (re-running an unchanged registry writes no new run graph) and SHALL run before
`derive`, so the coverage Observation's `gs:includesFelling` references pre-loaded fellings.

The **replant projection** (replacement-tree identities, `gs:Replanting`, `gs:plantedOn`,
`dct:isReplacedBy`) is **deferred to Phase 2** — it is an additive extension of this projection over
the same `kapenherplant` rows (`boomNieuwId`, `plantmaatregelDatumUitgevoerd`), not needed by the
Phase-1 coverage audit.

#### Scenario: A felled tree is projected as a felling event

- **WHEN** a `kapenherplant` row has a `kapmaatregelDatumUitgevoerd`
- **THEN** the graph gains a `gs:Tree` and a `gs:Felling` (`gs:felledTree` → that tree, `gs:felledOn`
  the date), written through the SHACL gate

#### Scenario: Only felled trees are loaded

- **WHEN** the registry holds trees that were never felled
- **THEN** they do not enter the graph

#### Scenario: Unchanged re-projection is a no-op

- **WHEN** the bomen graph projection runs twice on an unchanged registry
- **THEN** the second run mints no new run graph (idempotent via the load gate's SCD2 signature)
