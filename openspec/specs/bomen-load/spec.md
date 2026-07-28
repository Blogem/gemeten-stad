# bomen-load Specification

## Purpose
TBD - created by archiving change bomen-preload. Update Purpose after archive.
## Requirements
### Requirement: Load both registries into typed PostGIS tables as-is

The system SHALL load the landed `kapenherplant` and `stamgegevens` data into their own typed
PostGIS tables, retaining all columns present in the source, with no row filtering.

#### Scenario: Both tables populated from a clean volume

- **WHEN** the bomen load runs against the full landed snapshot on a clean volume
- **THEN** the `kapenherplant` table contains 35,202 rows
- **AND** the `stamgegevens` table contains 323,728 rows
- **AND** every source column is present on the loaded rows

### Requirement: Non-destructive reload (never lose a referenced tree row)

A refresh SHALL NOT physically delete previously loaded rows. The load SHALL stage the new export
into a `*_staging` table per dataset, then upsert into the target keyed by the source's natural
`id`: a new row is inserted, an unchanged row is a no-op, and a previously loaded row absent from
the new export is **soft-deleted** by stamping `source_deleted_at` with the load timestamp, not
removed. Re-running the load against the same export SHALL be a no-op.

#### Scenario: New export adds a row

- **WHEN** a refresh loads an export containing a row not previously loaded
- **THEN** the new row is inserted
- **AND** all previously loaded rows are retained unchanged

#### Scenario: A row absent from the new export is soft-deleted

- **WHEN** a refresh loads an export in which a previously loaded row no longer appears
- **THEN** that row is retained with `source_deleted_at` set to the load timestamp, not physically deleted

#### Scenario: Re-running the same export is a no-op

- **WHEN** the load runs a second time against the same landed export
- **THEN** no rows are inserted, updated, or soft-deleted

### Requirement: Reset rebuilds from landed data

The load SHALL support a reset flag that drops and rebuilds both target tables from the landed
data, for a clean dev rebuild. Absent the flag, the load is the non-destructive upsert above.

#### Scenario: Reset rebuilds both tables

- **WHEN** the load runs with the reset flag
- **THEN** the `kapenherplant` and `stamgegevens` target tables are dropped and rebuilt from the landed data

### Requirement: Materialize the kapenherplant-to-stamgegevens point resolution

For each loaded `kapenherplant` row, the system SHALL resolve a point geometry by looking up
`stamgegevens` by `boomId`; on a miss, it SHALL retry via `boomNieuwId`. The resolved geometry (or
its absence) and which key resolved it SHALL be stored on the `kapenherplant` row as a
`resolvedGeom` and a `resolvedVia` value of `boomId`, `boomNieuwId`, or `unresolved`.

#### Scenario: Resolved via boomId

- **WHEN** a `kapenherplant` row's `boomId` matches a `stamgegevens` row
- **THEN** `resolvedGeom` is set to that `stamgegevens` row's geometry
- **AND** `resolvedVia` is `boomId`

#### Scenario: Resolved via boomNieuwId fallback

- **WHEN** a `kapenherplant` row's `boomId` does not match any `stamgegevens` row but its `boomNieuwId` does
- **THEN** `resolvedGeom` is set to the `boomNieuwId`-matched `stamgegevens` row's geometry
- **AND** `resolvedVia` is `boomNieuwId`

#### Scenario: Unresolved is a first-class outcome, not an error

- **WHEN** neither `boomId` nor `boomNieuwId` matches any `stamgegevens` row
- **THEN** `resolvedGeom` is left null
- **AND** `resolvedVia` is `unresolved`
- **AND** the load does not fail or skip the row

### Requirement: A spot-checked tree resolves without a BAG dependency

The system SHALL make a felled tree's point, buurt, and nearest address available directly from
the loaded columns — the resolved geometry (above), `gbdBuurtId`, and
`dichtstbijzijndeBagAdres`/`Postcode` — without querying BAG or the `location` resolver.

#### Scenario: A loaded kapenherplant row resolves to point + buurt + nearest address

- **WHEN** a `kapenherplant` row with a resolved geometry is queried
- **THEN** its point, its `gbdBuurtId`, and its nearest BAG address/postcode are all readable from the loaded row alone

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

