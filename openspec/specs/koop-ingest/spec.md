# koop-ingest Specification

## Purpose
TBD - created by archiving change ingest-koop. Update Purpose after archive.
## Requirements
### Requirement: Scoped SRU harvest of Amsterdam kap/verplant omgevingsvergunningen

The system SHALL harvest omgevingsvergunning publications from the KOOP SRU 2.0 endpoint using a
scoped `searchRetrieve` query that filters on publishing authority Amsterdam
(`dt.creator any "Amsterdam"`), document type `omgevingsvergunning`
(`dt.type any "omgevingsvergunning"`), the kap/verplant activity full-text term set
(`cql.textAndIndexes any "kappen vellen rooien verplanten houtopstand"`), and a publication-date
lower bound of 2021-01-01 or later (`dt.available>=`). The harvest SHALL be Amsterdam-wide: it
SHALL NOT apply any stadsdeel/Noord, geometry, or location filter — Noord selection is deferred to
the load stage. The query SHALL use `any` relations (not exact `==`) and SHALL page through all
results using `numberOfRecords`, `startRecord`, and `maximumRecords`.

#### Scenario: The query builder assembles the scoped Amsterdam kap/verplant query

- **WHEN** the harvester builds its SRU query
- **THEN** the query includes the `dt.creator any "Amsterdam"`, `dt.type any "omgevingsvergunning"`, kap/verplant `cql.textAndIndexes any` term set, and `dt.available>=` clauses
- **AND** it contains no stadsdeel, geometry, or postcode filter

#### Scenario: Results are paged to exhaustion

- **WHEN** an SRU query reports more results than one page holds
- **THEN** the harvester reads `numberOfRecords` and advances `startRecord` by `maximumRecords` until every record has been retrieved

#### Scenario: Both aanvraag and besluit publications are harvested

- **WHEN** the corpus contains both an aanvraag and a besluit publication for the same zaaknummer
- **THEN** both publications are harvested and landed
- **AND** no aanvraag/besluit deduplication is applied at harvest time

### Requirement: Land each publication verbatim with provenance

The system SHALL land each harvested publication as its own raw artifact in the landing store,
keyed by its publication identifier (`dcterms:identifier`, e.g. `gmb-2022-291126`), writing the SRU
record bytes **verbatim** (no field mapping, filtering, or reshaping) together with a provenance
record capturing the source SRU URL, fetch timestamp, byte size, and content hash.

#### Scenario: A harvested publication is landed verbatim

- **WHEN** the harvester retrieves a publication record
- **THEN** the record bytes are written to the raw store under the publication's identifier, unmodified
- **AND** a provenance record is written recording the source SRU URL, fetch timestamp, byte size, and content hash

#### Scenario: No structured field is extracted or dropped at harvest

- **WHEN** a publication record is landed
- **THEN** all content present in the SRU record is retained
- **AND** no publication is dropped for missing a structured field (e.g. a missing zaaknummer, geometry, or count)

### Requirement: Incremental, idempotent re-runs

Re-runs SHALL fetch only new or changed publications. The harvester SHALL maintain a publication-date
high-water mark cursor (max `dt.available` landed) and, on each run, SHALL query from that
high-water mark minus a small overlap window (so late or back-dated same-period publications are not
missed), skipping any publication whose identifier is already landed. A re-run against unchanged
upstream content SHALL be a no-op (no new artifact and no altered provenance).

#### Scenario: First run on a clean volume harvests the full window

- **WHEN** the harvester runs against an empty landing store
- **THEN** it queries from the 2021-01-01 lower bound and lands every matching publication in the 2021→present window

#### Scenario: Re-run against unchanged upstream is a no-op

- **WHEN** the harvester runs a second time with no new or changed publications upstream
- **THEN** no new artifact is written
- **AND** no existing artifact or provenance record is altered

#### Scenario: Re-run lands only new publications

- **WHEN** new publications have appeared upstream since the last run
- **THEN** the harvester lands only the publications whose identifiers are not already present
- **AND** already-landed publications within the overlap window are skipped

#### Scenario: A changed publication is re-landed

- **WHEN** a previously landed publication's fetched content hash differs from the landed artifact's
- **THEN** the artifact is re-landed with the new bytes
- **AND** a new provenance record is appended, preserving the prior provenance history

### Requirement: SRU plumbing lives in the shared ingest package

The system SHALL place the reusable SRU mechanics in the shared ingest package (`ingest/shared`),
not private to the KOOP source, so future SRU-based sources reuse them. These mechanics MUST cover
`searchRetrieve` URL/query construction, `numberOfRecords` parsing, `startRecord`/`maximumRecords`
paging, per-record verbatim extraction, request rate-limiting, and transient-failure retry with
exponential backoff. Network access MUST be via an injected HTTP getter (as the other ingest sources
do) so the harvest is testable without live network calls.

#### Scenario: SRU paging is exercised through the shared package

- **WHEN** the KOOP harvester pages an SRU result set
- **THEN** it drives the paging through the shared SRU client rather than a KOOP-private implementation

#### Scenario: Harvest runs against an injected getter in tests

- **WHEN** a test exercises the harvester
- **THEN** it supplies a fake HTTP getter returning recorded SRU responses
- **AND** the harvester completes without any live network request

#### Scenario: Transient SRU fetch failures are retried with backoff

- **WHEN** a page fetch fails with a transient network error (connection/handshake timeout, EOF)
- **THEN** the shared SRU client retries the fetch with exponential backoff, up to a bounded number of attempts
- **AND** it fails the run only if every attempt for that page is exhausted

### Requirement: `koop` is a registered pipeline ingest source

The system SHALL register `koop` as a named source in the `pipeline ingest` registry so that
`pipeline ingest koop` runs the harvest and the no-argument run-all path includes it. KOOP requires
no authentication; its HTTP getter SHALL issue plain unauthenticated GET requests.

#### Scenario: `pipeline ingest koop` runs the harvest

- **WHEN** `pipeline ingest koop` is invoked
- **THEN** the KOOP harvester runs against the raw landing store

#### Scenario: `koop` is included when all sources are ingested

- **WHEN** `pipeline ingest` is invoked with no source arguments
- **THEN** `koop` is among the sources ingested

### Requirement: Land the metadata.xml sidecar per publication

For each harvested publication the system SHALL additionally fetch its `metadata.xml` sidecar from
`https://zoek.officielebekendmakingen.nl/<id>/metadata.xml` and land it **verbatim** as its own raw
artifact keyed by the publication id (distinct from the SRU record artifact), together with a
provenance record (source URL, fetch timestamp, byte size, content hash). No field mapping or
reshaping SHALL occur at harvest time — the sidecar's fields (`OVERHEIDop.referentienummer`,
`OVERHEIDop.activiteit`, `OVERHEIDop.gebiedsmarkering`) are parsed by the load stage. The sidecar is
the sole landed source of the zaaknummer, which is absent from the SRU record.

#### Scenario: A publication's metadata sidecar is landed alongside its SRU record

- **WHEN** a publication `gmb-YYYY-NNNN` is harvested
- **THEN** its SRU record and its `metadata.xml` are both landed under the `koop/` prefix keyed by
  that id, each with provenance
- **AND** the metadata bytes are stored verbatim

#### Scenario: The landed metadata carries the referentienummer

- **WHEN** the load stage reads a landed `metadata.xml`
- **THEN** it can extract `OVERHEIDop.referentienummer` (the zaaknummer, whose prefix encodes the
  stadsdeel, e.g. `Z2022-N…` = Noord)

### Requirement: A missing metadata sidecar is non-fatal

If a publication's `metadata.xml` cannot be retrieved (some older ids return none), the system SHALL
still land the SRU record and SHALL record the sidecar's absence, rather than failing the harvest or
dropping the publication.

#### Scenario: Sidecar unavailable

- **WHEN** a publication's `metadata.xml` returns an error or empty body
- **THEN** the SRU record is landed as usual
- **AND** the missing sidecar is recorded, not treated as a fatal error

### Requirement: Metadata landing is incremental and idempotent

A re-run SHALL skip fetching a publication's `metadata.xml` when it is already landed and unchanged,
so a second run over an unchanged corpus performs no redundant sidecar fetches and lands no new
bytes.

#### Scenario: Re-run skips already-landed sidecars

- **WHEN** the harvest runs a second time over an unchanged corpus
- **THEN** no metadata sidecar is re-fetched or re-landed
