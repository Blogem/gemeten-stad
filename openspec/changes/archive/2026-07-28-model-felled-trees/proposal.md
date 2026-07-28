## Why

The coverage derive (P14) links a permit to registry fellings, but the fellings themselves live
**only in PostGIS** — the graph's `gs:Observation` is identity-only, so the audit graph can't say
*which* trees a permit's coverage rests on. That gap forced the `gs:CoveragePeriod` content-key to
approximate the outcome (it hashes the constant Observation IRI, not the felling set), which lets two
genuinely-different findings collide on one period IRI and produces **duplicate open periods** (a real
graph-integrity bug found on the Noord corpus: 59 anchors with two open periods). We fix it at the
root by modelling the felled trees + fellings as first-class graph entities and content-addressing the
Observation by its felling set — the correct Phase-1 finale under `docs/RDF_MODELING.md`'s
best-practice-over-leanness rule.

## What Changes

- **Model felled trees + the felling event as first-class graph entities**:
  - `gs:Tree` (`data:tree/<boomId>`) — the enduring tree, **identity only** (geometry stays in PostGIS).
  - `gs:Felling` event (`data:felling/<kapenherplant-id>`) — `gs:felledTree` → `gs:Tree`, `gs:felledOn`
    (`xsd:date`). Modelled as an event (carries the observed fact + its date) that the audit links to.
- **New bomen → graph load**: the bomen loader also projects **felled** trees + their fellings into the
  graph, through the SHACL gate. Only felled trees are loaded (trees never felled don't enter the graph).
- **Rework the coverage `gs:Observation`**: content-address it by its assigned felling set
  (`data:observation/<zaaknummer>/<felling-set-key>`) and link its members via `gs:includesFelling` →
  `gs:Felling` (a cross-load reference, gated `sh:nodeKind sh:IRI` + `sh:pattern` on the felling
  namespace, like `gs:coversIntervention`). Because the `gs:CoveragePeriod` content-key already hashes
  the Observation IRI, this makes periods version correctly and **supersedes the content-key
  duplicate-open-period bug** (no separate patch needed).
- **Reverses** the earlier leanness decisions "`gs:Observation` is identity-only" (derive-coverage-audit
  D2) and "no bulk registry rows in the graph" — the registry observation layer now belongs in the
  graph. `docs/RDF_MODELING.md` §1 is updated accordingly.

**Non-goals (deferred to Phase 2 — herplantplicht fulfilment):** the **replant layer** as one coherent
unit — `gs:Replanting`, `gs:plantedTree`/`gs:plantedOn`, the felled→replacement tree lineage
(`dct:isReplacedBy`), replacement-tree identities, and the fulfilment `Assessment`. The Phase-1
coverage audit consumes fellings only, and a replant event is useless without a link to the felling it
discharges — so the replant model lands together in Phase 2, additively (the replant data stays in
PostGIS until then). Also out of scope: tree geometry in the graph (stays in PostGIS).

## Capabilities

### New Capabilities

- (none — this extends existing capabilities)

### Modified Capabilities

- `graph-ontology`: add `gs:Tree`, `gs:Felling`, and the properties `gs:felledTree`, `gs:felledOn`,
  `gs:includesFelling`; recast `gs:Observation` from identity-only to a felling-set grouping.
- `graph-shapes`: add core-SHACL `gs:FellingShape` + `gs:TreeShape`; gate `gs:includesFelling` (min 1,
  IRI + felling-namespace pattern) on the matched `gs:Observation`.
- `bomen-load`: additionally project felled trees + fellings into the graph via the SHACL gate.
- `coverage-audit`: the matched `gs:Observation` is content-keyed by its assigned felling set and
  carries `gs:includesFelling`; the `gs:CoveragePeriod` content-key derives correct versioning from the
  Observation IRI (removing the felling-count/set patch and the duplicate-open-period bug).

## Impact

- **New code:** a bomen → graph turtle projection (`load/bomen`), reusing `load/graph.Load`. Rework of
  `derive/coverage` Observation assembly (`assemble.go`, `coverage.go`, `contentkey.go`).
- **Ontology:** `ontology/ontology.ttl` + `ontology/shapes.ttl` gain the tree/felling model.
- **Graph volume:** ~8.2k `gs:Felling` + felled `gs:Tree` nodes enter the graph (identity + felling
  date + relations; geometry stays in PostGIS).
- **Depends on / sequencing:** builds on `derive-coverage-audit` (now archived; its `coverage-audit`,
  `graph-ontology`, `graph-shapes` specs are in main). **Supersedes** derive-coverage-audit's content-key
  follow-up.
- **Load order:** bomen (now → PostGIS + graph fellings) must run before `derive`, so the Observation's
  `gs:includesFelling` references pre-loaded `gs:Felling` IRIs.
- **Phase-2 follow-on:** the replant layer (above) extends this projection + adds the fulfilment audit.
