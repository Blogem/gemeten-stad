# Implementation plan — vertical 1: the Amsterdam tree lifecycle, stadsdeel Noord

The first vertical of De Gemeten Stad (see `VISION.md` for the overarching idea;
`DATA_SOURCES.md` for the source catalog; `DATA_THREAD_TREES.md` for a live worked example).
Early phases are design-heavy — they settle the architecture and resolve the open questions
that decide whether the audit is sound before build-out.

## 1. Scope

- **Domain:** the statutory tree replant obligation (Bomenverordening 2014 art. 7 —
  herplantplicht "in beginsel altijd"; termijn per permit by the college).
- **Place:** stadsdeel **Noord** (`gebieden` id `03630000000019`, code `N`).
- **Timeframe:** kap/verplant permits from a **single backdated quarter (target: a 2022
  quarter)** — old enough that the replant window (often the next planting season, and up to
  ~2.5 yr in observed data) has elapsed, so fulfilment is actually observable. Phase 0 sizes
  Noord volume and picks the exact quarter.
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
  assessments, resolved-location links, revisable attributes → `validFrom`/`validTo`.

**Two mechanisms, chosen by shape (not one-size-fits-all):**
- **RDF-star statement annotation** for a single evolving fact that also carries confidence —
  `<< :permit :legalStatus :inForce >> :validFrom … ; :validTo … ; :confidence …`. Used for
  the location link and simple status facts.
- **A state/period node** (the n-ary / "fluent" pattern) when the evolving thing has several
  attributes that move together and you want to query the periods as objects — e.g. the
  `Assessment` below, or a permit's `LegalStatusPeriod`. This is the KG form of the
  data-warehouse **SCD Type 2** pattern.

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
| `kapenherplant` | `dichtstbijzijndeBagAdres` + postcode, `gbdBuurtId`, and (via `boomId`→`stamgegevens`) a point | already pinned; point + buurt + nearest address |

The join is then finest-common-granularity + count + time-window (+ project when
extracted), and the resulting `AuditLink` stores the granularity used and a confidence.
Reference addresses that don't exist in BAG (demolished in renewal areas) are **resolved
during load** and written with an explicit `unresolvedLocation` marker + confidence — never
written as if they were exact. No half-broken data enters the graph.

**Preloading is feasible and preferred (verified):**
- `kapenherplant` = **35,202 rows** total; `stamgegevens` = **323,728**. Both page/CSV-export
  cleanly → load the whole city once, refresh on a schedule.
- **BAG has a real bulk + incremental source (verified).** The Kadaster *LV BAG 2.0 Extract*
  is a free national dump refreshed monthly (the 8th, ~1.5 GB) **plus daily mutation files**
  (national-only, applied in order, empty on weekends) — via the Kadaster BAG-Extract product
  / PDOK atom feed; GDAL's `lvbag` driver loads it straight into PostGIS. Filter to gemeente
  `0363` for Amsterdam. So: monthly full load + daily mutaties keeps BAG current
  incrementally. `gebieden` polygons + CBS buurt/wijk geometries load **separately via
  WFS/GeoPackage** — the `lvbag` driver is BAG-specific. Raw downloads are retained in the
  landing store like any source (§5). Catalog detail: `DATA_SOURCES.md` §8.
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
don't push (KOOP ~daily, bomen ~weekly, BAG monthly + daily mutaties).

**Dumps.** A `dump` subcommand snapshots the graph, the PostGIS data, and — critically — the
**NER cache**, so expensive extraction is never lost and environments are reproducible.

**Testing isolation.** Integration tests target a **separate triplestore repo/namespace and a
separate Postgres database/schema**, env-driven, with a guard that refuses to run against the
production names — tests never pollute working data.

**Server layers:** controller (HTTP/SSE, GeoJSON) → service (audit queries, agent
orchestration) → repository (graph + value-store access). Agent tools = SPARQL over the graph
+ read-only SQL over the value store; answers are claim-level grounded and report uncertainty.

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
**dedup by zaaknummer** (aanvraag+besluit duplicates) and audit the besluit; **(C)**
count/species/project
extraction feasibility from permit prose — **including how to interpret the activity terms**
(kappen / vellen / rooien / *verplanten*) against the registry's `boommaatregelBesluit`, and
whether *verplanten* (transplant, the tree survives) vs *vellen* (removal) changes whether/how
herplantplicht applies; read the actual corpus before fixing the mapping (the thread's
"18 verplant = 18 vellen" match is provisional until this settles); **(D)** the geo bulk
backbone loads & resolves —
BAG *LV 2.0 Extract* via GDAL `lvbag` into PostGIS (filtered to `0363`) + `gebieden`/CBS
wijk-buurt polygons via WFS/GeoPackage, and free-text/reference-address → BAG resolution +
point-in-polygon work locally (the PDOK Locatieserver replacement; `DATA_SOURCES.md` §0/§8).

**Phase 1 — deterministic backbone (no LLM).** `ingest koop` (Noord kap permits, incremental
by publication id) + `load` (resolve location, assemble to graph, values to PostGIS) +
`load bomen` + `derive` (coverage: permit→registry entry?; fulfilment: progress vs termijn,
with fund-indeterminate). A working audit from structure alone, with confidences.

**Phase 2 — extraction.** Python extractor: counts / species / project / any stated termijn
from permit prose → into the graph with per-span provenance; feeds Spike B's matcher and
sharpens links.

**Phase 3 — webapp.** Map of interventions coloured by claim status (fulfilled / partial /
open / overdue / **indeterminate**) with an evidence + confidence panel; grounded chat;
"no matching source found" as a first-class, provenanced finding.

**Phase 4 — generalise, productionize & pursue hidden data.** **Productionize on k3s**
(CronJob-per-source + a `derive` CronJob + server Deployment, from the `deploy/k3s` manifests);
reconciliation vs the bomenboekhouding (the third audit leg; `DATA_SOURCES.md` §9); a **WOO
request for the herplantfonds balance**
(credible precisely because the rest works and uncertainty is shown); then a second
intervention type (e.g. EV-charging verkeersbesluiten) reusing the machinery — the seam test.

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
