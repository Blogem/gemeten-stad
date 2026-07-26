# bomen-ingest Specification

## Purpose
TBD - created by archiving change bomen-preload. Update Purpose after archive.
## Requirements
### Requirement: Land `kapenherplant` and `stamgegevens` verbatim

The system SHALL fetch the full city-wide `kapenherplant` and `stamgegevens` datasets from the
Amsterdam Datapunt `bomen` API — `kapenherplant` via paged JSON, and `stamgegevens` via the
uncapped GeoJSON export because it exceeds the API's 100-page paging cap — and SHALL land each
fetched response verbatim (no row/column filtering) into the raw landing store together with a
provenance record (source URL template, fetch timestamp, byte size, content hash).

#### Scenario: Full city-wide pull lands both datasets

- **WHEN** an ingest run executes for `kapenherplant` and `stamgegevens`
- **THEN** every page of both datasets is landed verbatim in the raw store
- **AND** a provenance record is written recording the source URL template, fetch timestamp, byte size, and content hash

#### Scenario: No row or column filtering at ingest time

- **WHEN** a page is landed
- **THEN** all columns present in the source response are retained
- **AND** no row is dropped based on field values (e.g. null lifecycle dates are not filtered out at ingest)

### Requirement: Idempotent scheduled full reload

Because neither dataset exposes a documented delta/mutation feed, a refresh SHALL be a full re-page
of the dataset, landed as a new immutable version. All landed versions SHALL be preserved (the tree
registry changes over time and audit needs the history — unlike BAG, which overwrites its single
large blob). Landing SHALL be content-addressed-idempotent: a refresh whose fetched content is
identical to the latest landed version SHALL NOT create a new version, and a refresh SHALL preserve
prior versions and their provenance records rather than erasing them.

#### Scenario: Re-running ingest against unchanged upstream content is a no-op

- **WHEN** ingest runs twice with no change in the source data between runs
- **THEN** the second run creates no new version (its content hashes identically to the latest)
- **AND** no prior version or provenance record is altered

#### Scenario: A scheduled refresh preserves prior versions

- **WHEN** a new scheduled refresh lands changed content as a fresh version
- **THEN** the previous version and its provenance record remain present and readable
- **AND** the new version and its provenance record are added alongside them

### Requirement: Client-side handling of known API quirks

The system SHALL NOT rely on the `[isnull]` query filter (documented to silently return an empty
result with no error) and SHALL NOT attempt a spatial query (`geometrie[within]`) against
`kapenherplant` (documented to return HTTP 403). Any null-lifecycle-date handling SHALL happen
client-side after fetch.

#### Scenario: Null-date filtering happens client-side

- **WHEN** the ingest logic needs to distinguish rows with a null lifecycle date
- **THEN** it filters the fetched rows in code, not via an `[isnull]` query parameter

#### Scenario: No spatial query is issued against kapenherplant

- **WHEN** ingest fetches `kapenherplant`
- **THEN** the request does not include a `geometrie[within]` filter

