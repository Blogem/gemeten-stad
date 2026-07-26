## ADDED Requirements

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
