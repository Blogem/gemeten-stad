# Implementation plan — vertical 1: the Amsterdam tree lifecycle, stadsdeel Noord

The first vertical of De Gemeten Stad (see `VISION.md` for the overarching idea;
`DATA_SOURCES.md` for the source catalog; `DATA_THREAD_TREES.md` for a live worked example).
Early phases are design-heavy — they settle the architecture and resolve the open questions
that decide whether the audit is sound before build-out.

## 1. Scope

- **Domain:** the statutory tree replant obligation (Bomenverordening 2014 art. 7 —
  herplantplicht "in beginsel altijd"; termijn per permit by the college).
- **Place:** stadsdeel **Noord** (`gebieden` id `03630000000019`, code `N`).
- **Timeframe:** kap/verplant permits over a **rolling backdated window — Noord publications
  from 2021 onward, with no recent-end cutoff.** The floor is data-driven, not arbitrary: the KOOP
  omgevingsvergunning kap stream for Amsterdam is empty in 2020 and begins in 2021 (855 citywide
  publications, vs 2,299 in 2022), and the `kapenherplant` registry's earliest Noord felling is
  **2021-02-10** — so permits older than 2021 have nothing to audit against. There is deliberately
  **no upper cutoff:** a permit whose replant window has not yet elapsed yields a *pending /
  indeterminate* verdict (a required first-class output — see "The test" below and §2), not noise —
  so ingesting the recent tail **is** the production behaviour, with no dev-only clipping to unwind.
  Observability is a **per-record** property (does a matched felling exist, and has its replant
  window elapsed?), read off the felling date — never a corpus-wide date filter. Phase 0 (P10)
  sizes the corpus; it is tiny at every horizon (Noord besluiten per settled year: 2021 · 38,
  2022 · 147, 2023 · 169).
- **The test:** harvest those permits, extract what was felled/moved (counts, species,
  project, location, any stated termijn), resolve their locations, and measure how much maps
  onto the `kapenherplant` registry — then compute replant progress, **with explicit
  confidence and indeterminate verdicts where data is missing.**

## 2. The audit thesis, corrected by the data

- **For trees, the claim comes from LAW, not from a stated ground.** The permit rarely says
  *why* (verified: `gmb-2022-203707` is 817 chars — *"het verplanten van 18 bomen binnen het
  projectgebied E-Buurt Oost NZ"* and nothing about the reason). The claim being audited —
  herplantplicht — is triggered by the permit's *existence*, via art. 7. So **ground/why
  extraction is NOT on the critical path** for this vertical. NER's real job is the
  quantities that size and locate the obligation.
- **The obligation is a computed quantity, not 1:1.** Diameter-class equivalence (a mature
  tree → several young ones); replant progress can be *partial*.
- **Herplantfonds discharge is legitimate and not publicly observable.** "No replant at
  location" is not automatically a violation → fulfilment tops out at *indeterminate* for
  fund-eligible cases. (Getting the fund balance is a Phase-4 WOO request.)
- **No shared key ties permits to registry rows** → linkage is fuzzy entity resolution on
  place + counts + time + project, carrying a confidence.
- The audit is a **triangle**: permit (external existence) + `kapenherplant` (obligation +
  fulfilment) + bomenboekhouding (aggregate self-report), with the law as external norm.

## 3. Storage (hybrid + a derived layer)

```
 GRAPH (RDF, thin)                          VALUE STORE (Postgres + PostGIS)
 ────────────────                           ────────────────────────────────
 Intervention ─locatedAt→ Place             kapenherplant rows, stamgegevens
    │ ├ partOfProject→ Project (OPTIONAL)    BAG addresses/buildings (preloaded)
    │ └ claims→ Claim                        gebieden + CBS polygons (preloaded)
    ▼                                        all geometry (PostGIS)
 Claim ─testedAgainst→ Observation ─locator→ SQL query
 DERIVED: AuditLink (Permit↔trees) + computed progress  ─── stored, not on-the-fly
 RDF-star on the fuzzy edges: << Intervention locatedAt Place >> confidence 0.7 ; evidence …
 PROV on every asserted triple: ← document span OR connector query OR derivation
```

- **`Project` is an optional, sparse relation** — present for renewal projects, absent for a
  one-off permit. This is exactly why the graph is worth it: we model `partOfProject` where
  it applies without forcing it everywhere, and other intervention types get *their* own
  edges. A rigid relational schema would need a nullable column or a join table for a
  relation most rows don't have.
- **`Place` is a first-class entity carrying a name + a containment skeleton — geometry stays
  in PostGIS.** A `Place`'s identity is its code (BAG object id / gebieden code); the graph
  also carries its common name (`rdfs:label`) and its containing area (`gs:within`, transitive),
  forming the buurt→wijk→stadsdeel skeleton. That split follows the hybrid rule precisely: the
  graph holds the *joinable skeleton* (identity + label + hierarchy), so rolling obligations and
  assessments up aggregation levels (tree → buurt → wijk → stadsdeel) is a graph traversal (a
  `gs:within+` property path), not a cross-store join; the *geometry* — the heavy spatial payload
  — stays in PostGIS, where point-in-polygon resolution and containment tests run. The skeleton is
  a **projection of the authoritative gebieden tables** (seeded by the load stage), so PostGIS
  stays the single source of truth and the graph copy cannot drift. Places are deliberately **not**
  SKOS vocab concepts — a Place is recognized by geometry, not by name, so it needs no NER/grounding
  entry; the analytics agent additionally gets direct value-store (PostGIS) access for spatial
  questions the graph should not answer.
- **Uncertainty is first-class (RDF-star).** The location-resolution edge and the
  permit↔trees audit edge carry a confidence + the evidence they rest on, so the UI can show
  *where we are not sure and how sure we are*. This is a requirement, not a nicety.
- **The audit is derived and stored.** Once both sources are loaded, a derivation pass
  computes the permit↔trees link and the replant-progress fraction and **stores** them
  (`prov:wasDerivedFrom` the permit + registry rows). Precomputed because we view at multiple
  aggregation levels (tree → buurt → stadsdeel) and recomputing per query would be slow. So
  we persist our *own* computed numbers, not just mapped source data.

### Temporal model (generic) — stamp what evolves, not what's fixed

Assessments are not the only thing that changes: a **permit can be repealed or amended**, a
registry row gets its replant date filled in later, a fuzzy location link can be revised. So
validity-over-time is a *cross-cutting* concern, handled by one rule rather than case by case.

**The rule: separate stable identity from time-varying state, and stamp validity only on the
state.**
- **Identity is timeless.** A permit, tree, place, project, claim each has one stable IRI that
  never changes and is never deleted — the anchor everything hangs off.
- **Immutable facts stay un-stamped.** "This permit was published on 2022-05-05", "this tree
  was felled on 2024-01-25" are timeless facts; recording *when it happened* is not the fact
  *evolving*. Do not temporalize these — over-stamping is a real anti-pattern (every query
  then drags time filters for no gain). This is the lean rule applied to time.
- **Evolving state carries valid time.** Legal force (in-force → repealed), fulfilment
  assessments, and other genuinely time-bounded relationships → `validFrom`/`validTo`. A
  resolved-location link is *not* one of these (see below) — refining its confidence is a
  correction, not a change in the world.

**Two mechanisms, chosen by shape (not one-size-fits-all) — see `docs/RDF_STAR_RELATIONSHIPS.md`
for the authoritative decision guide:**
- **RDF-star statement annotation** carries *refinable metadata* — confidence, evidence, a
  caveat — about a fact that itself holds timelessly. It is **transaction-time only** (the
  latest run's annotation is current) and **never carries valid time**. This is the location
  link's mechanism: `gs:locatedAt` is annotated with `gs:confidence`/`gs:caveat` only, no
  `validFrom`/`validTo` — resolving it better later is a correction, not a new world-state.
- **A state/period node** (the n-ary / "fluent" pattern) carries **valid time** when the
  relationship's existence or state itself changes over the world — e.g. legal force
  (in-force → repealed) or a fulfilment assessment. Each period is its own node, grouped by a
  stable `gs:versionOf` series anchor; the writer keeps exactly one **open** period per anchor,
  closing the prior when a new one appears — the `Assessment` below, a permit's
  `LegalStatusPeriod`, and `gs:AuditLink`'s coverage periods are all instances of this. This is
  the KG form of the data-warehouse **SCD Type 2** pattern.

**Two time dimensions — and one is free.** Valid time (world) comes from the mechanisms above.
**Transaction time (when *we* recorded it) is exactly PROV-O**, which we already commit to:
every `load`/`derive` run is a `prov:Activity` writing into a **run-stamped named graph** with
`prov:generatedAtTime`, so "what did we know, and when" is answerable per run with no extra
machinery — provenance and transaction-time are the same layer. In the Postgres value store
the same idea is native: SCD-Type-2 `valid_from`/`valid_to` (or append-only per-run snapshots)
for rows that change, e.g. a replant date filled in months later.

**Amend/repeal, concretely:** the publication is immutable (it happened); the decision's
*legal force* is a validity-stamped state; an **amendment is a new decision entity** linked
`amends`/`supersedes` to the prior, each with its own validity — never an in-place edit. The
`Assessment` model below is just the most active instance of this one general rule.

**Finding (P13 `load koop`) — multiple decisions per case, and what v1 does not yet model.**
Over the real KOOP corpus a single zaak (`OVERHEIDop.referentienummer`) often carries more than one
publication under one `zaaknummer`: the primary **Besluit**, and sometimes a later **Verlenging**
(the replant/decision term extended) or a **Rectificatie/amendment**. These arrive as distinct
title-prefixed publications, not as edits to the original. **v1 audits only the primary besluit** as
one `Intervention`; the aanvraag / verlenging / ingetrokken / rectificatie publications are retained
verbatim in the PostGIS trail (`koop_publications`, one row per publication) but are **not** assembled
into the graph.

*Implication:* the graph today has no representation of an **extended or amended decision period** —
a case whose replant term was later extended, or whose besluit was rectified, looks in the graph
exactly like its original besluit. Any downstream reasoning keyed on the original besluit's dates
(P14 elapsed-time / timeliness, legal-force checks) will therefore **misjudge cases whose term was
changed by a Verlenging** — the change is invisible above the value store. No data is lost (the raw
publications sit in PostGIS), so the model can be added later without re-harvesting; but P14 must not
silently assume one decision per case.

*Deferred to Phase 2:* representing this as a validity-stamped **decision period** (the
`LegalStatusPeriod` / SCD-Type-2 node form above, or `amends`/`supersedes` decision entities) so the
graph carries the current term and its history. Phase 1 stops at the primary besluit to stay lean;
this note records the gap so it is reasoned about explicitly, not assumed away.

**Reference data is bitemporal too — resolve at the intervention's valid-time.** This is not only
an internal concern: **BAG itself is bitemporal** (Spike D, now `location/`) — every address is a
sequence of *voorkomens* with valid-time (`beginGeldigheid`/`eindGeldigheid`) and transaction-time
(`tijdstipRegistratie`/`eindRegistratie`), and a change may be a real-world event (valid-time
advances) or a correction (same valid-time, re-registered). Because we audit **backdated**
interventions, a 2022 permit's location must be resolved against the BAG state **valid in 2022**,
not today's snapshot — so we load the *full* voorkomen history and prefer `beginGeldigheid <= D AND
(eindGeldigheid IS NULL OR eindGeldigheid > D) AND eindRegistratie IS NULL`. Withdrawal/demolition
is a `status` change, not a dropped row, so a since-demolished address still resolves (with its
status). And because the intervention's *own* date can be unreliable, if nothing is valid at D we
**fall back to any voorkomen and stamp the mismatch on the link** (a `timeMismatch` caveat on the
resolved-location edge) rather than lose the link — resolve, but record how sure we are of the
time alignment. Detail + the load recipe: `DATA_SOURCES.md` §8.

### Fulfilment: best-effort, multi-axis, and tracked through time

Two rules here, both load-bearing for auditability.

**(1) Don't collapse to "indeterminate" — estimate on every axis you can.** A single status
throws away information and leaves the map mostly grey. Split the judgment into independent
axes, each computed from whatever evidence exists:

- **fulfilment estimate** — observed replant vs. the computed obligation: `none` /
  `partial(fraction)` / `fulfilled`. Computed whenever there is a link + counts, *regardless*
  of whether we know the deadline or the fund status.
- **timeliness** — `notYetDue` / `onTime` / `overdue` / `deadlineUnknown`. Needs the termijn
  (Spike A); absent it, `deadlineUnknown` — which does **not** block the fulfilment estimate.
  **Spike A settled this: the termijn lives nowhere publicly reliable, so `deadlineUnknown` is
  the default for almost all claims** (`spikes/spike-a/`). Deadline ladder: (1) an explicit
  permit-text termijn if Phase-2 NER finds one — expect ≈0 hits, and never accept the bezwaar
  window "binnen N weken" as the deadline; (2) a *soft* project horizon for herstructurering
  ("by end of project", `deadlineApprox`, low confidence); (3) `deadlineUnknown`.
  `kapenherplant.datumAfrondenVoor` is **rejected** (work-order artifact: 53% precede the
  felling, overshot in 100% of completed cases). Any elapsed-time signal must anchor on
  `kapmaatregelDatumUitgevoerd` (felling) — the "permit granted" dates are batch-assigned. An
  advisory "long-overdue" flag off elapsed-since-felling is a caveat, not a hard `overdue`.
- **caveats** — explicit flags for what we *don't* know: `fundEligibilityUnknown`,
  `deadlineUnknown`, `weakLink`. They qualify the estimate; they don't erase it.
- **confidence** — how sure we are of the permit↔trees link the whole assessment rests on
  (RDF-star + evidence), shown in the UI.

So instead of "indeterminate," a case reads as *"partial (9/18), deadline unknown,
fund-eligibility unknown, link confidence 0.8."* True `indeterminate` is reserved for the
narrow case where there is no usable link at all (we don't even know the obligation) — those
stay grey honestly, but they should be the minority, not the default.

**(2) Assessments evolve — track them bitemporally, never overwrite.** The obligation is
stable; the *assessment* of it changes as the registry updates, and we keep the full history:

- The **`Claim`** (obligation: N trees of class C at place L, deadline D-or-unknown) is
  derived once from the permit + law and is stable.
- Each `derive` run produces an **`Assessment`** — {estimate, timeliness, caveats,
  confidence} — stamped with **valid time** (`validFrom`/`validTo`, from the observation
  dates = when it was true in the world) and **transaction time** (which derive run produced
  it, against which source snapshot).
- When a later run finds the state changed (9→18 replanted), it **closes** the current
  Assessment (`validTo`) and **opens** a new one — it never overwrites. So a claim that is now
  `fulfilled` still carries its earlier `partial` assessment in history, with the interval it
  held.

That answers "does it still have a partial status once fulfilled?" — **yes, in history**; the
*current* assessment is fulfilled, the *prior* partial is retained with a closed valid
interval. Bitemporality (world-time + our-derivation-time) is what lets the audit answer "what
did we believe on date X, from which data" — the whole point of being auditable, and another
reason the versioned/provenanced graph earns its place. It is a real, required addition.

### Domain vocabulary (SKOS) — build from authoritative thesauri, grow from the corpus

A shared **SKOS ConceptScheme** does three jobs: seeds the NER **EntityRuler** (surface forms
→ concepts), **grounds the analytics agent** (what herplantplicht / herplantfonds / houtopstand
mean; the status vocabulary), and backs the **controlled value sets** SHACL enforces. We do not
invent it from scratch — the Netherlands already publishes most of it as Linked Data
(cataloged in `DATA_SOURCES.md` §11, regulation in §10):

- **TOOI** (`standaarden.overheid.nl/tooi/waardelijsten/`) — the government's SKOS thesauri for
  official publications; the `OVERHEID*/OVERHEIDvb` schemes the bekendmakingen are tagged with
  *are* TOOI waardelijsten. Source for document/rubriek + intervention typing. (Honest limit:
  kap-omgevingsvergunningen carry thin structured typing, so TOOI covers the publication
  taxonomy, not the tree substance.)
- **IMBOR** (CROW, published in RDF — `github.com/Stichting-CROW/imbor`) — the standard model
  for public-space objects, including the **BOOM** object with species + management measures,
  aligned with the Norminstituut Bomen *Handboek Bomen*. The authoritative tree-domain source.
- **Nederlands Soortenregister** — species Latin/Dutch names + synonyms, if IMBOR +
  `stamgegevens.soortnaam` leave a gap.
- **Data enums** — distinct values of `kapenherplant.boommaatregelBesluit`, `boomgebreken`,
  `stamgegevens.soortnaam`; **`gebieden`/CBS** for place names + codes.
- **The regulation** — legal/obligation concepts from Bomenverordening 2014 (CVDR323217) +
  the *Compensatie en herplant van bomen* beleidsregel (CVDR697591, source of the diameter-class
  equivalence) — houtopstand, herplantplicht, herplantfonds, monumentale boom, stamomtrek /
  diameter classes. See `DATA_SOURCES.md` §10.

**Build recipe:** (1) import only the *slices we actually touch* from the authoritative sources
(lean — not all of TOOI/IMBOR), aligning our concepts to their IRIs with `skos:exactMatch`
rather than minting parallel identifiers; (2) **mine `skos:altLabel`s from the real permit
corpus** — the phrasings permits use interchangeably (kappen / vellen / rooien / **verplanten**
/ herplant; "t.h.v."; abbreviations) — with the LLM proposing concept↔surface mappings for
human confirmation (the recognition-critical part curated lists always miss); (3) hand-author
the small legal top and its broader/narrower links; (4) iterate — NER misses feed back as new
altLabels, so the vocab is a living artifact: seeded from sources, grown from the corpus.
Identity stays in codes (BAG / CBS / IMBOR IRIs), never names — the vocab is a *recognition and
grounding* aid.

## 4. Location: keys and preloading

**There is no single fine key.** We resolve *both* sides to the ladder and link at the
finest level we can establish, with confidence:

| Source side | What it gives | Resolves to |
|---|---|---|
| Kap permit | a **structured point geometry (RD+WGS84)** ~99% of the time (Spike B — the metadata is *not* thin), plus a free-text / reference address ("t.h.v. …") and sometimes a project area | the **smallest area we can confidently place it in** — point-in-polygon from the permit's own geometry first, the free-text address only as a fallback: BAG object (point/footprint) ideally, else whichever of {project polygon, postcode-6, buurt} is *smallest by actual area* and clears a confidence threshold — a project polygon may be smaller than a postcode-6, so compare areas, don't assume a fixed order |
| `kapenherplant` | `dichtstbijzijndeBagAdres` + postcode, `gbdBuurtId`, and (via `boomId`→`stamgegevens`, with the `boomNieuwId` fallback for replanted rows — `DATA_SOURCES.md` §2a) a point | already pinned; point + buurt + nearest address |

The join is then finest-common-granularity + count + time-window (+ project when
extracted), and the resulting `AuditLink` stores the granularity used and a confidence.
Reference addresses that don't exist in BAG (demolished in renewal areas) are **resolved
during load** and written with an explicit `unresolvedLocation` marker + confidence — never
written as if they were exact. No half-broken data enters the graph.

**Preloading is feasible and preferred (verified):**
- `kapenherplant` = **35,202 rows** total; `stamgegevens` = **323,728**. Both page/CSV-export
  cleanly → load the whole city once, refresh on a schedule.
- **BAG has a real bulk source (verified, Spike D).** The Kadaster *LV BAG 2.0 Extract* is a free
  national dump (~**3.6 GB**), loaded via GDAL's `lvbag` driver straight into PostGIS, filtered to
  gemeente `0363`. Refresh is an **idempotent monthly full reload** — the driver reads only the ST
  snapshot, **not** the daily Mutatie-Levering files, so there is no incremental-via-`lvbag`. Load
  BAG **as-is** (it is the location master data): full tables, all columns, all voorkomens,
  municipality filter only. `gebieden` polygons (Datapunt GeoJSON) + CBS buurt/wijk geometries (PDOK
  WFS) load **separately** — the `lvbag` driver is BAG-specific. Raw downloads are retained in the
  landing store like any source (§5). **Full recipe + all corrections: `DATA_SOURCES.md` §8.**
- Location resolution runs **entirely against the local PostGIS — no PDOK Locatieserver call,
  not even as a fallback.** Everything Locatieserver offered (free-text → BAG, postcode/buurt
  lookup) is derivable from the bulk-loaded BAG + gebieden + CBS, and the fuzzy permit-address
  → BAG matching *is* core project work we want to own, not outsource. We would add PDOK back
  only if it turns out to expose something the bulk genuinely lacks and we need it then — not
  preemptively.

## 5. Software architecture

**Language boundary:** Go everywhere except NLP; Python only for the spaCy extractor;
TypeScript + SvelteKit for the frontend. **Operational model:** batch, idempotent,
incremental; re-running any stage is a no-op.

**Pipeline shape = raw → conformed → derived (bronze / silver / gold — the standard medallion
layering, and the best-practice answer to "where do transforms go").** This is what makes the
"separate canonicalize/assemble stages" instinct wrong:
- **raw / landing (bronze):** `ingest` writes each source *verbatim* + provenance to an
  immutable landing store (files/blobs + a raw table), never to the graph. You can always
  reprocess from here — and the **expensive NER output is cached at this layer** (keyed by
  document id + model/prompt version) so re-runs never re-invoke the LLM. Bulk/reference
  sources (BAG, `gebieden`, CBS geometries) land here too — we **keep all raw** so any stage
  can be reprocessed; the one candidate to later exempt is the large BAG extract, a cheap
  idempotent re-fetch rather than a captured event.
- **conformed (silver):** `load` maps + resolves location + assembles fully-formed, validated
  entities into the graph (+ values/geometry into PostGIS). Only clean data reaches the
  serving model.
- **derived (gold):** `derive` computes the cross-source `AuditLink` + replant progress and
  stores them, materialized for tree→buurt→stadsdeel aggregation.

So yes — we now follow best practice: an immutable raw layer you can reprocess from,
transform-on-load into a clean conformed layer, a materialized derived layer, every stage
idempotent, and expensive enrichment (NER) cached.

**Package layout — stage × source; binaries in `cmd/`:**

```
gemeten-stad/
  cmd/
    pipeline/             # one binary; subcommands: ingest / extract / load / derive / dump
    server/               # the API + SPA host (own lifecycle)
  ingest/                 # BRONZE: raw data in, per source
    koop/  bomen/  bag/  gebieden/
    shared/               # http, SRU, WFS/geo, paging, rate-limit, raw landing + provenance
  extract/                # OPTIONAL — unstructured only (permits); output cached durably
    shared/  koop/  domain/
  load/                   # SILVER: map + resolve + assemble
    koop/  bomen/          #   source-specific mapping
    graph/  geo/           #   shared writers (triplestore ; PostGIS)
  location/               # preloaded BAG + gebieden + CBS ; resolver (smallest-area + confidence)
  derive/                 # GOLD: AuditLink + computed progress (stored)
  dump/                   # export/snapshot: graph + PostGIS + the NER cache
  ontology/               # .ttl: ontology, SKOS vocab, SHACL shapes
  server/                 # (logic) layered controller → service → repository
  webapp/                 # SvelteKit SPA (map + chat)
  deploy/
    compose/              #   DEV: docker compose (triplestore + PostGIS + server)
    k3s/                  #   PROD (Phase 4): CronJob-per-source + derive CronJob + server Deployment
                          #   + observability wiring to the cluster monitoring stack (§"Observability")
```

**Loading does the resolving and assembling** — no separate post-load canonicalize/assemble
stage. Per record: map → resolve location against local PostGIS → write the fully-formed
entity (+ provenance, + confidence, + `unresolvedLocation` if unpinnable) to graph + PostGIS.
The only post-load step is **`derive`** (it needs both sources already present); its outputs
are stored derived data, not base entities.

**Ingest is scoped, not a full crawl.** The KOOP harvest is a *scoped SRU query* —
kap/verplant omgevingsvergunningen in Noord for the timeframe — not all bekendmakingen.
Widening scope later = widening the query.

**Binaries & orchestration.** Two Go binaries (`cmd/pipeline`, `cmd/server`) + the Python
extractor image. **Dev:** `docker compose` brings up triplestore + PostGIS + server; stages
run as `pipeline <subcommand>`. **Prod (Phase 4):** a CronJob per source
(ingest→extract→load), a `derive` CronJob, server as a Deployment; cron-polling since sources
don't push (KOOP ~daily, bomen ~weekly, BAG monthly full reload — `lvbag` has no daily-mutatie path, §8).

**Dumps.** A `dump` subcommand snapshots the graph, the PostGIS data, and — critically — the
**NER cache**, so expensive extraction is never lost and environments are reproducible.

**Testing isolation.** Integration tests target a **separate triplestore repo/namespace and a
separate Postgres database/schema**, env-driven, with a guard that refuses to run against the
production names — tests never pollute working data.

**Server layers:** controller (HTTP/SSE, GeoJSON) → service (audit queries, agent
orchestration) → repository (graph + value-store access). Agent tools = SPARQL over the graph
+ read-only SQL over the value store; answers are claim-level grounded and report uncertainty.

### Observability — reuse the cluster stack, add to it via GitOps

Design lives here; the wiring lands in Phase 4 (nothing to instrument until the pipeline runs on
k3s). The target is the **existing** `infra-workloads` monitoring stack — Prometheus + Pushgateway
+ Loki/Promtail + Grafana + Alertmanager, all in the `monitoring` namespace, deployed by Argo CD.
We **add to it via GitOps and never stand up our own** Prometheus/Grafana/Loki. Three signal
classes, each mapped to a mechanism the stack already provides:

- **Logs & warnings — make them structured so they stop vanishing.** The stages and server already
  log; today's warnings are write-only (unpinnable address → `unresolvedLocation`, the
  `timeMismatch` valid-time fallback, a `zaaknummer` dedup collision, NER low-confidence, an
  LLM↔rule-binder disagreement) and end up nowhere. The fix is to log **structured with a level**
  (Go `slog`/logfmt, Python JSON): Promtail's cluster pipeline *already* promotes `level` to a Loki
  stream label and keeps the message as queryable structured metadata, so a `level=~"warn|error"`
  panel plus a Loki-rate alert rule surfaces every warning — **no new infrastructure**, only a
  logging convention the code must honour.
- **Load stats — the pipeline is batch, so push, don't scrape.** Each `ingest`/`extract`/`load`/
  `derive` CronJob run pushes a metric set to the cluster **Pushgateway** (`honor_labels: true`,
  the idiomatic fit for short-lived jobs a scrape would miss), keyed by stage + source: records
  in/out, `zaaknummer` dedup drops, location-resolution outcomes (pinned / `unresolvedLocation` /
  `timeMismatch`) and the resolution rate, confidence distribution, NER-cache hits vs LLM
  escalations (bounds cost — Phase 2's ~14% target), `AuditLink`s derived + coverage rate,
  assessments opened/closed, per-stage duration, exit status, and a last-success timestamp. A small
  metrics helper in `ingest/shared` (Go) and the extractor (Python) emit these.
- **The audit's own uncertainty *is* a data-quality signal.** `unresolvedLocation`, `weakLink`,
  `deadlineUnknown`, and the `indeterminate`-verdict share are precisely the "warnings that should
  end up somewhere": their rates go on the dashboard as gauges so a data-quality regression is
  visible, not buried — the observability layer inherits the project's uncertainty-first ethos.

## 6. Phases

**Phase 0 — foundations & design spikes.** Repo skeleton + **docker compose** (triplestore +
PostGIS) + CI + the `dump`/NER-cache tool + integration-test DB isolation (k3s deferred to
Phase 4). Ontology v0 + the SKOS domain vocab (bootstrapped from sources) + SHACL + the
RDF-star confidence + bitemporal-assessment patterns. Preload
BAG/gebieden/CBS into PostGIS and the full `kapenherplant`/`stamgegevens`. Pick the exact
Noord quarter (size the permit volume). Spikes: **(A) — DONE (`spikes/spike-a/`):** the replant
*termijn* lives nowhere publicly reliable → default `deadlineUnknown`, `datumAfrondenVoor`
rejected (see §"Fulfilment"); **(B) — DONE (`spikes/spike-b/`):** permit→registry links at
**90%** (place+time, buurt-level; 54% also on count), registry→permit at **≥70%**, with **~3
candidate clusters per hit** so buurt+time alone is not unique — a **place-led confidence model**
(τ=0.60; count + finer BAG place disambiguate) carries the `AuditLink`. Two corrections fell out:
kap permits **do** carry a structured point geometry + controlled activiteit + zaaknummer (so
place/activity are structured, not NER'd; see §4 and `DATA_SOURCES.md` §1), and Phase-1 load must
**dedup by zaaknummer** (aanvraag+besluit duplicates) and audit the besluit; **(C) — DONE
(`spikes/spike-c/`):** count extraction is feasible in **three tiers** — a **deterministic abstract parser**
(no dependency) extracts the obligation count for **72%** of citywide besluiten (spelled-out numbers +
`houtopstand`, splitting the felling activities and summing them); a **spaCy EntityRuler seeded from the
domain vocabulary** (the `msr-graph` pattern) lifts recognition recall to **90%** by catching species-headed
counts ("drie essen"); and **LLM count-binding** resolves the appositive / snoeien / "waarvan"-breakdown
cases the rule binders miss — exact-match on an 80-case hand-labeled hard set **44% (regex) → 55% (spaCy) →
95% (LLM)**. Species (<10% in prose) / project (~1%) are the fuzzy residue the registry lacks entirely (0%
city-wide) and double as the **recognition vocabulary**. The abstract *is* the document's Omschrijving line
(body adds nothing → no per-doc fetch). And **verplanten ≡ vellen**: Bomenverordening art. 1k *defines*
vellen to include verplanten, the registry has **no Verplanten value** (all 35,202 rows: only
`Vellen (boom verwijderen)`) and logs transplants as Vellen + replants them → the "18 verplant = 18 vellen"
match **holds** (carry a `transplantOrigin` caveat). Corrections folded into `DATA_SOURCES.md`
§1/§2a/§10/§11 and §"Fulfilment"/Phase 2 below; **(D) — DONE
(promoted into `ingest/bag`, `ingest/gebieden`, `load/geo`, `location/` — Phase 0 · P6):** the geo
bulk backbone loads & resolves locally — BAG *LV 2.0 Extract* via
GDAL `lvbag` into PostGIS (national ~3.6 GB, filtered to `0363`, **all voorkomens**) + `gebieden`
(Datapunt GeoJSON) / CBS (PDOK WFS) polygons, with **100% point-in-polygon** accuracy vs
`gbdBuurtId` and **90% address-precision** free-text/reference resolution at the permit's valid-time,
median 0 m from the permit's own point — confirming the PDOK Locatieserver replacement. Corrections
folded into `DATA_SOURCES.md` §0/§8: extract is national-only ~3.6 GB, `lvbag` is ST-snapshot-only
(no daily ML → monthly full reload), BAG is bitemporal (resolve at valid-time); **(E) — DONE
(`spikes/spike-e/`):** the triple store is **Apache Jena Fuseki** (Apache-2.0) — the only genuinely
open-source candidate that meets all five needs (RDF-star, SHACL gate, PROV named graphs, free
container, native dump) in one container, confirmed empirically against Fuseki 5.5.0. The two flagged
risks hold up: SHACL validates over RDF-star (Jena #3503 does not bite), and the **confidence-presence
rule is enforced with a `sh:sparql` SPARQL-star constraint** — core SHACL cannot reach into a quoted
triple — validated identically by the Fuseki SHACL endpoint and the Jena CLI. Confidence is written with
the `{| … |}` annotation form (asserts the base edge **and** annotates it); run provenance lives in a
dedicated named graph (the dataset serves `unionDefaultGraph` on). GraphDB Free (proprietary + lock-in),
Oxigraph (no native SHACL) and RDF4J (beta RDF-star, flagged RDF-1.2-incompatible) were eliminated on
their downsides, not their feature lists.

**Phase 1 — deterministic backbone (no LLM).** `ingest koop` (Noord kap permits, incremental
by publication id, Go SRU harvest) + `load koop` (dedup by zaaknummer, resolve location, assemble
`Intervention`/`Claim` to graph via the SHACL-gated `load/graph` writer, values to PostGIS) +
`derive` **coverage only** (permit→registry entry? — the Spike-B τ=0.60 place-led `AuditLink` with
confidence, and "no source found" as a first-class provenanced finding). A working **coverage** audit
from structure alone, with confidences. **`load bomen` is not a Phase-1 item** — the registry is
value-store data already loaded in Phase 0 (P7) and never becomes graph entities (§3); `derive` only
reads it. **Fulfilment (progress) and the permit-count cross-check are deferred to Phase 2**, where
extraction lands — a fulfilment fraction is only meaningful against an extracted obligation count.
Decomposed into work items in `PHASE_1_PLAN.md` (P11–P15); prerequisite: **P8 closes in Phase 0**.

**Phase 2 — extraction.** A **three-tier extractor mirroring `msr-graph`** (Spike C, `spikes/spike-c/`),
scaled so the audit works without the fancy parts but count recovery is near-complete with them:
1. **Deterministic floor (Go, no dependency)** — the abstract parser: **per-activity counts**
   (kappen/vellen/rooien/**verplanten** split then summed; spelled-out-aware; herplant kept on the replant
   side), 72% of besluiten, the offline fallback. Place/activity/zaaknummer are already structured (Spike B).
2. **Recognition — spaCy EntityRuler seeded from the SKOS graph** (activity altLabels + species from
   IMBOR/Soortenregister/mined altLabels; concept IRI in the pattern `id` → `ent_id_`, the `msr-graph`
   `graph_reader → seeding` shape). Primary recognizer (recall 83%→90%) and the NER learning track; grows
   with the vocab, no code changes. Per-span provenance into the graph; feeds Spike B's matcher.
3. **LLM count-binding — invoked selectively, not on every permit** — the recognized activity/species IRIs
   constrain an LLM that binds the counts over the one-sentence abstract (44%→55%→**95%** on the hard tail);
   resolves appositives / snoeien / "waarvan". It is an **escalation**: triggered only where tiers 1–2
   disagree, a felling permit yields no count, or a hard-shape flag fires — where the two cheap tiers agree
   the count is taken as-is (LLM matched 98/100 there). So only ~14% of felling permits reach the LLM;
   the majority is classified offline, bounding cost and preserving reproducibility.
Plus a **statistical-`nl` mining loop** proposing new species/project altLabels → human confirmation → the
graph (the SKOS "living vocab" recipe). §5 holds: the floor is Go, **Python owns spaCy**; the LLM binder is a
new *additive* dependency the evidence earns — NER is central, but the audit core still runs without it.

**Phase 2 also completes the fulfilment axes** — extraction is the enabler, not the whole phase. Phase 1's
`derive` is coverage-only (`PHASE_1_PLAN.md`); once the extractor supplies the permit's **obligation count**,
Phase 2 **extends `derive`** to compute the multi-axis fulfilment of §"Fulfilment": the **fulfilment estimate**
(`none`/`partial(fraction)`/`fulfilled`) from observed registry replant vs the computed obligation, the
**registry↔permit count cross-check** (the external triangulation that catches under-reporting — registry-side
felled-vs-replant counting becomes meaningful only against the permit's own stated number), the **caveats**
(`fundEligibilityUnknown`, `deadlineUnknown`, `weakLink`, `transplantOrigin`), and the **bitemporal
`Assessment`** open/close as the registry updates. Timeliness stays `deadlineUnknown` by default (Spike A);
diameter-class equivalence (beleidsregel CVDR697591) turns raw felled counts into the obligation quantity.

**Phase 3 — webapp.** Map of interventions coloured by claim status (fulfilled / partial /
open / overdue / **indeterminate**) with an evidence + confidence panel; grounded chat;
"no matching source found" as a first-class, provenanced finding.

**Phase 4 — generalise, productionize & pursue hidden data.** **Productionize on k3s** — deploy
through the `infra-workloads` GitOps repo (Argo CD app-of-apps): a `gemeten-stad` namespace with a
CronJob per source (ingest→extract→load), a `derive` CronJob, and the server as a Deployment, from
the `deploy/k3s` manifests. **Observability wiring (design in §"Observability"):** feed the
*existing* cluster monitoring stack — stages push load-stats to the Pushgateway and log structured
(level-tagged) to Loki, the server carries `prometheus.io/scrape` annotations — surfaced behind one
**`grafana-dashboard-gemeten-stad`** ConfigMap (pipeline-run health & durations, load volumes over
time, resolution/coverage/confidence rates, the `warn|error` log stream, the data-quality gauges,
LLM-escalation rate) added to Grafana's provisioned dashboards, plus alert rules in
`prometheus-rules.yaml` routed through the existing Alertmanager (a stage missed its cadence — KOOP
daily / bomen weekly / BAG monthly, a stage exited non-zero, resolution rate below threshold, a
warning/error spike). Then: reconciliation vs the bomenboekhouding (the third audit leg;
`DATA_SOURCES.md` §9); a **WOO request for the herplantfonds balance** (credible precisely because
the rest works and uncertainty is shown); then a second intervention type (e.g. EV-charging
verkeersbesluiten) reusing the machinery — the seam test.

## 7. Explicitly out of scope for vertical 1

- **Self-evolving ontology** — deferred; revisit after the vertical works. Effort goes to
  location-resolution strategies (permit → BAG) instead.
- **CBS / police buurt context** — dropped; adds nothing to the replant verdict. Pull only if
  a later vertical needs it and it's cheap to link.

## 8. Risks carried into design

- Deadline source — **resolved by Spike A (`spikes/spike-a/`): it lives nowhere publicly
  reliable.** Not in the permit (published kap notices are stubs; the only date is the bezwaar
  "6 weken" decoy), not in the registry (`datumAfrondenVoor` precedes the felling 53% of the
  time, overshot 100%), and the policy makes the termijn a per-permit discretionary condition.
  → default `deadlineUnknown`, don't invent one; anchor any elapsed-time signal on the felling
  date, not a permit date.
- Linkage false-negatives — **measured by Spike B (`spikes/spike-b/`):** permit→registry 90%
  (place+time), registry→permit ≥70%; ~3 candidate clusters per hit mean buurt-level place is not
  unique, so the τ=0.60 place-led confidence model reports the rate and the unmatched cases as
  grounded "no source found" findings — confidence is shown, not hidden.
- Herplantfonds blind spot → *indeterminate* verdicts until the WOO data lands.
- Non-1:1 equivalence → obligation quantities need the diameter-class rules.
- Registry lag & batch-assigned `datumVergunningVerleend` → decision→registry latency is a
  measurable phenomenon, not a bug.
- Datapunt API key optional now; Datapunt signals a free key will be required **soon** (the
  exact date is not yet decided) and recommends provisioning one → provision a free key early;
  revisit the enforcement details once the vertical slice is up. (`DATA_SOURCES.md` §2.)
