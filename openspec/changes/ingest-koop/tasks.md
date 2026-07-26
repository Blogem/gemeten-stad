## 1. Shared SRU plumbing (`ingest/shared`)

- [x] 1.1 Add an SRU `searchRetrieve` URL/query builder (`operation=searchRetrieve`, `version=2.0`, url-encoded `query`, `startRecord`, `maximumRecords`) as a pure function.
- [x] 1.2 Add an SRU response parser using `encoding/xml`: read `numberOfRecords` and split `<record>` elements, capturing each record's verbatim inner XML plus the parsed `dcterms:identifier` and `dt.available`.
- [x] 1.3 Add a paged SRU fetch that iterates `startRecord` in `maximumRecords` steps until `numberOfRecords` is exhausted, driven by an injected `httpGet`.
- [x] 1.4 Add a modest request rate limiter (fixed inter-request interval, named constant) applied across sequential SRU pages.

## 2. KOOP harvester (`ingest/koop`)

- [x] 2.1 Define the scoped Amsterdam kap/verplant SRU query (D1: `dt.creator any "Amsterdam"` + `dt.type any "omgevingsvergunning"` + kap/verplant `cql.textAndIndexes any` term set + `dt.available>=`), with the date lower bound as a parameter.
- [x] 2.2 Implement the incremental cursor: read the `dt.available` high-water mark (default 2021-01-01), query from `highWater − overlap`, and persist the new high-water mark after the run (cursor sidecar under `koop/`).
- [x] 2.3 Implement per-publication landing: for each record, skip if the id is already landed and unchanged; land verbatim via `shared.RawStore.Land` keyed by `dcterms:identifier` under a `koop/` prefix, with provenance (source SRU URL, fetch time, size, hash).
- [x] 2.4 Implement changed-publication handling (D4): re-land when the fetched content hash differs from the landed artifact, appending provenance.
- [x] 2.5 Wire `Ingest(ctx, store, httpGet)` as the package entry point, rejecting a nil `httpGet`.

## 3. Pipeline wiring (`cmd/pipeline`)

- [x] 3.1 Add `koopHTTPGet` (plain unauthenticated GET) and register `koop` in `ingestRegistry`.
- [x] 3.2 Update `TestSelectSources` / `TestSelectSources_RegistryShape` to include `koop` in the expected source names.

## 4. Tests

- [x] 4.1 Unit-test the SRU query builder — asserts the scoped clauses are present and no Noord/geometry/postcode filter is added (spec: query builder scenario).
- [x] 4.2 Unit-test the SRU paging over a fake `httpGet` returning a multi-page recorded response — asserts every record across pages is yielded (spec: paged to exhaustion).
- [x] 4.3 Unit-test the incremental cursor: high-water-mark advance, overlap re-query, and id-dedup skip (spec: re-run lands only new publications; no-op re-run).
- [x] 4.4 Unit-test landing + provenance: verbatim bytes keyed by id, provenance fields recorded, and changed-content re-land appends provenance (spec: land verbatim; changed publication re-landed).
- [x] 4.5 Integration test: land a recorded SRU fixture subset end to end through `Ingest` with a fake getter — asserts both aanvraag and besluit land, a second run is a no-op, and a clause-by-clause `numberOfRecords` sanity check (guards the §1 silent-zero quirk).

## 5. Validation

- [ ] 5.1 `go build ./...` and `go vet ./...` pass.
- [ ] 5.2 `go test ./ingest/koop/... ./ingest/shared/... ./cmd/pipeline/...` pass.
- [ ] 5.3 `openspec validate ingest-koop --strict` passes.
