# Phase 0 plan — foundations & modeling

Phase 0 settles the architecture, stands up the stores, and preloads the reference data so the
Phase-1 deterministic backbone can be built on solid ground. See `IMPLEMENTATION_PLAN.md` §6 for
where Phase 0 sits; this file decomposes it into independently-tackleable work items.

## Spike status

All four design spikes that de-risk this vertical are **done** — their findings are already folded
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

Only **Spike E** (triple-store selection, item P2) is new and still open.

## Recommended sequencing

- **Wave A — unblockers (start immediately, mostly independent):** P1 repo skeleton · P2
  triple-store selection · P10 pick the quarter
- **Wave B — infra (needs A):** P3 dev compose · P4 CI · P5 integration-test isolation
- **Wave C — preload (needs skeleton + compose):** P6 lift BAG/geo · P7
  kapenherplant/stamgegevens preload
- **Wave D — modeling (needs P2):** P8 ontology + vocab + shapes + uncertainty/temporal patterns
- **Wave E — tooling (needs the stores populated):** P9 dump / NER-cache tool

The two hard gates are **P2** (blocks all of Wave D) and **P1** (blocks all code). Start both first.

---

## P1 · Repo skeleton

- **Goal:** Stand up the `stage × source` package layout from `IMPLEMENTATION_PLAN.md` §5 as a
  compiling Go module + the empty dir tree.
- **Entails:** `go.mod`; `cmd/pipeline` (subcommand stubs: `ingest/extract/load/derive/dump`) +
  `cmd/server`; the `ingest/ extract/ load/ location/ derive/ dump/ ontology/ server/ webapp/
  deploy/` tree; a `Makefile`/`Taskfile` with build/test/lint targets; `.golangci.yml`; a README
  pointing at the docs. SvelteKit `webapp/` as a bare `create-svelte` stub so CI wiring is real.
- **Key decisions:** subcommand wire-up (cobra vs stdlib `flag`); module path.
- **Done when:** `make build` produces both binaries; `pipeline --help` lists the five
  subcommands (no-op).
- **Depends on:** nothing.

## P2 · Triple-store selection — Spike E

- **Goal:** Settle Jena Fuseki vs GraphDB against the three requirements (free-to-use, container,
  RDF-star) **plus** the two load-bearing needs the design imposes: SHACL validation and PROV
  **named-graph** writes.
- **Entails:** a throwaway `spikes/spike-e/` that, in a container, (1) loads a tiny RDF-star
  sample, (2) runs a SPARQL-star query reading a `<< … >> :confidence`, (3) validates against a
  SHACL shape, (4) writes into a run-stamped named graph and queries across graphs.
- **Recommendation:** **Apache Jena Fuseki** most likely wins — the only candidate that is
  actually open source (Apache-2.0, vs GraphDB Free which is free-tier but proprietary/
  feature-gated), ships official containers, has native SPARQL-star, and bundles `jena-shacl`.
  Confirm SHACL-over-RDF-star and named-graph ergonomics before committing — everything in Wave D
  sits on this.
- **Done when:** a written decision (one paragraph) + a compose service definition for the
  winner + the four checks pass.
- **Depends on:** nothing. **Blocks:** P3, P8.

## P3 · Dev docker compose

- **Goal:** One `deploy/compose/` bringing up triplestore + PostGIS + server.
- **Entails:** lift spike-d's `compose.yaml` (PostGIS 16-3.4, named volume, healthcheck) as the
  base; add the P2-chosen triplestore service; add the `server` service; keep the `gdal` sidecar
  for loads (or fold into P6's decision). Env-driven connection config.
- **Done when:** `docker compose up` yields a healthy PostGIS + triplestore + reachable server.
- **Depends on:** P1, P2.

## P4 · CI

- **Goal:** Build/test/lint gate on push.
- **Entails:** Go build + `go test ./...` + golangci-lint; Python lint/test for the extractor
  scaffold; SvelteKit build for the stub; a job that spins compose services for integration tests
  (ties to P5). Runner: GitHub Actions.
- **Done when:** green pipeline on a trivial PR; red on a failing test.
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

## P10 · Pick the exact Noord quarter

- **Goal:** Choose the single backdated 2022 quarter the vertical is built on, and size its permit
  volume.
- **Entails:** reuse `spikes/spike-b/harvest_permits.py` (KOOP SRU, already proven) to count
  kap/verplant omgevingsvergunningen in Noord per 2022 quarter; pick one with enough volume *and*
  an elapsed replant window; sanity-check that those permits' felling dates land in
  `kapenherplant`.
- **Done when:** the quarter is recorded in `IMPLEMENTATION_PLAN.md` §1 (replacing "target: a 2022
  quarter") with the permit count.
- **Depends on:** nothing — pure data analysis, runs parallel to everything.
