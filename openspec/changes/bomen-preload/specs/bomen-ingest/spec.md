## ADDED Requirements

### Requirement: Land `kapenherplant` and `stamgegevens` verbatim

The system SHALL fetch the full city-wide `kapenherplant` and `stamgegevens` datasets from the
Amsterdam Datapunt `bomen` API by paging through all rows, and SHALL land each page verbatim (no
row/column filtering) into the raw landing store together with a provenance record (source URL
template, fetch timestamp, total row count, content hash).

#### Scenario: Full city-wide pull lands both datasets

- **WHEN** an ingest run executes for `kapenherplant` and `stamgegevens`
- **THEN** every page of both datasets is landed verbatim in the raw store
- **AND** a provenance record is written recording the source URL template, fetch timestamp, row count, and content hash

#### Scenario: No row or column filtering at ingest time

- **WHEN** a page is landed
- **THEN** all columns present in the source response are retained
- **AND** no row is dropped based on field values (e.g. null lifecycle dates are not filtered out at ingest)

### Requirement: Idempotent scheduled full reload

Because neither dataset exposes a documented delta/mutation feed, a refresh SHALL be a full re-page
of the dataset, landed as a new dated snapshot. Landing SHALL be idempotent: re-running an ingest
against an already-landed snapshot SHALL NOT re-fetch, and a refresh SHALL preserve prior
provenance records rather than erasing them.

#### Scenario: Re-running ingest against an unchanged snapshot is a no-op

- **WHEN** ingest runs twice in a row with no new snapshot requested
- **THEN** the second run does not re-fetch the already-landed data

#### Scenario: A scheduled refresh preserves prior provenance

- **WHEN** a new scheduled refresh lands a fresh snapshot
- **THEN** the previous snapshot's provenance record remains present and readable
- **AND** the new snapshot's provenance record is added alongside it

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
