## Why

The P14 coverage audit needs a permit's coverage outcome (no-source-found ↔ matched, with an
evolving confidence) to carry **valid-time history** in the graph as a proper state node. Designing
it produced a modeling doctrine, written up in `docs/RDF_STAR_RELATIONSHIPS.md`: **valid-time
versioning belongs on the node that owns a time-bounded relationship — not on an RDF-star annotated
edge** (an asserted triple is timeless, and a single node IRI cannot hold a multi-attribute history).
The correct shape is **one distinct node per period, grouped by a stable series anchor**, with the
writer keeping exactly one open period per anchor.

Two things in main block this:

1. **The graph writer versions only the RDF-star annotation form.** The `graph-load-gate` spec names
   a "state/period node" as evolving, but the shipped writer's open/close only handles
   `<< s p o >> gs:validFrom` annotations — the node-form path was deferred. `gs:AuditLink` (and,
   later, `gs:Assessment`) are node-form state entities and cannot version until this lands.
2. **`load/koop` stamps `gs:validFrom` on the `gs:locatedAt` edge**, mis-modeling a Flavour-1
   refinable edge (location *holds*; its confidence is refined) as valid-time state.

## What Changes

- **Graph writer (`load/graph`) — version state nodes grouped by a series anchor.** Add node-form
  SCD2: a node carrying a plain `gs:validFrom` **and** a `gs:versionOf <anchor>` pointer is a
  **period** in the series identified by `<anchor>`. The writer maintains **exactly one open**
  (no `gs:validTo`) period per anchor: when a new period node appears (a new content-derived IRI), it
  **closes** the prior open period in that series by stamping its `gs:validTo` equal to the new
  period's `gs:validFrom` (contiguous intervals), history retained. An unchanged re-run mints the
  same content-derived IRI and is a true no-op. This extends the change-detection signature,
  `closePriors`/`verifyOpenInvariant`, to node-form periods alongside the existing annotation form.
- **New generic term `gs:versionOf`** (`ontology/ontology.ttl`): a period node → its stable series
  anchor IRI. The writer keys node-form versioning on it; it is domain-agnostic (the anchor is
  whatever a model chooses — for coverage, a dedicated per-permit anchor node the periods hang off).
- **`load/koop` — `gs:locatedAt` becomes a refinable edge.** Drop the `gs:validFrom` from its
  `{| … |}` annotation; it carries `gs:confidence` (+ `gs:caveat`) only. Location is transaction-time
  (latest run authoritative, priors retained in their run graphs); a genuine relocation is a new
  permit entity. **Consequence:** with no `gs:validFrom`, a permit whose *resolution changed* between
  runs is an immutable-content change → the writer surfaces it (skip-and-warn) and re-resolution is
  applied via `--reset`; an in-place transaction-time refinement path for edges is a noted follow-up.
- **Docs.** `docs/RDF_STAR_RELATIONSHIPS.md` (added here) is the reference; update
  `docs/IMPLEMENTATION_PLAN.md` §"Temporal model" to point to it.

## Capabilities

### Modified Capabilities

- `graph-load-gate`: the SCD2 upsert SHALL version **node-form period nodes** grouped by a
  `gs:versionOf` series anchor (one open period per anchor; a new period closes the prior), not only
  RDF-star annotation-form edges.
- `koop-load`: the assembled `gs:locatedAt` edge SHALL NOT carry `gs:validFrom` (Flavour-1 refinable
  edge); the previous "re-resolution opens a new `locatedAt` version" behaviour is removed.

## Impact

- **Code:** `load/graph` (`signature.go`, `write.go` — node-form period detection, close-prior-in-
  series keyed on `gs:versionOf`, invariant across both forms); `ontology/ontology.ttl` (`gs:versionOf`);
  `load/koop/graph.go` (`renderLocatedAtAnnotations` drops `validFrom`). Integration tests for a
  node-form open/close cycle against the isolated Fuseki dataset.
- **Docs:** new `docs/RDF_STAR_RELATIONSHIPS.md`; `docs/IMPLEMENTATION_PLAN.md` temporal-model note.
- **Unblocks:** `derive-coverage-audit` (P14) — its `gs:AuditLink` **period nodes** rely on this
  node-form series versioning. This change is a **prerequisite** for that one.
- **Consumes/keeps stable:** the SHACL gate, run-stamped graphs + PROV, the `Config{Reset}` rebuild
  path, and the existing annotation-form versioning — all unchanged.
