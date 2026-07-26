## ADDED Requirements

### Requirement: Land the BAG bulk extract into the raw store

The system SHALL fetch the national _LV BAG 2.0 Extract_ from the PDOK atom feed and land it
verbatim into the raw landing store (bronze) with provenance (source URL, fetch timestamp, byte
size), before any load. The fetch SHALL be idempotent: if the same extract is already present in
the landing store it SHALL be reused, not re-downloaded. There SHALL be no per-record or per-column
filtering at ingest — the extract lands as-is. Landing SHALL be non-destructive: a refresh SHALL
NOT erase the provenance history of previously landed extracts (a new monthly extract is landed as
a new dated snapshot). The large extract blob itself MAY be pruned to the latest snapshot (it is a
cheap idempotent re-fetch), but its provenance record SHALL be retained.

#### Scenario: Extract absent from the landing store

- **WHEN** `pipeline ingest` runs the BAG work item and no extract is present in the raw landing store
- **THEN** the extract is downloaded from the PDOK atom feed into the landing store
- **AND** a provenance record is written capturing the source URL, the fetch timestamp, and the byte size

#### Scenario: Extract already landed

- **WHEN** `pipeline ingest` runs the BAG work item and the same extract is already present in the landing store
- **THEN** the download is skipped and the existing landed extract is reused

#### Scenario: Monthly full reload, not incremental

- **WHEN** a refresh lands a newer monthly extract
- **THEN** the newer extract is landed with its own provenance record, preserving the prior extract's provenance history
- **AND** no daily Mutatie-Levering (ML) delta is fetched or applied

### Requirement: Land the boundary geometries into the raw store

The system SHALL harvest the Amsterdam `gebieden` buurt and wijk polygons (Datapunt API, GeoJSON,
requested in RD / EPSG:28992) for the whole municipality (gemeente `0363`) — not scoped to a single
stadsdeel — and SHALL land the CBS "wijken en buurten" WFS reference (`gemeentecode='GM0363'`),
each verbatim into the raw landing store with provenance. The harvest SHALL be idempotent.
`gebieden` polygons SHALL be keyed by `identificatie` (= `gbdBuurtId`), the point-in-polygon
ground truth.

#### Scenario: Harvest the gebieden polygons

- **WHEN** `pipeline ingest` runs the gebieden work item
- **THEN** the whole-city `gebieden` buurt and wijk polygons are fetched as RD (EPSG:28992) GeoJSON and landed with provenance
- **AND** each buurt feature carries its `identificatie` (`gbdBuurtId`)

#### Scenario: Land the CBS reference

- **WHEN** `pipeline ingest` runs the gebieden work item
- **THEN** the CBS "wijken en buurten" boundaries filtered to `gemeentecode='GM0363'` are landed with provenance as a cross-reference layer

#### Scenario: Boundaries already landed

- **WHEN** the gebieden/CBS boundaries are already present in the landing store
- **THEN** the harvest is skipped and the existing landed geometries are reused
