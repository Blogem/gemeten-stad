## 1. Ontology TBox (`ontology/ontology.ttl`)

- [ ] 1.1 Declare namespaces (`gs:`, `data:` = `…/id/`, `run:` = `…/run/`, `prov:`, `skos:`, `sh:`, `xsd:`) and the ontology header
- [ ] 1.2 Define classes: `gs:Intervention`, `gs:Place`, `gs:Project`, `gs:Claim`, `gs:Observation`, `gs:AuditLink`, `gs:Assessment`, `gs:LegalStatusPeriod`, each with `rdfs:label` + `rdfs:comment`
- [ ] 1.3 Define object properties: `gs:locatedAt`, `gs:partOfProject` (optional/sparse), `gs:claims`, `gs:testedAgainst`, with domains/ranges
- [ ] 1.4 Define the uncertainty annotation terms `gs:confidence` (xsd:decimal) and `gs:evidence` (xsd:string), documenting the `{| … |}` write rule in a comment
- [ ] 1.5 Define the temporal terms `gs:validFrom`/`gs:validTo` and document the two mechanisms (RDF-star annotation vs `Assessment`/`LegalStatusPeriod` state node) plus the immutable-facts-stay-unstamped rule
- [ ] 1.6 Define the caveat flag terms (`gs:unresolvedLocation`, `gs:timeMismatch`, `gs:weakLink`, `gs:transplantOrigin`)

## 2. SKOS domain vocabulary (`ontology/vocab.ttl`)

- [ ] 2.1 Fetch and read the two CVDR regulation texts — Bomenverordening 2014 (`https://lokaleregelgeving.overheid.nl/CVDR323217/2`) and the *Compensatie en herplant van bomen* beleidsregel (`https://lokaleregelgeving.overheid.nl/CVDR697591`) — and derive the legal-top concepts from their text (houtopstand, herplantplicht, herplantfonds, monumentale boom, the diameter/stamomtrek classes + equivalence), creating the domain `skos:ConceptScheme` with a `skos:prefLabel` and a `skos:definition`/`skos:scopeNote` traceable to the source article for each
- [ ] 2.2 Model the single felling concept from `boommaatregelBesluit`, with `verplanten`/`rooien`/`kappen`/`kandelaberen` as `skos:altLabel` (no separate Verplanten concept) — the enum and the verplanten≡vellen finding are already pulled in `spikes/spike-c/registry_enum.py` / `parse_activities.py`
- [ ] 2.3 Seed species city-wide: read the distinct species via `SELECT DISTINCT soortnaam FROM stamgegevens WHERE soortnaam IS NOT NULL` against the Postgres table `load/bomen` (P7) already populates for all of Amsterdam (not a Datapunt API re-fetch), and create a SKOS concept per species (singular `skos:prefLabel` + source value as identity). Requires `load/bomen` to have run
- [ ] 2.4 Author grammatical `skos:altLabel`s for the whole species set: the Latin genus for every species, plus the Dutch plural/inflected form for every species whose prefLabel is a Dutch common noun (skip Latin-binomial / non-lexical entries). Anchor the ~20 common species on Spike C's corpus-attested forms (`spikes/spike-c/vocab.py`); verify the rest against an open Dutch lexicon (OpenTaal `github.com/OpenTaal/opentaal-wordlist` or nl.wiktionary `meervoud`) where an entry exists. Also ingest `boomgebreken` distinct values (from the loaded rows' `raw` jsonb) and `gebieden`/CBS place names + codes as concepts, preserving source codes as identity
- [ ] 2.5 Add `skos:exactMatch` alignments for the concepts defined above via per-concept IRI lookups (NOT a bulk thesaurus import) against TOOI waardelijsten (`https://standaarden.overheid.nl/tooi/waardelijsten/`), IMBOR RDF (`https://github.com/Stichting-CROW/imbor`, the BOOM object), and the Nederlands Soortenregister (species); none are cached in the repo. Best-effort — a concept without an easily-resolved source IRI keeps a local IRI now (no re-minted parallel identifiers, no blocker)
- [ ] 2.6 Record the deferred axes in the vocab artifact (a header comment or accompanying note): corpus-mined surface variants are a Phase-2 loop (mines the Noord corpus first), and a second intervention type's vocabulary slices are a Phase-4 revisit (IMPLEMENTATION_PLAN §6). Grammatical plurals are already city-wide in v0 (no inflection gap)

## 3. SHACL shapes (`ontology/shapes.ttl`)

- [ ] 3.1 Author the `gs:InterventionShape` structural constraints in core SHACL (`gs:locatedAt` minCount 1, `sh:class gs:Place`, with `sh:message`) — build on `spikes/spike-e/shapes/shapes.ttl`
- [ ] 3.2 Add the `sh:sparql` confidence-presence constraint (SPARQL-star `FILTER NOT EXISTS { << … >> gs:confidence ?c }`) and the [0,1] range check
- [ ] 3.3 Add controlled-value-set constraints binding species/activity/status/caveat properties to `domain-vocabulary` concepts
- [ ] 3.4 Add the caveat-presence constraints (a non-exact `locatedAt` edge must carry a caveat flag)

## 4. Validating load path (Go, `load/graph/`)

- [ ] 4.1 Flesh out the existing `load/graph/` package (currently a one-line `doc.go` stub on main; sibling of `load/geo`, `load/bomen`): expand `doc.go` to cite the design decisions, and `go:embed` `ontology.ttl`, `vocab.ttl`, `shapes.ttl`
- [ ] 4.2 Add a fail-loud `shared.FusekiURL(os.Getenv)` helper (reads `GS_FUSEKI_URL`), alongside `shared.DatabaseURL`
- [ ] 4.3 Implement the SHACL-validation gate: POST candidate graph + shapes to the Fuseki `…/shacl` endpoint, parse the report for `sh:conforms`, fail loudly on non-conform — reusing the HTTP/auth conventions from `internal/testdb/fuseki.go`
- [ ] 4.4 Implement the run-stamped named-graph write: on conform, POST TriG under a `run:load-…` graph and write the run's `prov:Activity` + `prov:generatedAtTime` into `run:_provenance`; on non-conform, return the violation detail and write nothing
- [ ] 4.5 Expose a `Load(ctx, …) error` orchestrator with `Config{Reset}` mirroring `load/geo` (reset clears+rebuilds run graphs; else additive), callable from the pipeline `load`/`derive` stages (primitive only; full stage assembly is out of scope)

## 5. Tests

- [ ] 5.1 Unit test: the three `.ttl` files parse and load without error (RDF syntax + valid SKOS)
- [ ] 5.2 Unit test: vocab assertions — felling concept has verplanten/rooien/kappen altLabels and no separate Verplanten concept; species coverage matches the distinct city-wide `soortnaamTop` set; every Dutch-common-noun species concept has ≥1 plural altLabel + Latin genus (Latin-binomial/non-lexical entries exempt); the ~20 common species match Spike C's attested forms; a plural surface form maps to the same concept as its singular
- [ ] 5.3 Unit test (table-driven, testify): the SHACL-report parser returns conforms/violation correctly for representative report payloads
- [ ] 5.4 Integration test (isolated Fuseki via P5 harness): well-formed graph → written to a `run:load-…` graph with a `run:_provenance` triple; missing-structure graph → rejected, not written; missing-confidence graph → rejected, not written; out-of-vocab value → rejected
- [ ] 5.5 Integration test: the #3503 mixed-violation case (valid RDF-star edge + a real structural violation) still reports non-conforming and pinpoints the node
- [ ] 5.6 Integration test: SPARQL-star read of `gs:confidence`/`gs:evidence` off a written `locatedAt` edge returns the annotated values

## 6. Verification & docs

- [ ] 6.1 Run `go build ./...` and `go test ./...` (unit) and the integration lane against the isolated Fuseki; all green
- [ ] 6.2 Confirm the done-when criteria: TBox + vocab + shapes load; a well-formed instance passes SHACL; a malformed one is rejected
- [ ] 6.3 Update `docs/PHASE_0_PLAN.md` to mark P8 DONE and note the `ontology/` artifacts + load-path package
