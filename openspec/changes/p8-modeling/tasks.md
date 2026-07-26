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
