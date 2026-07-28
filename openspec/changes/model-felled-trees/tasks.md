## 0. Prerequisite

- [x] 0.1 `derive-coverage-audit` archived (its `coverage-audit`, `graph-ontology`, `graph-shapes`, `koop-load` specs synced to main), so this change's deltas modify the delivered baseline. This change supersedes derive-coverage-audit's content-key follow-up.

## 1. Ontology + shapes (do FIRST)

- [ ] 1.1 In `ontology/ontology.ttl`: add `gs:Tree`, `gs:Felling` classes and `gs:felledTree` (`gs:Felling`→`gs:Tree`), `gs:felledOn` (`xsd:date`), `gs:includesFelling` (`gs:Observation`→`gs:Felling`), each with `rdfs:label` + `rdfs:comment`. Update `gs:Observation`'s comment (felling-set grouping, no longer identity-only).
- [ ] 1.2 In `ontology/shapes.ttl`: add `gs:FellingShape` (`gs:felledTree` min 1 IRI + tree-namespace `sh:pattern`; `gs:felledOn` `xsd:date`) and permissive `gs:TreeShape`; gate matched `gs:Observation`'s `gs:includesFelling` (min 1, IRI + felling-namespace `sh:pattern`). Cross-load refs via `sh:nodeKind`/`sh:pattern`, not `sh:class`.
- [ ] 1.3 Extend `ontology/embed_test.go`: assert the new terms + shape names are present.
- [ ] 1.4 Update `docs/RDF_MODELING.md` §1: the felled-tree/felling observation layer is now first-class in the graph (identity + descriptive dates + relations); geometry/bulk values stay in PostGIS.
- [ ] 1.5 Integration test (isolated Fuseki): a well-formed `gs:Felling` and matched `gs:Observation` conform; a felling missing `gs:felledTree`/`gs:felledOn` and an Observation missing `gs:includesFelling` are each rejected.

## 2. Bomen → graph projection

- [ ] 2.1 In `load/bomen`: assemble turtle for each **felled** `kapenherplant` row — `data:tree/<boomId> a gs:Tree` and `data:felling/<id> a gs:Felling ; gs:felledTree data:tree/<boomId> ; gs:felledOn "<date>"^^xsd:date`. Escape/validate literals + IRIs (mirror `load/koop/graph.go`). No replant projection (Phase 2).
- [ ] 2.2 Write the assembled turtle through `load/graph.Load` (SHACL gate + SCD2), `Reset:false`. Wire it into the bomen load path (runs as part of `load bomen`), before `derive`.
- [ ] 2.3 Unit tests: turtle assembler (felled tree + felling; felling references its tree; date as `xsd:date`; only felled rows projected).
- [ ] 2.4 Integration test (isolated Fuseki + Postgres): seed felled rows; assert the `gs:Tree`/`gs:Felling` nodes; assert an unchanged re-run mints no new run graph.

## 3. Derive: content-addressed Observation linking fellings

- [ ] 3.1 In `derive/coverage`: mint the matched `gs:Observation` as `data:observation/<zaaknummer>/<felling-set-key>` (hash of sorted assigned `gs:Felling` IRIs) carrying `gs:includesFelling` → each assigned felling (`data:felling/<id>`). Point `gs:linksObservation` at it.
- [ ] 3.2 Drop the interim content-key felling-count field: the period content-key derives correct versioning from the (now content-addressed) Observation IRI. Verify `ContentKey` still hashes the Observation IRI.
- [ ] 3.3 Map each assigned candidate `kapenherplant.id` → `data:felling/<id>` IRI so the Observation membership references real, pre-loaded fellings.
- [ ] 3.4 Unit tests: same felling set → same Observation IRI + same period content-key (no-op); changed set (incl. same-count swap, same rounded confidence) → new Observation IRI + new period IRI.

## 4. Idempotency + one-open-period integration

- [ ] 4.1 Integration test (isolated Fuseki + Postgres): matched permit's Observation lists its fellings via `gs:includesFelling`; a changed assigned set opens a new period + closes the prior; assert **exactly one open period per anchor** (the duplicate-open-period regression) — including a matched→matched same-count-different-felling transition across two runs.
- [ ] 4.2 Integration test: unchanged re-run is a true no-op (no new run graph, same Observation + period IRIs).

## 5. Validation + docs

- [ ] 5.1 `go test ./load/bomen/... ./derive/... ./ontology/... ./cmd/pipeline/...` and `go vet ./...` pass; `gofmt` clean; integration lane green with `-p 1`.
- [ ] 5.2 Run the full pipeline on the dev stores (geo, bomen [+graph], graph, koop, derive) and confirm graph open matched periods == `audit_metrics` matched (the 340-vs-281 discrepancy resolved); capture the corrected coverage numbers for `docs/PHASE_1_PLAN.md`.
- [ ] 5.3 Update `docs/IMPLEMENTATION_PLAN.md` §3/§4 to the tree/felling model + content-addressed Observation; note the replant layer (`gs:Replanting`, `dct:isReplacedBy` lineage, fulfilment `Assessment`) as the Phase-2 follow-on.
