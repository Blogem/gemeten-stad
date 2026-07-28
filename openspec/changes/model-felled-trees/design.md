## Context

The P14 coverage derive (`derive-coverage-audit`, now archived) links a permit to registry fellings,
but keeps the fellings **only in PostGIS** — the graph's `gs:Observation` is identity-only (one mutable
node per permit, derive-coverage-audit D2). A real-corpus run exposed the cost: the
`gs:CoveragePeriod` content-key hashes the (constant) Observation IRI rather than the assigned felling
set, so two different outcomes with the same rounded confidence collide on one period IRI and the
writer re-writes it into a second run graph — **59 anchors with two open periods** (a graph-integrity
bug). We fix it by modelling the felled trees + fellings and content-addressing the Observation — the
correct Phase-1 finale under `docs/RDF_MODELING.md`'s best-practice-over-leanness rule.

Data checked on the Noord corpus (informs the deferred Phase-2 replant layer): `boomId ≠ boomNieuwId`
(a replacement is a **new** distinct tree); felling and replant are separate acts, dates often >1yr
apart; ~126 fellings are unmet (felled, no replant yet); replacement-then-refelled chains are rare
(52/7223).

## Goals / Non-Goals

**Goals:**

- Model felled trees + the felling event as first-class graph entities: `gs:Tree`, `gs:Felling`.
- Load felled trees (+ their fellings) into the graph via the SHACL gate (a bomen → graph projection).
- Rework the coverage `gs:Observation` to be content-addressed by its assigned felling set and to link
  its members via `gs:includesFelling`, which makes period versioning correct (one open period per
  anchor) and supersedes the content-key duplicate-open-period bug.

**Non-Goals (deferred to Phase 2, as one coherent unit):**

- The **replant layer**: `gs:Replanting` (`gs:plantedTree`/`gs:plantedOn`), the felled→replacement tree
  lineage (`dct:isReplacedBy`), replacement-tree identities, and the herplantplicht fulfilment
  `Assessment`. See D5.
- Loading non-felled trees; tree geometry in the graph (stays in PostGIS).

## Decisions

### D1 — Felled trees + the felling event become first-class graph entities

`gs:Tree` (`data:tree/<boomId>`, identity only) and `gs:Felling` (`data:felling/<id>`, `gs:felledTree`
→ the tree, `gs:felledOn` `xsd:date`). The felling is modelled as an **event** (not a boolean flag on
the tree) because it carries its own date and is the observed fact the audit links to. This reverses
derive-coverage-audit D2's identity-only Observation and the "no bulk registry rows in the graph"
leanness call — now a production system, the registry observation layer belongs in the graph (identity
+ descriptive date + relations; geometry stays in PostGIS).

### D2 — Observation is content-addressed by its felling set

`data:observation/<zaaknummer>/<felling-set-key>` (hash of the sorted assigned `gs:Felling` IRIs),
carrying `gs:includesFelling` → its members. Immutable; a changed set = a new Observation IRI. Its
active-ness is carried by whether an **open** `gs:CoveragePeriod` links it — no separate `active` flag
(that would duplicate the period's valid-time). _Supersedes derive-coverage-audit D2_ (identity-only,
one-per-permit Observation).

### D3 — The content-key bug dissolves (no patch)

The `gs:CoveragePeriod` content-key already hashes the Observation IRI. Once the Observation IRI is
content-addressed by the felling set (D2), the period content-key changes **iff** the assigned set
changes, and stays identical otherwise. So a changed set → new period IRI → clean supersede; an
identical set → no-op. This removes the duplicate-open-period collision at the root — the earlier
felling-count/set content-key patch is dropped.

### D4 — Only felled trees are loaded

The graph's observation layer is the **felled** trees (`kapenherplant` rows with a felling date).
Trees never felled do not enter the graph. Keeps the graph a faithful felled-tree layer without
pulling in the whole registry.

### D5 — Cross-load references gated by IRI+namespace pattern

`gs:felledTree` and `gs:includesFelling` are references across separately loaded layers (bomen
graph-load ↔ derive). Like `gs:coversIntervention`, they are gated `sh:nodeKind sh:IRI` + `sh:pattern`
on the target namespace, **not** `sh:class` — the isolated-candidate SHACL gate can't see the other
layer's nodes.

### D6 — The replant layer is deferred to Phase 2 as one unit

The Phase-1 coverage audit consumes fellings only (`Observation → gs:includesFelling → gs:Felling`);
nothing in scoring, assignment, or the content-key touches the replant. And a `gs:Replanting` event is
**worthless without a link to the felling it discharges** — so the event and its felled→replacement
lineage (`dct:isReplacedBy`) are inseparable and land together, in Phase 2, alongside the fulfilment
`Assessment` that actually uses them. Deferring is clean and additive: the replant data
(`boomNieuwId`, `plantmaatregelDatumUitgevoerd`) stays in PostGIS, and Phase 2 just extends the
bomen→graph projection + adds the assessment. _On the lineage relation itself (Phase 2):_ use the
standard **`dct:isReplacedBy`** (reuse-first; Dublin Core, already active here) at the **tree level**
(`felled tree dct:isReplacedBy replacement tree`) — NOT a minted `gs:replaces` and NOT event-to-event
(an event doesn't "supersede" another event; the *tree* is what's replaced). Recorded here so Phase 2
starts from the settled choice.

## Risks / Trade-offs

- **[Graph volume]** ~8.2k `gs:Felling` + felled `gs:Tree` nodes enter the graph. → Identity + felling
  date + relations only (no geometry/bulk values); well within Fuseki's range.
- **[Invisible-but-correct version bumps]** A same-count/same-confidence felling *swap* now versions the
  period (new Observation IRI) even though the period's other visible triples are unchanged. → Correct
  (the covered trees changed); the "why" is the `gs:includesFelling` membership, now visible.
- **[Sequencing]** Builds on the archived derive-coverage-audit specs; load order = bomen (PostGIS +
  graph fellings) before `derive`.
- **[Historical membership]** Closed periods' Observations retain their `gs:includesFelling`, so
  per-version membership becomes queryable from the graph (an improvement over the identity-only model).

## Migration Plan

Additive to the ontology/shapes; new bomen graph-projection; derive Observation assembly reworked.
Rollback = revert the ontology/shapes/bomen/derive commits. Run order after load: geo, bomen (PostGIS +
graph fellings), graph (places), koop (interventions), derive. First derive run after this change
re-mints content-addressed Observations; the writer supersedes the prior (buggy) periods. Integration
tests against the isolated Fuseki + Postgres, never production names.

## Open Questions

- **Felling identity key**: use `kapenherplant.id` for `gs:Felling` IRIs (the record is 1:1 with the
  felling act); `boomId` for `gs:Tree`. Confirm no `boomId` collisions at load (17 boomIds have >1 row
  — key the felling by record id as specified; the tree by boomId).
