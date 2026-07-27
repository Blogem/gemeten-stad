## 1. Reference doc + ontology term

- [ ] 1.1 Add `docs/RDF_STAR_RELATIONSHIPS.md` (edge-vs-node + temporal decision guide) — **done in this change; verify present**.
- [ ] 1.2 Update `docs/IMPLEMENTATION_PLAN.md` §"Temporal model" to reference the doc and state: annotated edges = refinable metadata (transaction-time); valid-time lives on node-form period nodes grouped by a series anchor.
- [ ] 1.3 Add the generic `gs:versionOf` object property to `ontology/ontology.ttl` (period node → stable series anchor IRI), with `rdfs:label` + `rdfs:comment`; extend `ontology/embed_test.go` to assert it loads.

## 2. Writer: node-form period versioning (`load/graph`)

- [ ] 2.1 Extend live-open signature extraction so a **closed** node-form period (a node already carrying `gs:validTo`) is excluded from the open set — the node-form analogue of the annotation branch's `FILTER NOT EXISTS { << … >> gs:validTo }`.
- [ ] 2.2 Add series-close: for each **new** period node (`?p gs:versionOf ?a ; gs:validFrom ?vf`), stamp `gs:validTo = ?vf` on every other open period of the same `?a` across `run:load-*` graphs.
- [ ] 2.3 Extend `verifyOpenInvariant` to enforce exactly one open period per `gs:versionOf` anchor, alongside the annotation-form per-subject invariant.
- [ ] 2.4 Extend `stagingValidFrom` (or its node-form analogue) to read a period's node-triple `gs:validFrom` so the writer never invents the stamp.
- [ ] 2.5 Ensure new period nodes route through the close-in-series path (they arrive as `newIRIs`, not `changed`, because each period has a distinct content-derived IRI).

## 3. Writer tests

- [ ] 3.1 Integration test (isolated Fuseki, `GS_TEST_FUSEKI_URL`): seed one open period for an anchor; re-load identical content → true no-op (no run graph); load a new period (new IRI, same anchor) → prior closed with `validTo = new validFrom`, exactly one open, history retained.
- [ ] 3.2 Integration test: a candidate that would leave two open periods for one anchor fails the invariant with nothing written.
- [ ] 3.3 Keep a synthetic annotation-form open/close test so that path stays covered after `locatedAt` stops exercising it.

## 4. `load/koop`: locatedAt becomes a refinable edge

- [ ] 4.1 In `renderLocatedAtAnnotations`, drop the `gs:validFrom` branch — the `locatedAt` annotation carries `gs:confidence` (+ `gs:caveat`) only.
- [ ] 4.2 Update `load/koop` tests: the assembled `locatedAt` has no `gs:validFrom`; the Intervention is non-evolving; a changed resolution without `--reset` is skip-and-warn while the PostGIS row is upserted.
- [ ] 4.3 Update `load/koop` doc.go comments that reference `locatedAt` valid-time versioning.

## 5. Validation

- [ ] 5.1 `go test ./load/graph/... ./load/koop/... ./ontology/...` and `go vet ./...` pass; `gofmt` clean.
- [ ] 5.2 `openspec validate state-node-versioning --type change --strict` passes.
