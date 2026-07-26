## ADDED Requirements

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
