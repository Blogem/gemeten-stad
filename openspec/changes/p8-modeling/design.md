## Context

The triple store is Apache Jena Fuseki 5.5.0 (P2/Spike E), running with `unionDefaultGraph`
on, a SHACL endpoint at `…/shacl`, and RDF-star + SPARQL-star support. Spike E
(`spikes/spike-e/`) is the direct antecedent: it proved the four load-bearing modeling risks
and left reusable artifacts — `shapes/shapes.ttl` (core structure + `sh:sparql` confidence
rule), `data/sample.trig` (run-stamped named graphs + `run:_provenance`), and the SPARQL-star
read. P8 promotes those probes into the committed v0 model under `ontology/` plus a Go load
path. Spike C fixed the activity vocabulary (**verplanten ≡ vellen**; a `skos:altLabel`, not a
concept) and flagged that the `nl_core_news_md` lemmatizer mis-normalizes botanical plurals, so
species concepts must carry plural surface forms.

The value-store loaders now on main (`load/geo` — BAG/gebieden/CBS; `load/bomen` —
kapenherplant/stamgegevens) establish the repo's loader convention this change follows: loaders
live at repo root as `load/<source>/`, expose a `Load(ctx, …) error` orchestrator with a
`Config{Reset}`, end in **sanity gates that fail loudly (non-zero exit)**, resolve config through
fail-loud `shared.*(os.Getenv)` helpers (`shared.DatabaseURL`, `shared.RawDataPath`), and carry a
`doc.go` citing their design decisions. They also encode the bitemporal stance (never
overwrite; soft-delete / append voorkomens). **They write only to the value store — none write
to the graph, so this change's `load/graph` is the first graph writer** (it already exists on
main as a one-line `doc.go` stub: "Package graph writes entities and confidence-annotated edges
into the triplestore" — P8 fleshes it out). `load/bomen` loads `stamgegevens` into Postgres with
a promoted `soortnaam` column plus a `raw` jsonb retaining every source field, so the city-wide
species list is a `SELECT DISTINCT` away (see D7). The Go pipeline stubs a `load` subcommand
(`cmd/pipeline/main.go`) with a `--reset` flag and Fuseki HTTP/auth conventions in
`internal/testdb/fuseki.go`; the compose stack already defines `GS_FUSEKI_URL` (runtime) and the
P5 harness uses `GS_TEST_FUSEKI_URL`. Namespaces are settled: `gs:` =
`http://gemetenstad.nl/ns#`, ids `http://gemetenstad.nl/id/`, runs `http://gemetenstad.nl/run/`
(no hyphen — the RDF base is `gemetenstad.nl`; note the Spike E probes used the hyphenated
`gemeten-stad.nl`, which P8 supersedes when it promotes them into the committed model).

## Goals / Non-Goals

**Goals:**

- A loadable, coherent v0 graph model — `ontology.ttl` (TBox), `vocab.ttl` (SKOS), `shapes.ttl`
  (SHACL) — that loads into Fuseki without error.
- A documented, exemplified uncertainty pattern (RDF-star `{| … |}` + SPARQL-star read) and
  bitemporal/PROV pattern (stable IRIs, valid-time on evolving state, transaction-time as
  run-stamped named graphs).
- A Go validating load path: validate a candidate graph against the shapes via the Fuseki SHACL
  endpoint; write it to a run-stamped named graph only if it conforms; reject otherwise.
- A well-formed instance passes SHACL; a malformed one (missing structure OR missing
  confidence annotation OR out-of-vocab value) is rejected — proven by tests.

**Non-Goals:**

- Corpus-mined altLabels (Phase-2 feedback loop). v0 seeds from sources + enums only.
- The full `load`/`derive` stage logic — P8 provides the model + the write/validate primitive
  the stages call, not the ingestion or derivation pipelines.
- Wholesale import of the TOOI/IMBOR/Soortenregister thesauri. "Only the slices we touch" = the
  concept *kinds* the tree vertical references: tree species, IMBOR's BOOM object + its
  management-measure concepts, the TOOI document/rubriek types the kap bekendmakingen are tagged
  with, and the named regulation concepts (§ D7) — each aligned to its source IRI by
  `skos:exactMatch`, not re-minted. **Species are seeded city-wide** (all distinct
  `stamgegevens.soortnaamTop` across Amsterdam, not just the Noord corpus — see D7), because that
  list is small and bounded and doing it now removes a Phase-4 gap. The thesauri themselves are
  NOT bulk-imported (D7): the Soortenregister is the entire Dutch biodiversity register (tens of
  thousands of non-tree taxa), IMBOR is a full public-space model, TOOI is many waardelijsten —
  importing them whole would swamp both the graph and the NER matcher with false-match noise.
- PDOK Locatieserver anything (deliberately local-BAG only).
- The PostgreSQL SCD2 DDL for the value store — the temporal convention is documented here; the
  Postgres tables land with the `load`/value-store work item.

## Decisions

**D1 — One `ontology/` dir, three `.ttl` files, split by concern.** `ontology.ttl` = TBox
(classes, object/datatype properties, the uncertainty & temporal annotation _terms_);
`vocab.ttl` = SKOS instance data (ConceptSchemes, concepts, labels, `exactMatch`); `shapes.ttl`
= SHACL NodeShapes. _Alternative:_ one merged file — rejected: the load path loads vocab and
shapes for different purposes (vocab is data written to the graph; shapes are posted to the
SHACL endpoint), and mixing SKOS data with SHACL shapes in one graph muddies the SHACL target.

**D2 — Confidence is written with the `{| … |}` annotation form, never bare `<< … >>`.**
Spike E proved the annotation form asserts the base edge _and_ stores the annotation, so core
SHACL still sees the structural edge; the bare form does not assert the base edge. Confidence +
evidence live on `locatedAt` and `AuditLink`. Read back via SPARQL-star. This is a hard model
rule, encoded in the shapes' `sh:sparql` presence check.

**D3 — Confidence-presence and caveat rules use `sh:sparql`, structure uses core SHACL.** Core
SHACL cannot path into a quoted triple (Spike E Finding 2). Structure (`locatedAt` minCount/
class, required types) is core `sh:property`; "every fuzzy edge carries `gs:confidence`" is a
`sh:sparql` `FILTER NOT EXISTS { << … >> gs:confidence ?c }` constraint. Controlled value sets
(species, activity, status, caveat vocab) are core SHACL `sh:in`/`sh:class` against SKOS
concepts.

**D4 — Two temporal mechanisms, chosen by shape.** (a) RDF-star statement annotation
(`validFrom`/`validTo` on a single evolving fact, e.g. a legal-status or location link that
also carries confidence); (b) a state/period node (`Assessment`, `LegalStatusPeriod`) — the
SCD2 fluent pattern — when several attributes move together and periods are queried as objects.
Immutable facts (publication date, felling date) stay un-stamped. `Assessment` is opened/closed
by writing a new node and stamping `validTo` on the prior — never overwritten.

**D5 — Transaction-time = PROV run-stamped named graphs.** Each `load`/`derive` run writes into
its own `run:load-…` named graph; run metadata (`prov:Activity`, `prov:generatedAtTime`) lives
in a dedicated `run:_provenance` named graph, not the default graph (which `unionDefaultGraph`
shadows). The load path is the component that mints the run graph and its provenance triple.

**D6 — The load gate is HTTP, in Go, no in-process JVM, structured like the other loaders.** A
new `load/graph/` package (sibling of `load/geo` and `load/bomen`) holds a Fuseki client with a
`Load(ctx, …) error` orchestrator and a `Config{Reset}`, mirroring `load/geo`: it (1) POSTs the
candidate graph + `shapes.ttl` to `…/shacl?graph=…` and parses the `sh:conforms` result — this
SHACL check **is** the loud-failing gate, the graph analogue of `load/geo/gates.go`; (2) on
conform, POSTs TriG to `…/data` under the run graph. The `.ttl` files are `go:embed`-ed so the
binary is self-contained. The runtime dataset URL resolves through a fail-loud
`shared.FusekiURL(os.Getenv)` helper (new, alongside `shared.DatabaseURL`) reading `GS_FUSEKI_URL`;
integration tests use `GS_TEST_FUSEKI_URL` via the P5 harness. Reuse the request/auth conventions
from `internal/testdb/fuseki.go`. _Alternative:_ shell out to the Jena CLI — rejected: adds a JVM
dependency to the Go pipeline; the CLI stays the offline double-check only (as in Spike E).
_Alternative:_ put the Fuseki client in `internal/` — rejected: the value-store loaders live at
`load/<source>`, so the graph writer sits at `load/graph` for symmetry (promoted to a shared
package only once the server also needs a read client — not pre-built).

**D7 — Vocabulary is seeded, not invented; identity stays in codes.** Import only touched
slices, align to source IRIs with `skos:exactMatch`. Concretely, where each part comes from:

- **Legal top (regulation).** Fetch and read the two CVDR texts —
  Bomenverordening 2014 (`https://lokaleregelgeving.overheid.nl/CVDR323217/2`) and the
  *Compensatie en herplant van bomen* beleidsregel (`https://lokaleregelgeving.overheid.nl/CVDR697591`)
  — and derive the legal concepts from them (houtopstand, herplantplicht, herplantfonds,
  monumentale boom, the diameter/stamomtrek classes and their equivalence), not from memory.
  These are not cached in the repo; they must be fetched at build time.
- **Species (city-wide) + activities.**
  - **Concepts: all distinct species across Amsterdam** — a `SELECT DISTINCT soortnaam FROM
    stamgegevens WHERE soortnaam IS NOT NULL` against the Postgres table `load/bomen` (P7)
    already populates city-wide (a few hundred values), not a re-fetch from the Datapunt API.
    This is the whole-city species vocab, not a Noord slice. (The column is `soortnaam`; a
    coarser grouping via `soortnaamTop` is available in the row's `raw` jsonb if wanted.) This
    makes the vocab's species-seeding depend on `load/bomen` having run.
  - **Grammatical plural/inflected + Latin `altLabel`s for EVERY species, authored once now.**
    The species list is small and stable, so getting inflections right up front is cheap
    insurance — a *missing* plural silently loses tree counts, a wrong one is a harmless
    non-matching label, so doing all of them is the safer default. These are authored (LLM +
    Dutch morphology: consonant doubling es→essen/den→dennen, -s→-z els→elzen, the -en/-s
    choice) and **verified against an open Dutch lexicon** — OpenTaal
    (`github.com/OpenTaal/opentaal-wordlist`) or nl.wiktionary `meervoud` — where an entry
    exists. The ~20 common species that appear in permit prose keep Spike C's corpus-attested
    forms (`spikes/spike-c/vocab.py`) as the gold set. Entries that are Latin binomials or
    non-lexical strings get no generated plural (Latin form only); compound Dutch names
    pluralize on the head noun. The result is a checked-in artifact, not regenerated each build.
    The **Nederlands Soortenregister supplies name synonyms/variants, not grammatical plurals**,
    so it is an alignment target (D7 `exactMatch`), not the plural source.
  - The felling/replant activity concepts likewise promote from `spikes/spike-c/vocab.py`.
- **Data enums.** The `boommaatregelBesluit` enum was already pulled city-wide by
  `spikes/spike-c/registry_enum.py` (→ single `Vellen (boom verwijderen)` value, no Verplanten).
  `soortnaam` distinct values come from the loaded `stamgegevens` Postgres table (above);
  `boomgebreken` distinct values come from the loaded rows' `raw` jsonb (`load/bomen` retains
  every source field there), falling back to the Datapunt API only if the field was not landed.
  gebieden/CBS names+codes come from the P6 geo load.
- **`skos:exactMatch` alignment targets** (fetched, not cached): TOOI waardelijsten
  (`https://standaarden.overheid.nl/tooi/waardelijsten/`), IMBOR RDF
  (`https://github.com/Stichting-CROW/imbor`, the BOOM object), and the Nederlands
  Soortenregister for species IRIs. v0 aligns the species/activity/legal concepts it actually
  defines — a per-concept IRI lookup, not a bulk import; concepts without an easily-resolved
  source IRI carry a local IRI now and gain the `exactMatch` when the alignment is cheap — a
  lean, best-effort pass, not a blocker.

**Species concepts carry both singular and plural/inflected `skos:altLabel`s** (iep/iepen,
es/essen, populier/populieren, els/elzen + Latin genus) for the common set — load-bearing for
the NER EntityRuler.

**D8 — City-wide species now; the rest of the vocab is revisited at Phase 4.** Species are seeded
for the whole city up front (D7) because it is cheap and future-proofs scale. Everything else in
v0 is already Amsterdam-wide (the regulation is a city ordinance; the enums and gebieden/CBS were
pulled/loaded city-wide) or national (TOOI/IMBOR/Soortenregister), so v0 is *not* Noord-limited
in its concept coverage. Grammatical plurals are authored for every city-wide species now
(D7), so there is no whole-city *inflection* gap to revisit. What remains deferred is: (a)
**corpus-mined surface variants** — abbreviations, misspellings, "t.h.v.", cultivar phrasings —
which genuinely need the permit text and are the Phase-2 mining loop's job; it runs against the
*Noord* backfill corpus first, so that variant coverage reaches city-wide richness only once the
whole-city corpus is mined; and (b) at **Phase 4** (IMPLEMENTATION_PLAN §6 — "generalise,
productionize & pursue hidden data") a **second intervention type** (e.g. EV-charging
verkeersbesluiten — the seam test) needs its own vocabulary slices (activities, object types,
its regulation), which v0 deliberately does not model. Called out here so the scale-up is a
known, planned revisit rather than a surprise.

## Risks / Trade-offs

- **[SHACL-over-RDF-star edge cases beyond what Spike E probed]** → Spike E validated the exact
  `{| … |}` + `sh:sparql` pattern on Fuseki 5.5.0 including the #3503 mixed-violation case;
  integration tests re-run accept + both reject paths against a live isolated Fuseki, so a Jena
  upgrade regression surfaces in CI, not production.
- **[`unionDefaultGraph` shadows the stored default graph]** → provenance lives in a dedicated
  named graph (D5); the load path never relies on the default graph for reads.
- **[Vocabulary scope creep — importing too much of TOOI/IMBOR]** → the spec bounds v0 to the
  slices we touch + enums + the legal top; corpus mining is an explicit Non-Goal deferred to
  Phase 2, so "living vocab" growth doesn't leak into this change.
- **[Species altLabel coverage gaps silently lose tree counts]** → a test asserts the common
  Amsterdam species each carry ≥1 plural altLabel; the shapes require species values resolve to
  a known concept, so an unrecognized species surfaces as a load rejection rather than a silent
  miss.
- **[Model churn as `load`/`derive`/server are built on top]** → greenfield POC, no
  backwards-compat obligation (CLAUDE.md); v0 is deliberately the smallest model §3 needs and is
  changed in place when a real downstream need appears.
