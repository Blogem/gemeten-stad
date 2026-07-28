## Why

The coverage derive (P14) links a permit to registry fellings, but the fellings themselves live
**only in PostGIS** — the graph's `gs:Observation` is identity-only, so the audit graph can't say
*which* trees a permit's coverage rests on. That gap forced the `gs:CoveragePeriod` content-key to
approximate the outcome (it hashes the constant Observation IRI, not the felling set), which lets two
genuinely-different findings collide on one period IRI and produces **duplicate open periods** (a real
graph-integrity bug found on the Noord corpus: 59 anchors with two open periods). It also blocks
Phase 2 (herplantplicht fulfilment), which needs felled trees and their replants as first-class
entities. Rather than patch the content-key, we model the registry reality properly — this is the
correct Phase-1 finale, done under `docs/RDF_MODELING.md`'s best-practice-over-leanness rule.

## What Changes

- **Model felled trees + their acts as first-class graph entities** (the W3C n-ary/event pattern):
  - `gs:Tree` (`data:tree/<boomId>`) — the enduring tree, **identity only** (geometry stays in PostGIS).
  - `gs:Felling` event (`data:felling/<kapenherplant-id>`) — `gs:felledTree` → `gs:Tree`, `gs:felledOn`
    (`xsd:date`).
  - `gs:Replanting` event (`data:replanting/<kapenherplant-id>`) — `gs:plantedTree` → the replacement
    `gs:Tree`, `gs:plantedOn` (`xsd:date`), `gs:replaces` → the `gs:Felling` it discharges. Modelled as
    an event (not a `replantedBy` edge) because the replant is a dated real-world act, often >1yr after
    the felling, and ~126 fellings are unmet (a `gs:Felling` with no `gs:Replanting`).
- **New bomen → graph load**: the bomen loader also projects **felled** trees + their fellings +
  replants into the graph, through the SHACL gate. Only felled trees are loaded as full entities;
  replacement trees (`boomNieuwId`, mostly never felled) are minted as **bare `gs:Tree` identities**
  referenced by `gs:plantedTree`.
- **Rework the coverage `gs:Observation`**: content-address it by its assigned felling set
  (`data:observation/<zaaknummer>/<felling-set-key>`) and link its members via `gs:includesFelling` →
  `gs:Felling` (a cross-load reference, gated `sh:nodeKind sh:IRI` + `sh:pattern` on the felling
  namespace, like `gs:coversIntervention`). Because the `gs:CoveragePeriod` content-key already hashes
  the Observation IRI, this makes periods version correctly and **supersedes the content-key
  duplicate-open-period bug** (no separate patch needed).
- **Reverses** the earlier leanness decisions "`gs:Observation` is identity-only" (derive-coverage-audit
  D2) and "no bulk registry rows in the graph" — now that this is a production system, the registry
  observation layer belongs in the graph. `docs/RDF_MODELING.md` §1 is updated accordingly.

**Non-goals (Phase 2):** the herplantplicht **fulfilment/timeliness assessment** (was the replant on
time vs. a deadline? the `gs:Assessment`). This change models the registry *facts* only; the audit
*judgment* over them is later.

## Capabilities

### New Capabilities

- (none — this extends existing capabilities)

### Modified Capabilities

- `graph-ontology`: add `gs:Tree`, `gs:Felling`, `gs:Replanting`, and the properties `gs:felledTree`,
  `gs:plantedTree`, `gs:felledOn`, `gs:plantedOn`, `gs:replaces`, `gs:includesFelling`; recast
  `gs:Observation` from identity-only to a felling-set grouping; note dates-as-descriptive-literals.
- `graph-shapes`: add core-SHACL shapes for `gs:Tree`/`gs:Felling`/`gs:Replanting`; gate
  `gs:includesFelling` (min 1, IRI + felling-namespace pattern) on the matched `gs:Observation`.
- `bomen-load`: additionally project felled trees + fellings + replants into the graph via the SHACL
  gate (bare `gs:Tree` identities for replacement trees).
- `coverage-audit`: the matched `gs:Observation` is content-keyed by its assigned felling set and
  carries `gs:includesFelling`; the `gs:CoveragePeriod` content-key derives correct versioning from the
  Observation IRI (removing the felling-count/set patch and the duplicate-open-period bug).

## Impact

- **New code:** a bomen → graph turtle projection (`load/bomen`), reusing `load/graph.Load`. Rework of
  `derive/coverage` Observation assembly (`assemble.go`, `coverage.go`, `contentkey.go`).
- **Ontology:** `ontology/ontology.ttl` + `ontology/shapes.ttl` gain the tree/felling/replanting model.
- **Graph volume:** ~8.2k `gs:Felling` + ~8k `gs:Replanting` + felled/replacement `gs:Tree` nodes enter
  the graph (identity + dates + relations; geometry stays in PostGIS).
- **Depends on / sequencing:** builds on `derive-coverage-audit` (the `coverage-audit`, `graph-ontology`,
  `graph-shapes` capabilities it introduced) — that change archives first, delivering those specs to
  main; this change then modifies them. **Supersedes** derive-coverage-audit's content-key follow-up.
- **Load order:** bomen (now → PostGIS + graph fellings) must run before `derive`, so the Observation's
  `gs:includesFelling` references pre-loaded `gs:Felling` IRIs.
