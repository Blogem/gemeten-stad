# geo-ingest Specification

## Purpose
TBD - created by archiving change geo-backbone-preload. Update Purpose after archive.
## Requirements
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

### Requirement: Per-source ingest selection

The `pipeline ingest` command SHALL accept zero or more positional source names and ingest only the
named sources, in the order given. With no arguments it SHALL ingest all registered sources (the
existing behavior). Each source SHALL remain idempotent and independently runnable, so sources can be
scheduled at different cadences (e.g. the BAG extract monthly, the `gebieden`/CBS boundaries weekly).
An unknown source name SHALL cause the command to exit non-zero with an error listing the valid
source names, without ingesting anything.

#### Scenario: Ingest a single named source

- **WHEN** `pipeline ingest bag` runs
- **THEN** only the BAG source is ingested
- **AND** the `gebieden`/CBS source is not fetched

#### Scenario: No arguments ingests all sources

- **WHEN** `pipeline ingest` runs with no source arguments
- **THEN** all registered sources are ingested, in registration order (BAG, then `gebieden`/CBS)

#### Scenario: Unknown source name fails fast

- **WHEN** `pipeline ingest <unknown>` runs with a name that is not a registered source
- **THEN** the command exits non-zero with an error listing the valid source names
- **AND** no source is ingested

