## Context

The P14 coverage derive (`derive-coverage-audit`) links a permit to registry fellings, but keeps the
fellings **only in PostGIS** — the graph's `gs:Observation` is identity-only (one mutable node per
permit, derive-coverage-audit D2). A real-corpus run exposed the cost: the `gs:CoveragePeriod`
content-key hashes the (constant) Observation IRI rather than the assigned felling set, so two
different outcomes with the same rounded confidence collide on one period IRI and the writer re-writes
it into a second run graph — **59 anchors with two open periods** (a graph-integrity bug). Separately,
Phase 2 (herplantplicht fulfilment) needs felled trees and their replants as first-class entities.
Rather than patch the content-key, we model the registry reality — the correct Phase-1 finale under
`docs/RDF_MODELING.md`'s best-practice-over-leanness rule.

Data checked on the Noord corpus: `boomId ≠ boomNieuwId` (a replacement is a **new** distinct tree);
felling and replant are separate acts, dates often >1yr apart; ~126 fellings are unmet (felled, no
replant yet); replacement-then-refelled chains are rare (52/7223).

## Goals / Non-Goals

**Goals:**

- Model felled trees + their acts as first-class graph entities: `gs:Tree`, `gs:Felling` event,
  `gs:Replanting` event, with the felled→replacement lineage and both dates.
- Load felled trees (+ their fellings/replants) into the graph via the SHACL gate (a bomen → graph
  projection); reference replacement trees as bare identities.
- Rework the coverage `gs:Observation` to be content-addressed by its assigned felling set and to link
  its members via `gs:includesFelling`, which makes period versioning correct (one open period per
  anchor) and supersedes the content-key duplicate-open-period bug.

**Non-Goals (Phase 2):**

- The herplantplicht **fulfilment / timeliness assessment** (was the replant on time vs. a deadline?
  the `gs:Assessment`). We model the registry *facts*; the *judgment* over them is later.
- Loading non-felled trees; tree geometry in the graph (stays in PostGIS).

## Decisions

### D1 — Replant is an event (`gs:Replanting`), not a `replantedBy` edge

The replant is a dated real-world act with its own participants (the new tree) and, in Phase 2,
attributes (timeliness vs. a deadline). A relationship carrying its own attributes is an **n-ary
relation**, which RDF models as a node (the W3C n-ary-relations pattern) — not a binary edge. So we
mint `gs:Replanting` (`gs:plantedTree`, `gs:plantedOn`, `gs:replaces` → the `gs:Felling`), symmetric
with `gs:Felling`. _Alternative rejected — an annotated `A gs:replantedBy B` edge (RDF-star):_ RDF-star
annotates a **statement** with metadata *about the assertion* (confidence, provenance, valid-time of a
claim); a planting **date** is a property of a world event, not of the statement — a category error.
This project reserves RDF-star strictly for uncertainty/provenance (`docs/RDF_MODELING.md` §6/§7). A
bare edge also can't carry the date, can't be addressed (link the obligation, query replantings), and
can't represent the ~126 unmet fellings cleanly (which are simply a `gs:Felling` with no
`gs:Replanting`).

### D2 — Link the replanting to the felling (event↔event), lineage derived

`gs:Replanting gs:replaces gs:Felling` — the herplantplicht is "*this felling* must be followed by a
replant," so event-to-event keeps the obligation/fulfilment reasoning where Phase 2 needs it. The
tree lineage (B replaces A) is reachable via `gs:felledTree`/`gs:plantedTree`; a direct
`gs:Tree → gs:Tree` edge is deferred (avoid redundancy until an ecology-style query needs it).

### D3 — Observation is content-addressed by its felling set

`data:observation/<zaaknummer>/<felling-set-key>` (hash of the sorted assigned `gs:Felling` IRIs),
carrying `gs:includesFelling` → its members. Immutable; a changed set = a new Observation IRI. Its
active-ness is carried by whether an **open** `gs:CoveragePeriod` links it — no separate `active` flag
(that would duplicate the period's valid-time). _Supersedes derive-coverage-audit D2_ (identity-only,
one-per-permit Observation).

### D4 — The content-key bug dissolves (no patch)

The `gs:CoveragePeriod` content-key already hashes the Observation IRI. Once the Observation IRI is
content-addressed by the felling set (D3), the period content-key changes **iff** the assigned set
changes, and stays identical otherwise. So a changed set → new period IRI → clean supersede; an
identical set → no-op. This removes the duplicate-open-period collision at the root — the earlier
felling-count/set content-key patch is dropped.

### D5 — Only felled trees are loaded; replacements are bare identities

The graph's observation layer is the **felled** trees. Replacement trees (`boomNieuwId`, almost never
felled — 52/7223) are minted as **bare `gs:Tree` identities** referenced by `gs:plantedTree`; their
attributes aren't loaded (they weren't felled). If a replacement is itself later felled, its own
felled row loads it fully. Keeps the graph a faithful felled-tree layer without pulling in the whole
registry.

### D6 — Cross-load references gated by IRI+namespace pattern

`gs:felledTree`, `gs:plantedTree`, `gs:replaces`, `gs:includesFelling` are references across separately
loaded layers (bomen graph-load ↔ derive). Like `gs:coversIntervention`, they are gated
`sh:nodeKind sh:IRI` + `sh:pattern` on the target namespace, **not** `sh:class` — the isolated-candidate
SHACL gate can't see the other layer's nodes.

## Risks / Trade-offs

- **[Graph volume]** ~8.2k `gs:Felling` + ~8k `gs:Replanting` + felled/replacement `gs:Tree` nodes enter
  the graph. → Identity + dates + relations only (no geometry/bulk values); well within Fuseki's range.
- **[Invisible-but-correct version bumps]** A same-count/same-confidence tree *swap* now versions the
  period (new Observation IRI) even though the period's other visible triples are unchanged. → This is
  correct (the covered trees changed); the "why" is the `gs:includesFelling` membership, now visible in
  the graph.
- **[Sequencing on derive-coverage-audit]** This modifies capabilities that change introduces
  (`coverage-audit`, `graph-ontology`, `graph-shapes`). → derive-coverage-audit archives first
  (delivering those specs to main); this change then modifies them. Load order: bomen graph-projection
  before `derive`.
- **[Historical membership]** `audit_metrics` keeps only the current felling set; closed periods'
  Observations retain their `gs:includesFelling` in history, so per-version membership is now queryable
  from the graph (an improvement over the identity-only model).

## Migration Plan

Additive to the ontology/shapes; new bomen graph-projection; derive Observation assembly reworked.
Rollback = revert the ontology/shapes/bomen/derive commits. Run order after load: geo, bomen (PostGIS
+ graph fellings), graph (places), koop (interventions), derive. First derive run after this change
re-mints content-addressed Observations; the writer supersedes the prior (buggy) periods. Integration
tests against the isolated Fuseki + Postgres, never production names.

## Open Questions

- **Direct tree lineage edge** (`gs:Tree gs:replaces gs:Tree`): deferred (derivable via the events);
  add if a lineage/ecology query needs it.
- **Felling identity key**: use `kapenherplant.id` for `gs:Felling`/`gs:Replanting` IRIs (the record
  is 1:1 with the felling act); `boomId`/`boomNieuwId` for `gs:Tree`. Confirm no `boomId` collisions at
  load (17 boomIds have >1 row — pick the felled row, or key the felling by record id as specified).
