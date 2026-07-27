## 0. Prerequisite

- [ ] 0.1 `state-node-versioning` merged (node-form period versioning via `gs:versionOf`, the `gs:versionOf` term, and `locatedAt` no longer valid-time). The coverage period nodes depend on it.

## 1. Ontology + shapes (P8 carry-over — do FIRST)

- [ ] 1.1 In `ontology/ontology.ttl`: recast `gs:AuditLink` as the per-permit coverage **anchor** (update its comment); add `gs:coversIntervention` (`gs:AuditLink` → `gs:Intervention`), the `gs:CoveragePeriod` class, `gs:linksObservation` (`gs:CoveragePeriod` → `gs:Observation`), `gs:granularity`, and `gs:noSourceFound`, each with `rdfs:label` + `rdfs:comment`.
- [ ] 1.2 In `ontology/shapes.ttl`: add `gs:AuditLinkShape` (anchor: `gs:coversIntervention` minCount 1, `sh:class gs:Intervention`) and `gs:CoveragePeriodShape` (`gs:versionOf` minCount 1 `sh:class gs:AuditLink`; `gs:validFrom` + `gs:evidence` present; `sh:xone` of matched [`gs:linksObservation` + `gs:confidence`] vs no-source [`gs:noSourceFound true`]; `gs:confidence` in `[0,1]`; `gs:granularity` `sh:in` (`gs:address gs:postcode gs:buurt`)).
- [ ] 1.3 Extend `ontology/embed_test.go`: ontology + shapes parse/load; the new terms + both shapes present.
- [ ] 1.4 Integration test (isolated Fuseki): a well-formed anchor + matched period and a well-formed no-source period conform; an anchor without `gs:coversIntervention`, a neither-branch period, an out-of-range confidence, and a period missing `gs:versionOf`/`gs:evidence` are each rejected.

## 1b. Publication date in the graph (`dct:available`) — do before §2 (derive reads it)

- [ ] 1b.1 In `load/koop/graph.go`: emit `dct:available "YYYY-MM-DD"^^xsd:date` on each besluit `gs:Intervention` (from `Publication.Available`), reusing Dublin Core (`dct:` prefix); no `gs:validFrom`/`gs:validTo`. Update the koop graph unit test (`load/koop/graph_test.go`) to assert the triple + no valid-time.
- [ ] 1b.2 In `ontology/shapes.ttl`: extend the Intervention structural shape to gate `dct:available` (`sh:path dct:available ; sh:minCount 1 ; sh:maxCount 1 ; sh:datatype xsd:date ; sh:message …`). Add `dct:available` to the `ontology/embed_test.go` shapes markers.
- [ ] 1b.3 In `docs/RDF_MODELING.md` §1: scope the "no literals" rule to *quantitative + geometry* values, and record that descriptive-metadata dates (via `dct:`) are legitimate graph literals — with the Intervention-vs-Observation teachable line (per design D10).
- [ ] 1b.4 Integration test (isolated Fuseki): an Intervention with a well-formed `dct:available` conforms; one missing it, and one with a non-`xsd:date` value, are each rejected.

## 2. Candidate generation over the registry

- [ ] 2.1 Enumerate besluit `Intervention`s from the **graph** (SPARQL), reading `dct:available` (publication date) + `gs:locatedAt`; read the buurt code + `resolved_geom`/`resolved_tier` as **values** from `koop_publications`, joined by zaaknummer.
- [ ] 2.2 Query `kapenherplant` for **individual** felled rows in that buurt with `kapmaatregelDatumUitgevoerd` in `[publication, +3yr]` (id, boomId, felling date, `resolvedGeom`). No `datumVergunningVerleend` grouping.
- [ ] 2.3 Per candidate felling, compute metric distance `ST_Distance(ST_Transform(k."resolvedGeom",28992), $point)` (registry 4326 → 28992); point-less permits skip it.
- [ ] 2.4 Unit tests: windowing (before-publication excluded) + SRID-aligned distance (known-distance pair).

## 3. Scoring + exclusive felling assignment

- [ ] 3.1 Implement the place-led scorer (place 0.50 floor graduated by proximity → tier; time publication→felling gap; count registry vs permit [unknown Phase 1 → +0, `countUnknown`]; ambiguity). Adapt `spikes/spike-b/match_rate.py::confidence`; clamp `[0,1]`, 2dp; τ=0.60.
- [ ] 3.2 Implement **exclusive assignment**: gather all permit↔felling pairs, assign each felling to its single best permit (deterministic tie-break); a permit's Observation = its assigned set.
- [ ] 3.3 Golden unit tests: buurt-floor buckets match Spike B; proximity buckets; contested felling → one permit; multi-tree permit; assignment stable across runs.

## 4. Anchor + period assembly + PostGIS

- [ ] 4.1 Compute the outcome **content-key** (hash of state/Observation/rounded-confidence/granularity/sorted-caveats, excluding timestamps); unit-test it (same outcome → same key; distinct outcomes → distinct keys).
- [ ] 4.2 Emit the per-permit anchor `data:auditlink/<zaaknummer> a gs:AuditLink ; gs:coversIntervention <intervention>` (write-once).
- [ ] 4.3 Matched path: mint `data:observation/<zaaknummer> a gs:Observation`; assemble `data:auditlink/<zaaknummer>/<key> a gs:CoveragePeriod ; gs:versionOf <anchor> ; gs:validFrom <run-time> ; gs:linksObservation <obs> ; gs:confidence …; gs:granularity <tier> ; [gs:caveat gs:weakLink|countUnknown] ; gs:evidence …; prov:wasDerivedFrom …`.
- [ ] 4.4 No-source path: assemble the period with `gs:noSourceFound true` + `gs:evidence` (searched buurt+window); no Observation.
- [ ] 4.5 Create the slim `audit_metrics` PostGIS table (schema-qualified, staging + upsert + `--reset` drop): `zaaknummer`, `matched`, `assigned_felling_ids`, `assigned_felling_count`, `candidate_count`, `nearest_dist_m`, `run_id`. No confidence/granularity/caveat/geometry columns.
- [ ] 4.6 Unit tests on the assembler (anchor + matched/weak/no-source periods) and the `audit_metrics` upsert.

## 5. Wiring + idempotency

- [ ] 5.1 Add `runCoverageDerive` and a `deriveRegistry []deriveSource` in `cmd/pipeline`, replacing the `derive` stub; resolve sources like `loadRegistry`.
- [ ] 5.2 Write assembled turtle through `load/graph.Load` (SHACL gate + node-form series SCD2); persist `audit_metrics` in the same run.
- [ ] 5.3 Registry test asserting `deriveRegistry` names (`["coverage"]`) mirroring `load_sources_test.go`.
- [ ] 5.4 Integration test (isolated Fuseki + Postgres, with `state-node-versioning` merged): seed permits + fellings covering a strong match, a below-τ weak link, a contested felling (exclusive), a multi-tree permit, and a no-source permit; assert the anchor + period nodes + `audit_metrics` rows; assert an unchanged re-run is a no-op (same content-key, no new run graph); assert a newly-appearing felling opens a matched period and closes the prior no-source period; assert a malformed period is rejected.

## 6. Validation + docs

- [ ] 6.1 `go test ./derive/... ./cmd/pipeline/... ./ontology/...` and `go vet ./...` pass; `gofmt` clean.
- [ ] 6.2 Update `docs/PHASE_1_PLAN.md` (mark P14 landed; observed link rates + confidence distribution vs Spike B, split by resolution tier) and `docs/IMPLEMENTATION_PLAN.md` §4 to the settled anchor+period + per-felling-assignment form.
- [ ] 6.3 Confirm keyless-besluit handling (zaaknummer `""`): the audit iterates the Interventions the graph holds; verify P13's keyless permits either have no Intervention or a synthesized key.
