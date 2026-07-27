## Context

`docs/RDF_STAR_RELATIONSHIPS.md` (added here) sets the doctrine: valid-time versioning belongs on the
**node** that owns a time-bounded relationship, expressed as **one node per period** grouped by a
stable **series anchor**; RDF-star annotated edges carry **refinable metadata** (transaction-time),
never valid-time. Two things in main need to change to make that real and to unblock P14:

- **`load/graph`** — the SCD2 upsert (`write.go`, `signature.go`) detects an entity as *evolving* for
  both a node-triple `gs:validFrom` and an RDF-star `<< s p o >> gs:validFrom` (`evolvingFlagPattern`
  matches both), but the **close** only handles the annotation form (`closePriors` stamps `validTo`
  onto `<< ?s ?p ?o >> gs:validFrom`; `verifyOpenInvariant` counts annotation edges). A node-form
  period is detected as changed but never closed → the invariant fails. The code comment names this
  the deferred "node-form Assessment close."
- **`load/koop`** — `renderLocatedAtAnnotations` stamps `gs:validFrom` on the `locatedAt` annotation,
  mis-modeling a Flavour-1 refinable edge.

## Goals / Non-Goals

**Goals:**

- Writer: version **node-form period nodes** grouped by `gs:versionOf` — one open period per anchor,
  a new period closes the prior, history retained, unchanged re-run a true no-op.
- Add the generic `gs:versionOf` term.
- `load/koop`: `locatedAt` carries `gs:confidence` (+ `gs:caveat`) only, no `gs:validFrom`.
- Land the doctrine doc and align `IMPLEMENTATION_PLAN.md`.

**Non-Goals:**

- **No in-place transaction-time refinement of edges.** A changed *immutable* edge (e.g. a re-resolved
  `locatedAt`) still skip-and-warns; Phase 1 re-resolves via `--reset`. A refinement path is a noted
  follow-up.
- **No `gs:AuditLink` shape / coverage classes** — those belong to `derive-coverage-audit`; this
  change provides only the generic node-form period mechanism + `gs:versionOf`, tested with a
  synthetic period series.
- **No change to the SHACL gate, run-graph/PROV, `Config{Reset}`, or the existing annotation-form
  versioning.**

## Decisions

### D1 — A period node = plain `gs:validFrom` + `gs:versionOf <anchor>`; the writer keeps one open per anchor

The generic contract: a **period node** carries a node-triple `gs:validFrom` and a
`gs:versionOf <anchorIRI>`. The `<anchorIRI>` is any stable IRI identifying the series (domain's
choice). The writer:

- **Signature** (change detection) ignores `gs:validFrom`/`gs:validTo` (already the rule) and excludes
  a **closed** period (one already carrying `gs:validTo`) from the *open* set — add the node-form
  analogue of the annotation branch's `FILTER NOT EXISTS { << … >> gs:validTo }`, i.e.
  `FILTER NOT EXISTS { ?s gs:validTo ?vt }` for node subjects.
- **Open/close is series-keyed, triggered by a *new* period node** (not "same-IRI changed"): because
  each period has a **content-derived IRI** (D2), a changed outcome is a *new* subject, not a mutated
  one. So for every new period node `?p` with `gs:versionOf ?a` and `gs:validFrom ?vf`, the writer
  closes the prior open period(s) of the same `?a` by stamping their `gs:validTo = ?vf`:
  `INSERT { GRAPH ?g { ?old gs:validTo ?vf } } WHERE { VALUES(?p ?a ?vf){…} GRAPH ?g { ?old gs:versionOf ?a ; gs:validFrom ?ovf . FILTER NOT EXISTS { ?old gs:validTo ?any } . FILTER(?old != ?p) } }`.
- **`verifyOpenInvariant`** — after close, exactly one open period per anchor touched this run
  (the new period in staging), counting node-form periods alongside the annotation form.
- The writer still never invents `gs:validFrom` — it reads the new period's stamp (via
  `stagingValidFrom`, extended to node-form) to use as the prior's `gs:validTo`.

_Alternative rejected (same-IRI node versioning):_ mutating one node IRI across periods is ambiguous —
flat multi-attribute triples can't associate a value with a period (`RDF_STAR_RELATIONSHIPS.md`).
_Alternative rejected (annotation-form on the node):_ works on the existing writer but keeps the
outcome as a timeless base triple and the value on an annotation (`sh:sparql` gate) — the user chose
plain-property period nodes (core-SHACL) for a true system.

### D2 — Content-derived period IRIs make re-runs idempotent

A period node's IRI is derived from a hash of its **outcome content** (state + targets + rounded
values), **excluding** the timestamps. So an unchanged outcome re-mints the *same* IRI → the writer's
valid-time-agnostic signature matches → **unchanged**, a true no-op (the original `gs:validFrom` is
retained, never overwritten). A changed outcome mints a *new* IRI → **new** period → the prior is
closed. The caller (the derive stage) owns the hashing; the writer only relies on "same content →
same IRI, changed content → new IRI." _Alternative rejected:_ run-stamped or sequential IRIs — a
re-run would mint a new IRI and never be a no-op, or would require the derive stage to read current
graph state to compute a sequence.

### D3 — `locatedAt` loses `validFrom`; corrections are transaction-time

`renderLocatedAtAnnotations` drops the `gs:validFrom` branch. The edge is Flavour-1: it holds; its
confidence is refinable. An unchanged re-run is a no-op; a *changed* resolution is now an
immutable-content change → the writer skips-and-warns (shipped D4), and Phase 1 re-resolves via
`--reset`. Acceptable: location is resolved once from the stable local BAG mirror, and a genuine
relocation is a new permit entity, not an in-place edit. _Alternative considered:_ a transaction-time
edge-refinement path (latest run supersedes) — deferred (Non-Goals).

## Risks / Trade-offs

- **[Writer regression risk]** Touching `signature`/`closePriors`/`verifyOpenInvariant` on a shipped
  primitive. → Additive node-form branches beside the annotation form; new integration tests for a
  synthetic period series (open → new period → close → invariant), and the existing P12 tests must
  still pass. Keep an annotation-form test (a synthetic annotated edge) so that path stays covered
  after `locatedAt` stops exercising it.
- **[Content-hash collisions / instability]** A poorly-chosen hash could collide (two distinct
  outcomes → same IRI → a real change missed) or be unstable (same outcome → different IRI → spurious
  version churn). → The caller hashes a canonical, fully-specified outcome tuple; `derive` owns and
  tests it. Documented as the caller's contract.
- **[Re-resolution silently skipped]** `locatedAt` immutable → a changed resolution is skip-and-warn.
  → Documented; `--reset` re-resolves; the warn diagnostic surfaces it; low impact (stable BAG,
  resolve-once).
- **[Two evolving forms coexist]** Annotation-form and node-form both supported; a subject/series must
  still have at most one open version. → The invariant enforces it across both; documented in
  `RDF_STAR_RELATIONSHIPS.md`.

## Migration Plan

Additive writer branches (node-form + `gs:versionOf`), a subtractive one-liner in `load/koop`
(`validFrom` drop), a new ontology term, plus docs. No graph-data migration; existing run graphs are
unaffected. Integration tests run against the isolated Fuseki dataset. Rollback = revert the writer
branches, the term, and restore the `validFrom` line.

## Open Questions

- **Edge refinement (transaction-time) — when needed?** If a re-resolved `locatedAt` must take effect
  without `--reset`, add a transaction-time refinement path later. Tracked; not built here.
- **`gs:versionOf` range** — left generic (any IRI) so any model can anchor a series; the shape that
  constrains a coverage period's anchor to a `gs:AuditLink` node lives with the coverage model
  (`derive-coverage-audit`), not in the generic writer term.
