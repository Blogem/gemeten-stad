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

**P8 (ontology + SKOS vocab + SHACL shapes) must be DONE before Phase 1 assembles anything to the
graph.** `load/graph` (P12) writes through the SHACL gate and uses the `{| … |}` confidence +
`sh:sparql` presence pattern from `ontology/shapes.ttl`; `load koop` (P13) mints `Intervention` /
`Claim` / `Place` against the P8 TBox. Phase 1 assumes that model exists — it is the last Phase-0
item, not a Phase-1 work item.

## Recommended sequencing

- **Wave A — independent starts (need only P8 + the Phase-0 stores):** P11 `ingest koop` (Go SRU
  harvest) · P12 `load/graph` (the Fuseki writer). No dependency between them.
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

## P12 · `load/graph` — the Fuseki graph writer

- **Goal:** The shared silver-layer graph writer both P13 and P14 write through — the concrete
  realization of the Spike-E patterns against Fuseki.
- **Entails:** write assembled triples into a **run-stamped PROV named graph**
  (`prov:generatedAtTime`, one `prov:Activity` per `load`/`derive` run — transaction time, §3); the
  `{| … |}` annotation form for confidence-bearing edges; a **SHACL gate on write** that rejects
  half-broken instances so "no half-broken data enters the graph" (§4) — using `ontology/shapes.ttl`
  incl. the `sh:sparql` confidence-presence constraint (P8/Spike E). Idempotent upsert semantics
  keyed by stable IRI so re-running `load` is a no-op. Env-driven connection (the compose `fuseki`
  service, `internal/testdb` for the isolated dataset).
- **Key decisions:** update granularity (per-entity named subgraph vs delete-insert by IRI) ·
  whether SHACL validates pre-commit (staging graph) or post-commit-with-rollback · batching.
- **Done when:** a well-formed instance with a confidence-annotated edge lands in a run-stamped named
  graph and passes SHACL; a malformed one is rejected without partial writes; re-writing the same
  IRI is idempotent. Unit tests on triple construction + the `{| … |}` serialization; an integration
  test against the isolated Fuseki dataset (`GS_TEST_FUSEKI_URL`).
- **Depends on:** **P8** (ontology + shapes), P2 (Fuseki), P5 (`internal/testdb`).

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
  correction). Map the besluit fields. **Resolve location via the existing `location/` resolver** —
  the permit's structured point first, the free-text/reference address only as fallback; carry the
  `timeMismatch` / `unresolvedLocation` caveats and the resolution confidence onto the edge; resolve
  at the permit's **valid-time** against BAG voorkomens (§"Temporal model"). Assemble
  `Intervention –locatedAt→ Place` (confidence-annotated edge via P12) `–claims→ Claim`, where the
  Claim is the **herplantplicht triggered by the permit's existence via art. 7** — *no obligation
  count yet* (the count is Phase-2 extraction; §2 says the claim comes from law, not a stated
  ground). The resolved `Place` is a `gs:Place` keyed by its code, carrying its common name
  (`rdfs:label`) and containing area (`gs:within`) from the **gebieden skeleton** — a projection of
  the P7 gebieden tables (codes + names + buurt→wijk `gs:within`; geometry stays in PostGIS),
  seeded once by **P12b** — so resolved places already sit in the aggregation hierarchy (roll-up
  traversal over `gs:within+` is a Phase-3 UI concern). Values + geometry to PostGIS; provenance on every asserted triple. SHACL gate (P12).
  Idempotent by permit IRI.
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

## P14 · `derive` — coverage audit (permit ↔ registry linkage)

- **Goal:** The gold step, **coverage only**: for each loaded permit, does a matching
  `kapenherplant` felling exist? Produce the `AuditLink` with confidence + evidence, or a
  provenanced *"no matching source found"* finding.
- **Entails:** implement **Spike B's place-led confidence model (τ=0.60)** — candidate fellings by
  finest-common place granularity + felling-date window; the registry's own felled **count** and the
  finer BAG place disambiguate the ~3 candidate clusters a buurt+time match alone leaves (Spike B);
  the count used here is the **registry** count, never a permit-text count. Store the `AuditLink`
  with the **granularity used**, the confidence, the evidence it rests on, and
  `prov:wasDerivedFrom` the permit + registry rows (§3 — derived and *stored*, not on-the-fly).
  Anchor any elapsed-time reasoning on `kapmaatregelDatumUitgevoerd` (felling), never the
  batch-assigned permit dates (Spike A). **Bucket matched vs unmatched** and keep pending separate.
  **Explicitly NOT in P14:** the fulfilment estimate (`none`/`partial`/`fulfilled`), timeliness
  beyond `deadlineUnknown`, and the permit-count cross-check — all Phase 2.
- **Key decisions:** the exact candidate-generation query (buurt + window) and the τ=0.60 scoring
  function port from spike-b · `AuditLink` storage shape (graph edge with RDF-star confidence per §3,
  values/geometry to PostGIS) · how the "no source found" finding is represented so the Phase-3 UI
  can render it as first-class.
- **Done when:** `derive` links the Noord corpus at ≈ Spike B rates (permit→registry ~90% place+time)
  with per-link confidence; unmatched permits surface as grounded findings, not silent gaps;
  re-running is a no-op and supersedes prior links by run (PROV). Unit tests on the scoring/candidate
  logic against spike-b's labeled cases; integration test over a seeded permit + registry subset.
- **Depends on:** P13 (permits in graph), P7 (`kapenherplant`/`stamgegevens` in PostGIS), P12 (graph
  writer). Reference: `spikes/spike-b/`, `IMPLEMENTATION_PLAN.md` §4/§8.

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
