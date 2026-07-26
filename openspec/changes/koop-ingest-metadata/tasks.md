## 1. Fetch + land the metadata sidecar

- [x] 1.1 Add a `metadata.xml` fetch to the koop harvest loop (`ingest/koop`): build the sidecar URL
      `https://zoek.officielebekendmakingen.nl/<id>/metadata.xml`, fetch via the shared http client,
      and `Land` it verbatim as `koop/<id>.metadata.xml` with provenance, keyed by publication id.
- [x] 1.2 Gate the fetch on need: skip when the sidecar is already landed and the SRU record is
      unchanged (reuse the sha256 content-unchanged skip), so an unchanged re-run does no redundant
      sidecar fetches.
- [x] 1.3 Make a missing/failed sidecar non-fatal: land the SRU record regardless and record the
      absence (provenance note or empty-marker), never dropping the publication.
- [x] 1.4 Rate-limit + retry the sidecar fetch (D5): pace at `metadataRateInterval`, retry transient
      transport errors with capped backoff (`metadataMaxAttempts`), and distinguish a genuine 404
      (`shared.ErrNotFound`, skip) from a transient reset (retry; leave unlanded for a later run).

## 2. Tests

- [x] 2.1 Unit test the sidecar URL builder and the naming/keying convention (`<id>.metadata.xml`).
- [x] 2.2 Unit/contract test: a recorded `metadata.xml` fixture lands verbatim with provenance; a
      second run skips it (idempotent); a 404/empty sidecar lands the SRU record and records the
      absence without error.
- [x] 2.3 Add a realistic `metadata.xml` fixture carrying `OVERHEIDop.referentienummer` (Noord
      `Z….-N…`), `OVERHEIDop.activiteit`, and `OVERHEIDop.gebiedsmarkering`, shared with the P13
      parser tests.

## 3. Docs

- [x] 3.1 Update `ingest/koop/doc.go` and `docs/DATA_SOURCES.md` §1 to note the sidecar is landed and
      is the authoritative zaaknummer source (correcting the older "thin metadata" note).
