## Why

The stores exist (Fuseki chosen in P2/Spike E; Postgres/PostGIS from P3) but there is no
graph model to write into them. Nothing downstream — `load`, `derive`, the server, the UI —
can assemble interventions, claims, or audit links until the TBox, the domain vocabulary, the
SHACL load gate, and a validating write path are defined. P8 turns the graph model from
`IMPLEMENTATION_PLAN.md` §3 and its uncertainty/temporal patterns into loadable `.ttl` plus a
load path that refuses malformed data. Spike E has already settled every open modeling risk
(RDF-star annotation form, SHACL-over-RDF-star, named-graph ergonomics) and Spike C has fixed
the activity vocabulary (**verplanten ≡ vellen**), so the model can be committed now.

## What Changes

- **Ontology v0 (TBox)** at `ontology/ontology.ttl`: the `Intervention –locatedAt→ Place`,
  `–partOfProject→ Project` (optional/sparse), `–claims→ Claim –testedAgainst→ Observation`
  spine, plus the derived `AuditLink` and `Assessment` classes. Values stay out of RDF.
- **Uncertainty pattern (RDF-star)**: first-class confidence + evidence on the fuzzy edges
  (`locatedAt`, `AuditLink`) written with the `{| … |}` annotation form, plus the SPARQL-star
  read the server/UI will use — the pattern Spike E proved.
- **Bitemporal + PROV pattern**: stable IRIs; un-stamped immutable facts; valid-time on
  evolving state via the two mechanisms (RDF-star annotation for single facts; state/period
  nodes for `Assessment`/`LegalStatusPeriod` SCD2); transaction-time as PROV run-stamped named
  graphs (`prov:generatedAtTime` in a dedicated `run:_provenance` graph); `Assessment`
  open/close (supersede, never overwrite).
- **SKOS domain vocabulary** at `ontology/vocab.ttl`: a ConceptScheme bootstrapped from
  authoritative sources (TOOI, IMBOR/BOOM, Soortenregister, the regulation) + data enums,
  aligned with `skos:exactMatch` (per-concept lookups, not a bulk thesaurus import). The legal
  top is derived by reading the CVDR regulation texts (not invented); **species are seeded
  city-wide** (distinct `soortnaam` from the `stamgegevens` table `load/bomen`/P7 loads for all
  of Amsterdam — a `SELECT DISTINCT`, not a Datapunt re-fetch), with **grammatical
  plural + Latin `skos:altLabel`s authored for every species** (load-bearing for NER — see the
  Spike C lemmatizer failure), anchored on Spike C's corpus-attested forms for the common set
  and verified against an open Dutch lexicon (OpenTaal/Wiktionary) for the rest. Corpus-mined
  *surface variants* (abbreviations, cultivar phrasings) remain a Phase-2 loop; a second
  intervention type's vocab is an explicit **Phase-4 revisit** (design D8).
- **SHACL shapes** at `ontology/shapes.ttl`: enforce structure + SKOS-backed controlled value
  sets + confidence/caveat presence (core SHACL for structure; `sh:sparql` SPARQL-star for the
  confidence-presence rule, per Spike E).
- **Validating load path**: a Go package the `load` stage uses to POST a candidate graph to the
  Fuseki `…/shacl` endpoint and write it to a run-stamped named graph **only if it conforms** —
  "no half-broken data enters the graph."

## Capabilities

### New Capabilities
- `graph-ontology`: the RDF TBox (classes, properties, relations) plus the RDF-star uncertainty
  convention and the bitemporal/PROV temporal convention that instance data must follow.
- `domain-vocabulary`: the SKOS ConceptScheme seeded from authoritative thesauri, the
  regulation, and data enums, with species plural altLabels and `skos:exactMatch` alignment.
- `graph-shapes`: the SHACL shapes that gate the graph — structure, controlled value sets, and
  confidence/caveat presence.
- `graph-load-gate`: the Go validating write path that validates a candidate graph against the
  shapes via the Fuseki SHACL endpoint and only writes conforming, run-stamped named graphs.

### Modified Capabilities
<!-- None. Existing specs (geo-ingest, geo-load, bomen-ingest, bomen-load, location-resolver)
     are unaffected — P8 adds new capabilities, it does not change their requirements. -->

## Impact

- **New/changed files:** `ontology/ontology.ttl`, `ontology/vocab.ttl`, `ontology/shapes.ttl`
  (the `ontology/` dir currently holds only `.gitkeep`); the `load/graph/` package, which exists
  on main as a one-line `doc.go` stub, fleshed out into the SHACL-gated Fuseki write path +
  shape/ontology embedding; a new `shared.FusekiURL` env helper; unit + integration tests.
- **Depends on:** P2 (Fuseki, done). **P7 (`load/bomen`, on main)** must have run to populate the
  city-wide `stamgegevens` table the species vocab reads. Builds directly on Spike E's
  `shapes.ttl`, `sample.trig`, and the `{| … |}` + `sh:sparql` patterns; reuses the Fuseki
  HTTP/auth conventions in `internal/testdb/fuseki.go` and mirrors the `load/geo`
  orchestrator+gate structure now on main (a `Load(ctx, …)` entrypoint, a `Config{Reset}`, a
  gate that fails loudly, fail-loud `shared.*FromEnv` config).
- **Unblocks:** P9 (something to dump), the `load`/`derive` pipeline stages (the first graph
  writer — geo/bomen populate only the value store), and the server/UI read paths. Namespace
  `http://gemetenstad.nl/ns#` (`gs:`), ids `…/id/`, runs `…/run/`; runtime Fuseki URL from
  `GS_FUSEKI_URL`.
- **Integration tests** run against an isolated Fuseki dataset (P5 harness, `GS_TEST_FUSEKI_URL`),
  never production names.
