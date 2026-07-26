## 1. Ontology TBox (`ontology/ontology.ttl`)

- [x] 1.1 Declare namespaces (`gs:`, `data:` = `…/id/`, `run:` = `…/run/`, `prov:`, `skos:`, `sh:`, `xsd:`) and the ontology header
- [x] 1.2 Define classes: `gs:Intervention`, `gs:Place`, `gs:Project`, `gs:Claim`, `gs:Observation`, `gs:AuditLink`, `gs:Assessment`, `gs:LegalStatusPeriod`, each with `rdfs:label` + `rdfs:comment`
- [x] 1.3 Define object properties: `gs:locatedAt`, `gs:partOfProject` (optional/sparse), `gs:claims`, `gs:testedAgainst`, with domains/ranges
- [x] 1.4 Define the uncertainty annotation terms `gs:confidence` (xsd:decimal) and `gs:evidence` (xsd:string), documenting the `{| … |}` write rule in a comment
- [x] 1.5 Define the temporal terms `gs:validFrom`/`gs:validTo` and document the two mechanisms (RDF-star annotation vs `Assessment`/`LegalStatusPeriod` state node) plus the immutable-facts-stay-unstamped rule
- [x] 1.6 Define the caveat flag terms (`gs:unresolvedLocation`, `gs:timeMismatch`, `gs:weakLink`, `gs:transplantOrigin`)

## 2. SKOS domain vocabulary (`ontology/vocab.ttl`)

- [x] 2.1 Fetch and read the two CVDR regulation texts — Bomenverordening 2014 (`https://lokaleregelgeving.overheid.nl/CVDR323217/2`) and the *Compensatie en herplant van bomen* beleidsregel (`https://lokaleregelgeving.overheid.nl/CVDR697591`) — and derive the legal-top concepts from their text (houtopstand, herplantplicht, herplantfonds, monumentale boom, the diameter/stamomtrek classes + equivalence), creating the domain `skos:ConceptScheme` with a `skos:prefLabel` and a `skos:definition`/`skos:scopeNote` traceable to the source article for each
- [x] 2.2 Model the single felling concept from `boommaatregelBesluit`, with `verplanten`/`rooien`/`kappen`/`kandelaberen` as `skos:altLabel` (no separate Verplanten concept) — the enum and the verplanten≡vellen finding are already pulled in `spikes/spike-c/registry_enum.py` / `parse_activities.py`
- [x] 2.3 Seed species city-wide: read the distinct species from the Postgres table `load/bomen` populates for all of Amsterdam (not a Datapunt API re-fetch), and create a SKOS concept per species. **Note:** `soortnaam` is Latin binomial+cultivar (1662 values, no Dutch nouns); species concepts are keyed at the `soortnaamTop` genus level (~185, Dutch common noun), with `soortnaam` cultivars as `skos:broader` children (user-approved two-level hierarchy). Requires `load/bomen` to have run
- [x] 2.4 Author grammatical `skos:altLabel`s: the Latin genus for every species, plus the Dutch plural for every species whose prefLabel is a Dutch common noun (Latin-only tops exempt). Anchored the ~25 Dutch-common-noun tops on Spike C's forms; verified against OpenTaal (`github.com/OpenTaal/opentaal-wordlist`). Ingested the `boomAanwezigheid` status enum (consumed by the `gs:status` shape). **Places (gebieden + CBS) are NOT seeded as concepts** (user decision): a place's meaning is its geometry + admin hierarchy, authoritative in PostGIS; places live as `gs:Place` instance nodes keyed by code and the agent queries the value store directly (Phase 2) — a SKOS shadow adds nothing. **`boomgebreken` is NOT a stamgegevens field — it is the separate un-ingested `gebrekregistratie` dataset (load/bomen/P7 gap), deferred.** All documented in the vocab header + the amended domain-vocabulary spec
- [x] 2.5 `skos:exactMatch` alignment — best-effort pass. TOOI/IMBOR (bulk LinkedData zips)/Soortenregister were not cheaply per-concept-resolvable in this pass, so per D7 concepts keep local IRIs (no re-minted parallel identifiers). Legal concepts carry `dct:source` to the exact CVDR articles (authoritative traceability); alignment IRIs deferred and documented in the vocab header
- [x] 2.6 Record the deferred axes in the vocab artifact header: corpus-mined surface variants (Phase-2, Noord corpus first), a second intervention type's vocab (Phase-4), boomgebreken/gebrekregistratie (P7 loader gap), exactMatch alignment. Grammatical plurals are already city-wide in v0 (no inflection gap)

## 3. SHACL shapes (`ontology/shapes.ttl`)

- [x] 3.1 Author the `gs:InterventionShape` structural constraints in core SHACL (`gs:locatedAt` minCount 1, `sh:class gs:Place`, with `sh:message`) — build on `spikes/spike-e/shapes/shapes.ttl`. **Proven on live Fuseki 5.5.0.**
- [x] 3.2 Add the `sh:sparql` confidence-presence constraint (SPARQL-star `FILTER NOT EXISTS { << … >> gs:confidence ?c }`) and the [0,1] range check. **Proven live (presence + range reject).**
- [x] 3.3 Add controlled-value-set constraints binding species/activity/status to `domain-vocabulary` concepts (`sh:node` + `skos:inScheme` via `targetSubjectsOf`); caveat values controlled by an `sh:sparql` `IN` check on the four terms. **Proven live (in-vocab pass, out-of-vocab reject).**
- [x] 3.4 Add the caveat-presence constraint (a non-exact `locatedAt` edge — `gs:confidence < 1.0` — must carry a `gs:caveat`; exact edges = 1.0 need none). **v0 decision (Spike E did not probe caveat shapes); proven live.**

## 4. Validating load path (Go, `load/graph/`)

- [x] 4.1 Flesh out the `load/graph/` package: `doc.go` cites the design decisions. The three `.ttl` are embedded via the `ontology` package (`ontology/embed.go` — `go:embed` can't reach a sibling dir, so the embed lives with the canonical files and `load/graph` imports `ontology.Ontology/Vocab/Shapes`)
- [x] 4.2 Add a fail-loud `shared.FusekiURL(os.Getenv)` helper (reads `GS_FUSEKI_URL`), alongside `shared.DatabaseURL`
- [x] 4.3 Implement the SHACL-validation gate: POST candidate + shapes to the Fuseki `…/shacl` endpoint, parse `sh:conforms`, fail loudly on non-conform. **Merge-vocab recipe** (ontology+vocab+candidate in one scratch graph) so controlled-value checks see each concept's `skos:inScheme` — cross-graph validation cannot (proven). Reuses `internal/testdb/fuseki.go` HTTP/auth conventions
- [x] 4.4 Implement the run-stamped named-graph write: on conform, POST under a `run:load-…` graph and write the run's `prov:Activity` + `prov:generatedAtTime` into `run:_provenance`; on non-conform, return the violation detail and write nothing
- [x] 4.5 Expose `Load(ctx, fusekiURL, candidate, Config{Reset}) error` mirroring `load/geo` (reset clears run graphs; else additive), wired into `cmd/pipeline` as the `graph` load source (primitive only; full stage assembly out of scope)

## 5. Tests

- [x] 5.1 The three `.ttl` parse+load without error — asserted in the integration lane (loaded into Fuseki; parse errors surface as load errors) + a Go embedded-artifact smoke test; the rdflib SKOS check was run during authoring
- [x] 5.2 Vocab assertions (integration SPARQL, `TestLoadReferenceModelAndVocab`): felling concept has verplanten/rooien/kappen altLabels + no separate Verplanten concept; species iep→iepen+Ulmus; a plural (essen) resolves to the same concept as its singular (es); no place concepts. (Full soortnaamTop-coverage + every-Dutch-noun-has-plural were verified via rdflib during authoring — Go has no Turtle parser)
- [x] 5.3 Unit test (table-driven, testify): the SHACL-report parser returns conforms/violation correctly for representative report payloads (`shacl_test.go`)
- [x] 5.4 Integration test (`load_integration_test.go`): well-formed graph → written to a `run:load-…` graph with a `run:_provenance` triple; missing-structure / missing-confidence / out-of-vocab → rejected, not written. **All pass live against Fuseki 5.5.0.** NB: runs against a SHACL-enabled dataset via `GS_TEST_FUSEKI_URL` — runtime `dbType=mem` datasets lack `/shacl` (405); provisioning an isolated SHACL-enabled test dataset is a small P5-harness/infra follow-up
- [x] 5.5 Integration test: the #3503 mixed case (valid RDF-star edge + real structural violation) still reports non-conforming — proven live
- [x] 5.6 Integration test: SPARQL-star read of `gs:confidence`/`gs:evidence` off a written `locatedAt` edge returns the annotated values — proven live

## 6. Verification & docs

- [x] 6.1 `go build ./...` clean; `go test ./...` (unit) all green; the integration lane (`-tags integration`) all green against a live Fuseki 5.5.0
- [x] 6.2 Done-when criteria confirmed: TBox + vocab + shapes load; a well-formed instance passes SHACL and is written with provenance; malformed ones (missing structure/confidence, out-of-vocab, #3503) are rejected and not written
- [x] 6.3 Update `docs/PHASE_0_PLAN.md` to mark P8 DONE and note the `ontology/` artifacts + load-path package
