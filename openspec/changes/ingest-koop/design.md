## Context

KOOP (`repository.overheid.nl/sru`) is the legally mandated publication channel for every
municipal decision, including kap/verplant omgevingsvergunningen — the intervention documents
the tree audit rests on (`docs/DATA_SOURCES.md` §1). Nothing from this stream has been landed;
P11 is the first harvest of it.

The harvest is already proven in `spikes/spike-b/harvest_permits.py`: a scoped SRU query
(`dt.creator any "Amsterdam"` + `dt.type any "omgevingsvergunning"` + a kap/verplant full-text
term set + a `dt.available` window), paged at `maximumRecords=100`, reading the Phase-1 structured
signals **straight from each SRU record** — point geometry (RD + WGS84), controlled `activiteit`,
zaaknummer (`OVERHEIDop.referentienummer`), title, and the count-bearing abstract, all present
~99% of the time. This change reimplements that in Go (§5: Go-everywhere-except-NLP) against the
existing `ingest/shared` landing/provenance plumbing, extending it with the SRU paging the geo
sources never exercised.

Existing plumbing this builds on:
- `shared.RawStore.Land(name, r, sourceURL, fetchedAt)` — writes bytes verbatim + a
  history-preserving provenance sidecar; `Landed(name)` reports presence.
- The `pipeline ingest [source ...]` registry in `cmd/pipeline/main.go`, with per-source injected
  `httpGet` functions (BAG/gebieden/bomen pattern).

## Goals / Non-Goals

**Goals:**
- Land every Amsterdam kap/verplant omgevingsvergunning publication (2021 → present) verbatim,
  one raw artifact per publication id, with a provenance record.
- Incremental re-runs: fetch only new/changed publications; an unchanged re-run is a no-op.
- Add reusable SRU plumbing to `ingest/shared` (paged `searchRetrieve`, record splitting,
  `numberOfRecords` parsing, rate-limiting).
- Unit-test the query builder + incremental cursor + landing/provenance; integration-test a
  recorded SRU fixture subset end to end.

**Non-Goals:**
- **No Noord filter, no location resolution, no geometry parsing at harvest** — the harvest is
  Amsterdam-wide; Noord selection and BAG resolution are P13's job.
- **No aanvraag/besluit dedup** — both are landed; dedup by zaaknummer is P13.
- **No field mapping, no count extraction, no NER** — the raw record is landed as-is; parsing is
  P13 (structured fields) / Phase 2 (count, NER).
- **No per-publication `metadata.xml` fetch** — every Phase-1 signal is in the SRU record itself
  (§1), so a second fetch per publication would be waste.

## Decisions

### D1 — SRU query shape (Amsterdam-wide kap/verplant, 2021 → present)

Query: `(dt.creator any "Amsterdam" AND dt.type any "omgevingsvergunning" AND
cql.textAndIndexes any "kappen vellen rooien verplanten houtopstand" AND dt.available>="2021-01-01")`.
This is the spike-b query with the year *upper* bound dropped (2021 → present) and no Noord clause.
The publication-date lower bound advances on incremental runs (see D3).

*Rationale:* reproduces the proven spike-b corpus shape. `dt.creator any "Amsterdam"` is the
publishing authority (355k docs) — **not** the sparse `w.gemeentenaam`; `any` relations, not `==`
(§1 quirks). The kap/verplant activity IS filtered at ingest — via the full-text synonym set
`cql.textAndIndexes any "kappen vellen rooien verplanten houtopstand"`, which includes `kappen`
alongside its synonyms (`vellen`/`rooien`/`verplanten`/`houtopstand`). It is a **full-text** filter,
not a filter on the controlled `OVERHEIDop.activiteit` field: SRU does not expose an `activiteit`
index for omgevingsvergunningen (the controlled `w.*` indexes are verkeersbesluit-specific, §1), so
the controlled activiteit is not *queryable* at harvest. It is, however, **present in every SRU
record returned** (the `OVERHEIDop.activiteit` element — spike-b lifts it per-record with a regex,
`tag(rec, "activiteit")`), so landing each record verbatim preserves it for P13 to read downstream
(to confirm kap vs. an incidental full-text mention, split vellen/verplant, etc.). Full text is also required because title phrasing is per-gemeente house style and
`w.postcode` is empty for omgevingsvergunningen (§1). `verplanten` is deliberately kept in the set,
not dropped: the Bomenverordening defines *vellen* to include *verplanten*, so it triggers
herplantplicht identically and counts toward the felling obligation (§1, Spike C). Because an
unknown index or no match returns **0 records silently, never an error** (§1 quirk), the query
builder is validated by a clause-by-clause `numberOfRecords` sanity check in the integration test
rather than trusting a single combined count.

*Alternatives considered:* (a) Noord-scoped SRU query — rejected: Noord isn't reliably expressible
in SRU (`w.gemeentenaam`/`w.postcode` sparse/empty), and per the proposal's settled decision Noord
selection is deferred to P13. (b) Title-only search — rejected: house-style phrasing misses hits
(§1). (c) Include bouw omgevingsvergunningen — out of scope for the tree audit.

### D2 — Land verbatim, one raw artifact per publication id

Each SRU record is landed via `RawStore.Land`, keyed by its `dcterms:identifier` (`gmb-YYYY-NNNNNN`)
under a `koop/` prefix (e.g. `koop/gmb-2022-291126.xml`), with the record bytes written verbatim and
a provenance sidecar (source SRU URL, fetch time, size, hash).

*Rationale:* "land each publication verbatim" (P11) maps directly onto one-file-per-id, and that
granularity makes incremental dedup fall out for free — `store.Landed(id)` is the skip check, and a
re-run touches only new ids. This differs from `bomen` (one accumulated versioned blob) because
KOOP is an ever-growing stream of independent, individually-addressable publications, not a single
dataset re-paged in full.

*Alternatives considered:* (a) One accumulated JSONL/versioned blob like `bomen` — rejected:
defeats per-id incrementality and forces a full rewrite per run. (b) `LandVersion` per id — rejected:
publications are effectively immutable once published; the rare correction is handled by re-landing
(see D4), and version history per publication is not a Phase-1 need.

### D3 — Incremental cursor: `dt.available` high-water mark + id dedup + overlap

SRU can filter by publication **date** (`dt.available>=`), **not** by identifier, so the cursor is
a `dt.available` high-water mark, while **idempotency/dedup is by publication id**. Each run:
1. reads the stored high-water mark (max `dt.available` landed so far), defaulting to `2021-01-01`;
2. queries `dt.available >= (highWater − overlap)` — a small overlap window (e.g. a few days) so
   same-day and late-arriving publications back-dated before the last cursor are not missed;
3. for each returned record, **skips** ids already landed (`store.Landed(id)`) and lands the rest;
4. persists the new high-water mark (max `dt.available` seen this run).

*Rationale:* the date window is the only server-side incremental filter SRU offers; the id-dedup
skip makes the overlap re-query cheap and the whole run idempotent. A first run on a clean volume
walks the full 2021→present window; a second run with no new upstream publications lands nothing.

*Alternatives considered:* (a) Pure publication-id high-water mark — rejected: not queryable (SRU
has no `identifier >` operator) and gmb ids are a national, non-Amsterdam-only counter, so
"greater id" is not a sound Amsterdam cursor. (b) Full re-harvest every run relying only on
`Landed` skips — works but re-pages the entire corpus each time; the date cursor bounds the query
to the tail. (c) No overlap — rejected: risks missing publications whose `dt.available` is
back-dated just before the cursor.

### D4 — Changed-publication handling

The rare case where a publication's content changes after first landing (a correction) is handled
by comparing the freshly fetched record's content hash against the landed artifact's; on a
mismatch the artifact is re-landed (bytes overwritten, provenance **appended** — `Land` is
history-preserving on provenance). Unchanged content within the overlap window is skipped without a
rewrite.

*Rationale:* keeps "fetches only new/**changed** publications" (P11) honest without adding
per-publication versioning. Provenance retains the full fetch history even though the artifact
holds the latest bytes.

### D5 — SRU plumbing in `ingest/shared`

Add an SRU capability to `ingest/shared`, mirroring the injected-`httpGet` shape of the existing
sources:
- a `searchRetrieve` URL/query builder (`operation`, `version=2.0`, `startRecord`,
  `maximumRecords`, url-encoded `query`);
- a paged fetch that reads `numberOfRecords`, iterates `startRecord` in `maximumRecords` steps
  until exhausted, and yields each `<record>`'s **verbatim inner XML** plus the parsed fields the
  cursor needs (the publication identifier and publication date);
- a modest **rate limiter** (fixed inter-request interval) so sequential paging stays polite;
- **transient-failure retry with exponential backoff** around each page fetch (bounded attempts,
  capped per-attempt delay, named constants), because the KOOP endpoint intermittently drops
  connections mid-harvest (connection/handshake timeouts, EOF, dial i/o timeouts) — a single
  transient failure must not abort a 100+ page run. Each retry is **logged** (attempt number,
  backoff delay, startRecord, error) so a flaky harvest is observable rather than silent.

Parsing uses `encoding/xml` for the envelope (`numberOfRecords`, records) capturing each record's
`,innerxml` for verbatim landing, plus the two cursor fields — not regex (the spike used stdlib
regex; Go's `encoding/xml` is the idiomatic, robust choice here).

**Response element names (vs. query index names).** The cursor fields are read by XML *local
element name*: the identifier from `dcterms:identifier` (local name `identifier`) and the
publication date from **`dcterms:available`** (local name `available`). Note the query filters on
the CQL *index* `dt.available>=`, but the returned record carries the value in `dcterms:available`,
not a `dt.available` element — the two names must not be conflated (verified against the live KOOP
corpus: every record uses `dcterms:available`).

*Rationale:* the plan explicitly says to reuse `ingest/shared` and "extend it where the geo sources
didn't exercise SRU". SRU paging (`numberOfRecords` + `startRecord`) is a distinct shape from the
HAL `_links.next` (bomen) and atom-feed (bag) paging already present, and a future verkeersbesluit
harvest will reuse it.

*Alternatives considered:* SRU logic private to `ingest/koop` — rejected: the plan mandates shared
plumbing and other KOOP sources are coming.

### D6 — Registry wiring

Register `koop` in `ingestRegistry` (`cmd/pipeline/main.go`) with a `koopHTTPGet` (plain
unauthenticated GET — KOOP needs no auth), so `pipeline ingest koop` and the run-all path drive it.
Update `TestSelectSources`/`TestSelectSources_RegistryShape` to include `koop` in the expected
name set.

## Risks / Trade-offs

- **Silent-zero SRU queries** (an index typo or over-narrow clause returns 0 records, not an
  error, §1) → the integration test asserts a clause-by-clause `numberOfRecords` breakdown against
  a recorded fixture, and the harvester logs the total `numberOfRecords` per run so a sudden drop
  to 0 is visible.
- **Overlap window too small** → a late back-dated publication slips past the cursor. Mitigation: a
  conservative overlap (days, not hours) is cheap because id-dedup skips already-landed records; the
  window is a named constant, easily widened.
- **SRU record omits a signal P13 needs** (e.g. a publication missing `referentienummer`) → not a
  harvest failure: the record is landed verbatim regardless, and P13 handles missing fields with
  its caveats (`unresolvedLocation` etc.). Harvest never drops a record for missing structured
  fields.
- **Amsterdam-wide corpus larger than the Noord subset** (~2–4k publications vs ~600 Noord) → more
  raw artifacts landed than P13 will use; accepted per the settled Noord-deferral decision, and
  bounded by the per-id landing (small XML files) and the date-cursor tail on re-runs.
- **Endpoint politeness/throttling** → the shared rate limiter caps request rate; paging is
  sequential.
- **Endpoint instability mid-harvest** (observed on the live corpus: the ~106-page full run
  intermittently fails with connection/handshake timeouts, EOF, or dial i/o timeouts on a random
  page) → the shared SRU fetch retries each page with bounded exponential backoff, capping the
  per-attempt delay, and logs every retry (attempt, delay, startRecord, error) so the flakiness is
  observable. A run still aborts if a single page exhausts all retry attempts; because landing is
  id-idempotent, simply re-running resumes (already landed ids are skipped). Retry attempt count,
  base delay, and the per-attempt cap are named constants (defaults: 8 attempts, 1s base doubling to
  a 30s cap), tunable in P15.

## Migration Plan

Additive: a new source and new shared plumbing, no changes to existing landed data or other
sources. Rollback = drop the `koop/` raw artifacts and revert the registry entry. First
production run walks the full 2021→present window against a clean volume (P15 exercises this on the
real corpus).

## Resolved Questions

Settled (previously open):

- **Overlap window / rate-limit interval** — conservative named-constant defaults: a **7-day**
  `dt.available` re-query overlap (cheap because id-dedup skips already-landed records) and a modest
  inter-request interval (~200 ms, ≈5 req/s) for polite sequential paging. Both are named constants,
  tunable against the real endpoint in P15.
- **Cursor persistence** — the `dt.available` high-water mark persists as a small **cursor sidecar
  under `koop/`** in the raw store (not derived by rescanning landed provenance on every run).
