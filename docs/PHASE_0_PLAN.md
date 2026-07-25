# Phase 0 plan — foundations & modeling

Phase 0 settles the architecture, stands up the stores, and preloads the reference data so the
Phase-1 deterministic backbone can be built on solid ground. See `IMPLEMENTATION_PLAN.md` §6 for
where Phase 0 sits; this file decomposes it into independently-tackleable work items.

## Spike status

All five design spikes that de-risk this vertical are **done** — their findings are already folded
into `IMPLEMENTATION_PLAN.md` and `DATA_SOURCES.md`:

- **Spike A** (`spikes/spike-a/`) — the replant *termijn* lives nowhere publicly reliable →
  default `deadlineUnknown`, `datumAfrondenVoor` rejected.
- **Spike B** (`spikes/spike-b/`) — permit→registry links at 90% (place+time), registry→permit
  ≥70%; τ=0.60 place-led confidence model carries the `AuditLink`.
- **Spike C** (`spikes/spike-c/`) — counts are extractable from the formulaic permit abstract with
  a lightweight deterministic parser (no NER for the audit core; yield 35%→70% citywide), and
  **verplanten ≡ vellen**: the Bomenverordening defines vellen to include verplanten and the
  registry has no separate Verplanten value, so the thread's "18 = 18" holds (with a
  `transplantOrigin` caveat). Species/project are the fuzzy residue (<10% in prose, 0% in the
  registry) → the home for the Phase-2 spaCy NER lane, off the audit critical path.
- **Spike D** (`spikes/spike-d/`) — the geo bulk backbone loads & resolves locally: BAG *LV 2.0
  Extract* via GDAL `lvbag` into PostGIS + gebieden/CBS polygons, 100% point-in-polygon accuracy,
  90% address-precision resolution at the permit's valid-time.
- **Spike E** (`spikes/spike-e/`) — the triple store is **Apache Jena Fuseki** (Apache-2.0), the only
  genuinely open candidate meeting all five needs in one container. SHACL validates over RDF-star (Jena
  #3503 does not bite); the confidence-presence rule is enforced with a `sh:sparql` SPARQL-star constraint
  (core SHACL can't reach a quoted triple); confidence is written with the `{| … |}` form; run provenance
  lives in a dedicated named graph. GraphDB Free / Oxigraph / RDF4J eliminated on their downsides.

## Recommended sequencing

- **Wave A — unblockers (start immediately, mostly independent):** P1 repo skeleton · P2
  triple-store selection · P10 set the Noord backfill window
- **Wave B — infra (needs A):** P3 dev compose · P4 CI · P5 integration-test isolation
- **Wave C — preload (needs skeleton + compose):** P6 lift BAG/geo · P7
  kapenherplant/stamgegevens preload
- **Wave D — modeling (needs P2):** P8 ontology + vocab + shapes + uncertainty/temporal patterns
- **Wave E — tooling (needs the stores populated):** P9 dump / NER-cache tool

The two hard gates were **P2** (blocks all of Wave D) and **P1** (blocks all code); **both are now
DONE** (triple store = Fuseki; skeleton compiling), so every downstream item is unblocked. **P3** and
**P4** are also DONE. The remaining open work is P5–P9.

---

## P1 · Repo skeleton · **DONE**

- **Goal:** Stand up the `stage × source` package layout from `IMPLEMENTATION_PLAN.md` §5 as a
  compiling Go module + the empty dir tree.
- **Entails:** `go.mod`; `cmd/pipeline` (subcommand stubs: `ingest/extract/load/derive/dump`) +
  `cmd/server`; the `ingest/ extract/ load/ location/ derive/ dump/ ontology/ server/ webapp/
  deploy/` tree; a `Makefile`/`Taskfile` with build/test/lint targets; `.golangci.yml`; a README
  pointing at the docs. SvelteKit `webapp/` as a bare `create-svelte` stub so CI wiring is real.
- **Key decisions:** subcommand wire-up (cobra vs stdlib `flag`); module path.
- **Done when:** ~~`make build` produces both binaries; `pipeline --help` lists the five
  subcommands (no-op).~~ ✓ `task build` builds both binaries; `pipeline --help` lists the five
  subcommands. (Build tooling is a `Taskfile`, not a Makefile.)
- **Depends on:** nothing.

## P2 · Triple-store selection — Spike E · **DONE** (`spikes/spike-e/`)

- **Goal:** Settle Jena Fuseki vs GraphDB against the three requirements (free-to-use, container,
  RDF-star) **plus** the two load-bearing needs the design imposes: SHACL validation and PROV
  **named-graph** writes.
- **Entails:** a throwaway `spikes/spike-e/` that, in a container, (1) loads a tiny RDF-star
  sample, (2) runs a SPARQL-star query reading a `<< … >> :confidence`, (3) validates against a
  SHACL shape, (4) writes into a run-stamped named graph and queries across graphs.
- **Decision: Apache Jena Fuseki** (Apache-2.0), confirmed empirically against Fuseki 5.5.0 — the only
  genuinely open-source candidate meeting all five needs in one container. All 13 checks pass
  (`./run.sh`). The two flagged risks hold: SHACL validates over RDF-star (Jena #3503 does not bite), and
  the **confidence-presence rule needs a `sh:sparql` SPARQL-star constraint** (core SHACL can't reach a
  quoted triple) — validated identically by the SHACL endpoint and the Jena CLI. Confidence is written
  with the `{| … |}` form; run provenance lives in a dedicated named graph (dataset serves
  `unionDefaultGraph` on). GraphDB Free / Oxigraph / RDF4J eliminated on their downsides (see the spike
  README). **Hand-offs:** P3 lifts `compose.yaml`'s `fuseki` service (named volume, healthcheck,
  `-Xmx2g`, `ENABLE_SHACL`); P8 uses the `{| … |}` + `sh:sparql` pattern from `shapes/shapes.ttl`.
- **Done when:** ~~a written decision (one paragraph) + a compose service definition for the
  winner + the four checks pass.~~ ✓ all delivered in `spikes/spike-e/`.
- **Depends on:** nothing. **Blocks:** P3, P8 (now unblocked).

## P3 · Dev docker compose · **DONE** (`deploy/compose/compose.yaml`)

- **Goal:** One `deploy/compose/` bringing up triplestore + PostGIS + server.
- **Entails:** lift spike-d's `compose.yaml` (PostGIS 16-3.4, named volume, healthcheck) as the
  base; add the P2-chosen triplestore service; add the `server` service; keep the `gdal` sidecar
  for loads (or fold into P6's decision). Env-driven connection config.
- **Done when:** ~~`docker compose up` yields a healthy PostGIS + triplestore + reachable server.~~
  ✓ delivered in `deploy/compose/compose.yaml`.
- **Depends on:** P1, P2.

## P4 · CI · **DONE** (`.github/workflows/ci.yml`)

- **Goal:** Build/test/lint gate on push.
- **Entails:** Go build + `go test ./...` + golangci-lint; Python lint/test for the extractor
  scaffold; SvelteKit build for the stub; a job that spins compose services for integration tests
  (ties to P5). Runner: GitHub Actions.
- **Done when:** ~~green pipeline on a trivial PR; red on a failing test.~~ ✓ `.github/workflows/ci.yml`
  gates Go build/test/lint, the Python extractor scaffold, and the SvelteKit stub. (Integration lane
  lands with P5.)
- **Depends on:** P1 (and P5 for the integration lane).

## P5 · Integration-test DB isolation

- **Goal:** The guard from §5 — tests hit a **separate** triplestore repo/namespace + separate
  Postgres db/schema, and **refuse** to run against production names.
- **Entails:** env-driven test config; a startup assertion that aborts if target names match the
  production set; helpers to create/tear down the isolated namespace + schema; a shared test
  harness both Go and (if needed) Python use.
- **Done when:** tests run against the isolated targets; flipping the env to a production name
  makes the guard abort.
- **Depends on:** P1; informs P3/P4.

## P6 · Lift BAG / gebieden / CBS preload into main code

- **Goal:** Promote spike-d's geo backbone from throwaway shell into `ingest/bag`,
  `ingest/gebieden`, `load/geo`, `location/`.
- **Entails:** decide **how much stays ogr2ogr vs moves to Go** — the `lvbag`→PostGIS load is
  genuinely easier as ogr2ogr, so likely a Go orchestrator shelling to the GDAL sidecar, not a
  rewrite. Fold in the raw-landing/provenance requirement (§5 bronze). Port `sql/resolve.sql` +
  `sql/pip.sql` into `location/` as the resolver (smallest-area + valid-time voorkomen selection +
  `timeMismatch`/`unresolvedLocation` caveats). Keep the monthly-full-reload idempotency. Recipe:
  `DATA_SOURCES.md` §8.
- **Done when:** the ingest reproduces spike-d's sanity gates (69 buurten, 15 wijken, single SRID
  28992) on a clean volume; the resolver returns the same PIP/address-precision numbers.
- **Depends on:** P1, P3.

## P7 · Preload kapenherplant + stamgegevens

- **Goal:** Full-city load of `kapenherplant` (35,202 rows) + `stamgegevens` (323,728) into
  PostGIS.
- **Entails:** `ingest/bomen` paged CSV/API export → raw landing; `load/bomen` into typed PostGIS
  tables incl. the `boomId`→`stamgegevens` and `boomNieuwId`-fallback join (`DATA_SOURCES.md`
  §2a); scheduled-refresh idempotency. This is the value-store half the `derive` step later reads.
- **Done when:** row counts match; a spot-check tree resolves to a point + buurt + nearest address.
- **Depends on:** P1, P3.

## P8 · Modeling — ontology, vocab, shapes, uncertainty & temporal patterns

One coherent modeling effort under `ontology/`. Build the TBox first, layer the uncertainty and
temporal patterns onto it, add the vocabulary, then let SHACL validate the whole. The activity-term
vocabulary can be fixed now that Spike C has settled it (**verplanten ≡ vellen**; register a
`transplantOrigin` caveat rather than a separate concept).

- **Goal:** The graph model from `IMPLEMENTATION_PLAN.md` §3 and its patterns, as loadable `.ttl`
  plus a validating load path.
- **Entails:**
  - **Ontology v0 (TBox):** `Intervention –locatedAt→ Place`, `partOfProject→ Project`
    (optional/sparse), `–claims→ Claim –testedAgainst→ Observation`, derived `AuditLink`,
    `Assessment`. Values stay out of RDF. → `ontology/ontology.ttl`.
  - **RDF-star confidence pattern:** first-class uncertainty edges —
    `<< :intervention :locatedAt :place >> :confidence 0.7 ; :evidence …` on the location link and
    the `AuditLink`; a SPARQL-star read the server/UI will use.
  - **Bitemporality + PROV pattern:** stable IRIs; un-stamped immutable facts; valid-time on
    evolving state via the two mechanisms (RDF-star annotation for single facts; state/period
    nodes for the `Assessment`/`LegalStatusPeriod` SCD2 shape); transaction-time as PROV
    run-stamped named graphs (`prov:generatedAtTime`); the Postgres-side SCD2 `valid_from/valid_to`
    convention; `Assessment` open/close (supersede, never overwrite).
  - **SKOS domain vocabulary:** bootstrapped from authoritative sources, not invented (§3 "Build
    recipe") — import only the slices we touch from TOOI + IMBOR (BOOM object) + Soortenregister +
    the regulation (Bomenverordening 2014 CVDR323217, beleidsregel CVDR697591), aligning with
    `skos:exactMatch`; ingest data enums (`boommaatregelBesluit`, `boomgebreken`, `soortnaam`,
    gebieden/CBS); hand-author the small legal top. Corpus-mined `altLabel`s are a Phase-2
    feedback loop — v0 seeds from sources + enums only. → `ontology/vocab.ttl`.
    - **Species concepts MUST carry plural (and inflected) surface forms as `skos:altLabel`s**, not
      only the `skos:prefLabel` singular — e.g. `iep`/`iepen`, `es`/`essen`, `populier`/`populieren`,
      `els`/`elzen`, plus the Latin genus. This is load-bearing, not cosmetic: Spike C
      (`spikes/spike-c/`) showed the spaCy EntityRuler is seeded directly from these labels and the
      `nl_core_news_md` lemmatizer **mis-normalizes botanical terms** ("essen"→"Essen", "iepen"→"ie",
      "berken"→VERB), so a singular-only vocab silently fails to recognize the plural — and because a
      species-headed count ("drie *essen*") is unparseable until "essen" is a known term, missing
      plurals directly loses tree counts. Seed the common Amsterdam species with both forms in v0;
      the Phase-2 mining loop grows the long tail.
  - **SHACL shapes:** enforce structure + the SKOS-backed controlled value sets + confidence/
    caveat presence rules so "no half-broken data enters the graph" (§4); wired into `load` as a
    gate. → `ontology/shapes.ttl`.
- **Done when:** the TBox + vocab + shapes load into the triplestore; a well-formed instance
  (with a confidence-annotated location edge written into a run-stamped named graph) passes SHACL;
  a malformed one is rejected.
- **Depends on:** P2.

## P9 · Dump / NER-cache snapshot tool

- **Goal:** `pipeline dump` snapshots graph + PostGIS + **the NER cache** (§5) so expensive
  extraction is never lost and environments are reproducible.
- **Entails:** graph export (triplestore-native dump), PostGIS dump, NER-cache blob capture, plus
  restore. The NER cache is only *populated* in Phase 2, but the dump tool and the cache's on-disk
  shape (keyed by doc id + model/prompt version) should be defined now.
- **Done when:** a dump→restore round-trip reproduces both stores on a clean volume.
- **Depends on:** P6, P7 (something to dump); the triplestore from P2.

## P10 · Set the Noord backfill window & size the corpus — **DONE**

- **Goal:** Replace "a 2022 quarter" with the rolling backdated window the vertical loads, and size
  its permit volume. This is a **characterization**, not a narrow pick — the earlier "pick a
  quarter" framing assumed volume was the constraint; it never was (the corpus is tiny at any
  horizon), so the only real bound is *observability of the replant obligation*.
- **Decision (verified 2026-07-25):** **stadsdeel Noord, kap/verplant omgevingsvergunningen
  published 2021 → present, no recent-end cutoff.**
  - **Floor = 2021, data-driven.** The KOOP kap omgevingsvergunning stream for Amsterdam is empty
    in 2020 and starts in 2021 (855 citywide publications, vs 2,299 in 2022); the `kapenherplant`
    registry's earliest Noord felling is **2021-02-10**. Permits before 2021 have nothing to audit
    against.
  - **No recent bound.** A permit whose replant window has not yet elapsed resolves to a *pending /
    indeterminate* verdict — a first-class output (`IMPLEMENTATION_PLAN.md` §1/§2), not a case to
    exclude. So the load is production-shaped from day one; observability is judged **per-record**
    off the felling date, never by clipping the corpus.
  - **Corpus size (`spikes/spike-b/size_window.py`, reusing the proven harvest):** tiny at every
    horizon — a rolling multi-year window still processes very little data.

    | year | Noord publications | of which besluiten | observability |
    |---|---|---|---|
    | 2020 | 0 | 0 | stream absent |
    | 2021 | 93 | 38 | settled |
    | 2022 | 365 | 147 | settled |
    | 2023 | 376 | 169 | mostly settled |
    | 2024 | 297 | 121 | mixed |
    | 2025 | 300 | 138 | mostly *pending* |
    | 2026 | partial (year in progress) | — | *pending* |

    ~600 besluiten across the whole settled-plus-recent span (2021–2025) — a full rolling
    multi-year window still processes trivially little data, which is the point: the single-quarter
    restriction bought nothing.

  - **Observability gradient** — replant rate by felling year (Noord `kapenherplant`): 2021 **70%**
    → 2022 **84%** → 2023 **37%** → 2024 **28%** → 2025 **11%** → 2026 **0%**. The recent collapse
    is *pending, not violation* — which is exactly why the derived aggregation **must bucket
    pending/indeterminate separately** from fulfilled/overdue (a discipline the full window forces
    from the start).
- **Done when:** ~~the quarter is recorded~~ ✓ the window (Noord, 2021 → present) + per-year sizing
  are recorded in `IMPLEMENTATION_PLAN.md` §1.
- **Depends on:** nothing — pure data analysis, ran parallel to everything.
