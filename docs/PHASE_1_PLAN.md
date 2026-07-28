# Phase 1 plan — the deterministic backbone (no LLM)

Phase 1 turns the Phase-0 foundations into a **working coverage audit from structure alone**: it
harvests the Noord kap/verplant permits, assembles them into the graph with resolved locations and
confidences, and links each permit to its `kapenherplant` registry entry — reporting *"no matching
source found"* as a first-class, provenanced finding where no link clears the threshold. See
`IMPLEMENTATION_PLAN.md` §6 for where Phase 1 sits; this file decomposes it into
independently-tackleable work items, in the same shape as `PHASE_0_PLAN.md`.

Everything here runs against **structured data only** — permit metadata (point geometry, activiteit,
zaaknummer are structured, per Spike B) and the registry. **No NER, no LLM, no permit-text count
extraction** — that is Phase 2, and Phase 1 is deliberately built so the audit works without it.

## What changed from the one-line Phase-1 scope

The `IMPLEMENTATION_PLAN.md` §6 sketch listed `ingest koop` + `load` + **`load bomen`** + `derive`
*(coverage + fulfilment)*. Two corrections, settled while writing this plan:

- **`load bomen` is dropped** — it was already delivered by **P7** in Phase 0 (`load/bomen`, into
  PostGIS). Per §3 the registry is value-store data that never becomes graph entities, so there is
  no bomen-into-graph step by design. Phase 1 only *reads* the P7 tables (in `derive`); at most it
  refreshes them, it does not re-load them.
- **`derive` is coverage-only in Phase 1.** It establishes the permit→registry `AuditLink` (with
  confidence + evidence) and the "no source found" finding. **All fulfilment progress is deferred to
  Phase 2**, where the permit-text obligation count (Spike C extraction) lands — the cross-check that
  makes a progress fraction meaningful. Registry-internal felled-vs-replant counting is *computable*
  from the P7 tables without extraction, but is held back so the fulfilment axes (`IMPLEMENTATION_PLAN.md`
  §"Fulfilment") are built once, coherently, alongside extraction rather than half in each phase.

## Prerequisite — closes in Phase 0

**P8 (ontology + SKOS vocab + SHACL shapes) is DONE** (merged; Phase 0) — Phase 1 assembles against
it. P8 also shipped the `load/graph` **write/validate primitive**
(`Load(ctx, fusekiURL, candidate, Config{Reset})`: SHACL-gate a candidate turtle graph → write it
into a run-stamped PROV named graph on conform), using the `{| … |}` confidence + `sh:sparql`
presence pattern from `ontology/shapes.ttl`. So **P12 is no longer a from-scratch item** — it is a
*finish* of that primitive (the idempotent-upsert layer; see P12). `load koop` (P13) mints
`Intervention` / `Claim` / `Place` against the P8 TBox and writes through that gate.

## Recommended sequencing

- **Wave A — independent starts (need only P8 + the Phase-0 stores):** P11 `ingest koop` (Go SRU
  harvest) · P12 `load/graph` — **finish** the writer (P8 shipped the SHACL-gate + run-stamped-write
  primitive; P12 adds the idempotent-upsert layer). No dependency between them.
- **Wave B — assembly (needs A + `location/`):** P12b seed the gebieden `Place` skeleton (needs P12
  + the P7 gebieden tables), then P13 `load koop` — consumes the harvested permits, the graph writer,
  the resolver, and the seeded Place skeleton.
- **Wave C — the audit (needs the graph populated + P7 tables):** P14 `derive` coverage.
- **Wave D — the gate:** P15 end-to-end thread on the real Noord corpus.

The one hard gate is **P8** (Phase 0) blocking P12/P13. `location/` (P6) and the P7 registry tables
are already DONE, so the resolver and the registry half of `derive` need no new preload work.

---

## P11 · `ingest koop` — scoped SRU harvest (Go)

- **Goal:** Harvest Noord kap/verplant omgevingsvergunningen from KOOP into the raw landing store
  (bronze), incrementally by publication id.
- **Entails:** **reimplement the proven `spikes/spike-b/harvest_permits.py` harvester in Go**
  (decision: §5 is Go-everywhere-except-NLP; Python stays only for spaCy). A scoped SRU query — the
  Noord kap/verplant activiteit filter, 2021 → present, **not** a full bekendmakingen crawl
  (`IMPLEMENTATION_PLAN.md` §5). Land each publication *verbatim* + provenance to the immutable raw
  store, reusing `ingest/shared` (http, SRU, paging, rate-limit, raw landing + provenance) — extend
  it where the geo sources didn't exercise SRU. **Incremental by publication id:** a re-run fetches
  only new/changed publications and is otherwise a no-op. Harvest captures **both** aanvraag and
  besluit publications (they share a zaaknummer) — the aanvraag/besluit dedup is a *load* concern
  (P13), not a harvest filter.
- **Key decisions:** SRU query shape (activiteit + gebied filter that reproduces the spike-b Noord
  count) · incremental cursor (publication-id high-water mark vs date window) · where SRU plumbing
  lives in `ingest/shared`.
- **Done when:** a clean-volume run lands the Noord 2021→present corpus (~600 besluiten + their
  aanvragen, per P10's sizing) with provenance; a second run is a no-op; unit tests cover the query
  builder + incremental cursor + landing/provenance; an integration test lands a recorded SRU
  fixture subset.
- **Depends on:** P1 (skeleton), `ingest/shared`. Reference: `spikes/spike-b/harvest_permits.py`,
  `DATA_SOURCES.md` §1.

## P12 · `load/graph` — finish the Fuseki graph writer (idempotent-upsert layer)

- **Goal:** The shared silver-layer graph writer both P13 and P14 write through. **P8 already
  shipped the core primitive** — `Load(ctx, fusekiURL, candidate, Config{Reset})` in `load/graph/`:
  it SHACL-gates a candidate turtle graph against `ontology/shapes.ttl` (incl. the `sh:sparql`
  confidence-presence constraint) so "no half-broken data enters the graph" (§4), and on conform
  writes it into a **run-stamped PROV named graph** (`run:load-<ts>` + a `prov:Activity` /
  `prov:generatedAtTime` in `run:_provenance` — transaction time, §3), env-driven via
  `shared.FusekiURL`, with integration tests against an isolated Fuseki dataset. What remains for P12
  is the **idempotent-upsert layer** the primitive deliberately left out — it is currently
  append-per-run (each `Load` mints a fresh run graph, additive; only `Config{Reset}` clears).
- **Entails:** add **IRI-keyed idempotent upsert** on top of the shipped primitive so re-running
  `load` writes only *genuinely new or changed* data. **Semantics (SCD2-aware, §3 D4):** entity
  identity is the stable IRI; an entity is *unchanged* if every field matches what is already in the
  graph **except** the valid-time stamp (`validFrom`/`validTo`) on evolving state — an unchanged
  entity is **not** rewritten (a true no-op). When a tracked field changes, the writer **opens** the
  new version and **closes** the prior by stamping its `validTo`, never overwriting history;
  immutable facts (identity + un-stamped facts) are write-once, skipped thereafter. So a
  fully-unchanged re-run writes no new data. **The writer takes turtle bytes it is given** — the
  `{| … |}` confidence-annotation *construction* lives with the callers (P12b, P13, P14), not here;
  `load/graph` stays Postgres-free (doc.go: "candidates arrive already shaped").
- **Settled:** a **no-op run leaves no trace** — a run that writes no new or changed data mints no
  `run:load-<ts>` graph and no `prov:Activity`. PROV records transaction time only for runs that
  actually changed the graph, so re-running is a true no-op end to end (matches P11/P13/P15's "second
  run is a no-op").
- **Key decisions:** change-detection granularity (per-entity subgraph diff vs delete-insert by IRI)
  · batching. (The pre-commit-vs-post-commit SHACL question is already answered by the shipped
  primitive: it validates a merged scratch graph and writes only on conform.)
- **Done when:** re-running `load` on unchanged input is a true no-op (no new triples, no new run
  graph); a changed tracked field opens a new version and closes the prior's `validTo` without
  touching history; a malformed candidate is still rejected without partial writes (regression on the
  shipped gate). Unit tests on the change-detection / upsert logic; an integration test against the
  isolated Fuseki dataset (`GS_TEST_FUSEKI_URL`) covering the no-op re-run + the change-opens-a-new-
  version paths. (Turtle / `{| … |}` construction is tested by its owners — P12b/P13/P14.)
- **Depends on:** **P8** (ontology + shapes + the shipped write/validate primitive), P2 (Fuseki), P5
  (`internal/testdb`).

## P12b · Seed the gebieden `Place` skeleton into the graph

- **Goal:** Project the Amsterdam gebieden hierarchy from PostGIS into the graph as the `gs:Place`
  reference skeleton, so every resolved permit location already sits in a complete
  buurt→wijk hierarchy the audit aggregates over.
- **Entails:** read the loaded `gebieden_buurten` + `gebieden_wijken` tables (P7, PostGIS) and emit
  one `gs:Place` per area — identity = code, `rdfs:label` = naam, `gs:within` = its containing area
  (buurt → wijk via `ligtinwijkid`). Write the skeleton through the P12 graph writer as reference
  data, idempotent by code. **Decision: seed ALL ~630 areas** (518 buurten + 110 wijken), not only
  the places some permit happens to resolve to — so the coverage audit can report *"0 interventions
  in buurt X"* as a first-class, provenanced finding and roll-up covers areas with no activity. The
  **geometry stays in PostGIS**; only code + name + `gs:within` go to the graph. **Owner:** a small
  bridge step that reads PostGIS (like `load geo`/`load bomen`) and hands the projected turtle to the
  `load/graph` writer — `load/graph` itself stays Postgres-free (it writes turtle it is given).
- **Key decisions:** the wijk→stadsdeel level is **deferred** — only buurt + wijk tables are loaded
  (stadsdeel is derivable from the code prefix later; v1 audits a single stadsdeel, Noord) · the
  skeleton is written as reference data and passes SHACL trivially (no shape targets `gs:Place`).
- **Done when:** all ~630 gebieden areas exist as `gs:Place` with a name + `gs:within` parent; a
  `gs:within+` property path rolls a buurt up to its wijk; re-running is a no-op (idempotent by code).
- **Depends on:** P8 (`gs:Place`/`gs:within` model), P12 (graph writer), P7/`load geo` (gebieden in
  PostGIS).

## P13 · `load koop` — assemble permits to graph + PostGIS

- **Goal:** The silver step for permits: map → dedup → resolve location → assemble the
  fully-formed `Intervention` + `Claim` into the graph, values/geometry into PostGIS.
- **Entails:** per zaaknummer, **dedup aanvraag+besluit and audit the besluit** (Spike B
  correction). Map the besluit fields. **Set `gs:activity` to the felling concept** (verplanten ≡
  vellen; Spike C) — activiteit is *structured* permit metadata (Spike B), so asserting the concept
  edge is cheap, is SHACL-gated to the vocab, and already feeds the Spike-B / Phase-2 matcher; assert
  the same for any other structured besluit field that maps to a vocab concept. **Resolve location
  via the existing `location/` resolver** — the permit's structured point first, the
  free-text/reference address only as fallback; carry the `timeMismatch` / `unresolvedLocation`
  caveats and the resolution confidence onto the edge; resolve at the permit's **valid-time** against
  BAG voorkomens (§"Temporal model"). Assemble `Intervention –locatedAt→ Place` — **P13 builds the
  `{| … |}` confidence-annotated `locatedAt` turtle** (construction lives here, not in the writer),
  which P12 gates + writes — `–claims→ Claim`, where the
  Claim is the **herplantplicht triggered by the permit's existence via art. 7** — *no obligation
  count yet* (the count is Phase-2 extraction; §2 says the claim comes from law, not a stated
  ground). The resolved `Place` is a `gs:Place` keyed by its code, carrying its common name
  (`rdfs:label`) and containing area (`gs:within`) from the **gebieden skeleton** — a projection of
  the P7 gebieden tables (codes + names + buurt→wijk `gs:within`; geometry stays in PostGIS),
  seeded once by **P12b** — so resolved places already sit in the aggregation hierarchy (roll-up
  traversal over `gs:within+` is a Phase-3 UI concern). Values + geometry to PostGIS; provenance on every asserted triple. SHACL gate (P12).
  Idempotent by permit IRI under the P12 SCD2 upsert semantics — an unchanged permit re-loads as a
  no-op; a changed field opens a new version and closes the prior's `validTo`, never overwriting.
- **Key decisions:** the `Claim` shape without a count (obligation-exists vs obligation-of-N) · how
  `unresolvedLocation` permits are represented (written with the marker + confidence, never as if
  exact — §4) · besluit field → ontology property mapping.
- **Done when:** the Noord corpus loads into graph + PostGIS with resolved places and confidences;
  aanvraag/besluit pairs collapse to one audited besluit; unresolvable addresses carry the marker,
  not fake coordinates; re-running is a no-op. Unit tests on mapping/dedup/claim-assembly;
  integration test loads a fixture permit set through the real resolver + graph writer.
- **Depends on:** P11 (permits landed), P12 (graph writer), P6 (`location/`), P8 (model), P7
  (BAG/gebieden in PostGIS, for resolution). Reference: `IMPLEMENTATION_PLAN.md` §4,
  `DATA_SOURCES.md` §1/§8.

## P14 · `derive` — coverage audit (permit ↔ registry linkage) · **DONE**

**Landed as:** `derive/coverage` (change `derive-coverage-audit`, wired as `pipeline derive`,
replacing the stub). Coverage is recorded as a stable per-permit `gs:AuditLink` **anchor**
(`gs:coversIntervention`) plus versioned `gs:CoveragePeriod` nodes `gs:versionOf` the anchor,
content-keyed so an unchanged re-run is a true no-op. The matching unit is the **individual
felling** (`kapenherplant`), windowed against the besluit's **publication date** — now carried in
the graph as `dct:available` on the `Intervention` — over `[publication, +3yr]`; each felling is
assigned to **at most one** permit via exclusive greedy best-score assignment (Spike B's
place-led model: proximity + registry count + time + ambiguity, τ=0.60). A period is either
**matched** (`gs:linksObservation` → the permit's `Observation`, `gs:confidence`,
`gs:granularity`, `gs:caveat gs:weakLink` below τ) or **no-source** (`gs:noSourceFound true`).
Derived numbers (matched flag, assigned felling ids/count, candidate count, nearest distance, run
id) persist to a slim PostGIS `audit_metrics` table — the link itself lives only in the graph. See
`openspec/changes/derive-coverage-audit/design.md` D1–D10.

- **Goal:** The gold step, **coverage only**: for each loaded permit, does a matching
  `kapenherplant` felling exist? Produce the `AuditLink` with confidence + evidence, or a
  provenanced *"no matching source found"* finding.
- **Entails:** implement Spike B's place-led confidence model (τ=0.60) over per-felling candidates
  scoped by buurt + the `[publication,+3yr]` window; score on proximity (address/postcode/buurt
  tier → `gs:granularity`), the **registry**'s own felled count (never a permit-text count), time,
  and ambiguity; assign each felling exclusively to its single best-scoring permit. The
  `gs:AuditLink` modeling prerequisite carried over from P8 landed with this change: TBox
  `gs:coversIntervention`/`gs:CoveragePeriod`/`gs:linksObservation`/`gs:granularity`/
  `gs:noSourceFound` (reusing `gs:versionOf` from `state-node-versioning`) + core-SHACL
  `AuditLinkShape`/`CoveragePeriodShape`. Anchors elapsed-time reasoning on
  `kapmaatregelDatumUitgevoerd` (felling) and the besluit's `dct:available` publication date
  (Spike A), never the batch-assigned permit dates. **Bucket matched vs no-source**; fulfilment
  and timeliness beyond this stay out of scope for Phase 1.
- **Done when:** `derive` links the Noord corpus at ≈ Spike B rates (permit→registry ~90%
  place+time) with per-link confidence; unmatched permits surface as grounded `noSourceFound`
  findings, not silent gaps; re-running is a no-op on unchanged periods and, when an outcome
  changes, opens a new period version and closes the prior's `gs:validTo`, each run stamped by
  PROV; a period missing its confidence/evidence/outcome shape is rejected by
  `CoveragePeriodShape` (not written). Unit tests on the scoring/candidate/assignment logic
  against Spike B's labeled cases; integration test over a seeded permit + registry subset (incl.
  a malformed-period reject case).
- **Depends on:** P13 (permits in graph, incl. `dct:available`), P7 (`kapenherplant`/`stamgegevens`
  in PostGIS), P12 (graph writer), `state-node-versioning` (node-form period versioning).
  Reference: `spikes/spike-b/`, `IMPLEMENTATION_PLAN.md` §4,
  `openspec/changes/derive-coverage-audit/design.md`.

### Results — measured on first full run

Populated from a real `pipeline derive` run over the Noord corpus and compared against the Spike B
calibration; cells are placeholders until that run lands.

| Resolution tier | Permits | Matched % | Median confidence |
|---|---|---|---|
| `address` | TBD | TBD | TBD |
| `postcode` | TBD | TBD | TBD |
| `buurt` | TBD | TBD | TBD |

### Verified coverage numbers (post `model-felled-trees`)

A full dev-store pipeline run (`geo` → `graph` → `koop` → `bomen` → `derive`, Noord corpus) confirms
the coverage audit reconciles end to end: of 659 permit anchors, **281 matched** and **378
no-source** (281 + 378 = 659), and the open matched `gs:CoveragePeriod` count in the graph agrees
exactly with the matched row count in `audit_metrics`. Anchors with more than one open period: **0**
— the "exactly one open period per anchor" invariant holds. This resolves the earlier 340-vs-281
discrepancy, which traced to duplicate open periods minted under the pre-change content-key;
content-addressing the matched `gs:Observation` by its assigned felling set (`model-felled-trees`,
§3) fixed it at the root rather than papering over it downstream. The bomen→graph registry
projection loaded **8,195 `gs:Felling`** and **8,193 `gs:Tree`** nodes, all with canonical integer
IRIs (a load-time `json.Number` fix ensures `data:felling/<id>` carries the exact integer, e.g.
`felling/4301189`, never scientific notation).

## P15 · End-to-end thread on the real Noord corpus — the acceptance gate

- **Goal:** Prove the deterministic backbone runs end to end and produces the intended "working
  audit from structure alone, with confidences."
- **Entails:** run `ingest koop → load koop → derive` on the real Noord 2021→present corpus; confirm
  the pipeline reproduces the mechanics of the `DATA_THREAD_TREES.md` worked example (permit →
  resolved place → matched registry felling → stored `AuditLink` with confidence), only with the
  Noord place filter; spot-check that unmatched permits read as provenanced "no source found"; verify
  every stage is idempotent (re-run = no-op). Document the observed link rates + confidence
  distribution against Spike B's predictions.
- **Done when:** the three stages run clean on a fresh volume; the audit output is inspectable
  (SPARQL over the graph + SQL over PostGIS) and matches Spike B rates; the thread is written up as a
  short Noord counterpart to `DATA_THREAD_TREES.md`.
- **Depends on:** P11–P14.

## Explicitly deferred to Phase 2 (so Phase 1 stays honest)

- **Permit-text count extraction** (the three-tier `msr-graph` extractor) — the obligation count.
- **Fulfilment estimate** (`none`/`partial(fraction)`/`fulfilled`) and the registry-vs-permit
  cross-check — needs the count above to be meaningful.
- **Timeliness beyond `deadlineUnknown`** — Spike A settled that the termijn is Phase-2 NER at best.
- **Species / project extraction** — the fuzzy residue that doubles as the NER recognition vocab.
