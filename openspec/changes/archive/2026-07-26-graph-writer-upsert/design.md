## Context

P8 shipped `load/graph` as an **append-per-run** primitive: `Load(ctx, fusekiURL, candidate, cfg)`
merges `ontology + vocab + candidate` into a transient scratch graph, SHACL-validates it, and on
conform POSTs the candidate into a fresh `run:load-<runID>` named graph plus a `prov:Activity` in
`run:_provenance` (`load/graph/load.go`, `write.go`). Only `Config{Reset}` clears prior runs; absent
it, every conforming write is additive — a re-run duplicates every triple under a new run graph.

Phase 1's three graph writers (P12b Place skeleton, P13 `load koop`, P14 `derive`) all specify
_"re-running is a no-op"_, and the bitemporal model (`IMPLEMENTATION_PLAN.md` §3, `p8-modeling`
design D4) mandates **never overwrite history**: evolving state is superseded by opening a new
version and closing the prior with `gs:validTo`. This change adds that upsert layer _on top of_ the
shipped primitive — the SHACL gate, the run-stamped-graph + PROV write path, and the merge-vocab
validation recipe are all reused unchanged.

Constraints inherited from the codebase:

- **HTTP-only, no in-process JVM** (D6) — all graph work is SPARQL/Graph-Store over Fuseki.
- **Go where possible** (§5) — no new Python; avoid heavyweight deps.
- **The writer takes turtle it is given** and stays **Postgres-free** — `{| … |}` construction lives
  with the callers (`doc.go`: "candidates arrive already shaped").
- The dataset runs `unionDefaultGraph=on`, so named graphs must be addressed explicitly.

## Goals / Non-Goals

**Goals:**

- A `Load` re-run on unchanged input is a **true no-op**: no new triples, no new `run:load-<runID>`
  graph, no `prov:Activity`.
- A changed tracked field on evolving state **opens a new version** (new `gs:validFrom`) and
  **closes the prior** (`gs:validTo`), never touching history.
- Immutable facts (identity + un-stamped triples, e.g. the Place skeleton, the `Claim`) are
  **write-once**: inserted if absent, skipped thereafter.
- The malformed-candidate gate is a **preserved regression**: non-conforming candidate → rejected,
  no partial writes.
- The change-detection/upsert core is **unit-testable Go logic** plus an integration test on the
  isolated `gs-test` dataset.

**Non-Goals:**

- Constructing `{| … |}` annotations or reading Postgres — stays with P12b/P13/P14.
- The `gs:Assessment` fulfilment SCD2 node (Phase 2) — the mechanism must _extend_ to node-form
  evolving state, but no Assessment handling is built now.
- AuditLink TBox attachment edges + `AuditLinkShape` — owned by P14 (`p8-modeling` archive note).
- Concurrent `Load` against one dataset — remains serial (unchanged from the shipped primitive).
- Cross-run garbage collection / compaction of closed versions.

## Decisions

### D1 — Per-entity (subject-IRI) subgraph diff, NOT delete-insert-by-IRI

Change detection is keyed on the **stable subject IRI**. For each candidate entity the writer
compares its content against what the graph already holds and classifies it _new / unchanged /
changed_. Delete-insert-by-IRI was considered and **rejected**: replacing an IRI's triples wholesale
destroys the prior version, violating the never-overwrite rule (§3 D4). Per-entity diff is the only
option that can _close_ a prior version rather than delete it.

### D2 — Diff runs in the triplestore via SPARQL, not a Go RDF-star parser

The candidate is opaque turtle bytes; to compare it against live data we need its structure. Rather
than add a Go RDF-star parser dependency, the writer **stages the candidate into its own named graph**
(`run:stage-<runID>`, candidate-only — distinct from the P8 validation-merge scratch graph) and
computes a **per-entity content signature** with SPARQL: a `GROUP_CONCAT` of the entity's sorted
`(predicate, object)` rows plus its RDF-star annotation rows, **excluding `gs:validFrom`/`gs:validTo`**.
The same SELECT runs against the live **open** versions (union of `run:load-*` graphs, "open" = the
evolving form carries no `gs:validTo`). Both result sets return to Go, which does the classification
as a **pure function over `(iri → signature)` maps** — directly satisfying the plan's "unit tests on
the change-detection / upsert logic" without any live store. This reuses the existing scratch-graph
pattern and HTTP client; no new dependency. Alternative (Go-side canonicalization with an RDF-star
parser) was rejected as a heavier dependency doing work Fuseki already does.

### D3 — Caller owns `gs:validFrom` (world-time); the writer owns the close (`gs:validTo`)

Valid-time is **world-time** = the observation/permit dates, which only the caller knows; the writer
must not invent it. So the caller emits `gs:validFrom` on evolving facts (it already builds the
`{| … |}` annotation), and the writer:

- **strips `gs:validFrom`/`gs:validTo`** from both sides before computing the signature (so an
  unchanged entity re-emitted with a fresh `validFrom` still compares equal → no-op);
- on **supersede**, stamps the prior open version's `gs:validTo` = the new version's `gs:validFrom`
  (contiguous intervals). The writer never writes `gs:validFrom` itself.

### D4 — `gs:validFrom` presence is the evolving/immutable discriminator

An entity is **evolving** iff the candidate asserts a `gs:validFrom` for it (as an RDF-star
annotation on one of its edges, or on a state/period node it owns); otherwise it is **immutable**.
This keys entirely off the temporal convention already in the ontology — no per-predicate hardcoding
in the writer.

- **Immutable** entities (Place, Claim, plain identity triples): **insert-if-absent** — present →
  skipped, never overwritten (write-once). If a re-asserted immutable entity's non-temporal content
  **differs** from what is stored, that is a data anomaly (an immutable fact should never change):
  the writer still does NOT overwrite it, but it **emits a warning diagnostic** (`log.Printf`,
  `graph: …` prefix like `load/geo`) naming the subject IRI and the conflict, so these cases are
  surfaced for later investigation rather than silently dropped. So `classify` returns immutable
  conflicts as a **distinct bucket** (not folded into `unchanged`): the write outcome is skip, the
  reporting outcome is a logged warning. (A future consumer may aggregate these warnings into a
  dashboard; this change only guarantees they are surfaced, not where.)
- **Evolving** entities (`locatedAt` annotation, `AuditLink`): participate in **open/close**.

### D5 — A no-op run leaves no trace

After classification, the writer builds a **delta** = new ∪ changed entities. If the delta is empty,
it drops the staging graph and returns `nil` **without** minting a `run:load-<runID>` graph or a
`prov:Activity`. PROV records transaction time only for runs that actually changed the graph — so a
second run is a true no-op end to end (matches P11/P13/P15's "second run is a no-op").

### D6 — Whole-candidate batching; two bulk writes, not per-entity round-trips

The candidate is the whole batch (e.g. the Noord corpus). The writer classifies **all** entities in
one pair of SELECTs, then performs at most two mutations: (1) one batched SPARQL Update closing every
superseded prior (`INSERT` the `gs:validTo` stamps), and (2) one SPARQL Update copying the delta
entities' triples + annotations from `run:stage-<runID>` into `run:load-<runID>`, plus the
`prov:Activity`. Staging is dropped in a `defer` (detached context, like the shipped `validate`
cleanup). No per-entity HTTP round-trips.

### D7 — The SHACL gate is unchanged and runs before any mutation

Validation stays exactly as shipped (merge-vocab recipe, `sh:conforms` parse). It runs first; on
non-conform the writer returns the violation detail and performs no staging-copy and no close — the
regression the plan requires. (Validating the full candidate rather than only the delta is fine and
simpler: unchanged entities were already conforming when first written.)

## Risks / Trade-offs

- **[RDF-star signature correctness]** — A signature that mis-handles annotation rows could classify
  a changed edge as unchanged (missed supersede) or vice-versa (spurious version churn). → Keep
  entity shapes small and known (Place/Intervention/Claim/locatedAt/AuditLink); cover each shape's
  new/unchanged/changed classification in Go unit tests with representative signature rows, and
  assert the open/close outcome in the integration test.
- **[Open-version ambiguity]** — If a prior supersede left two "open" versions for one edge, the
  close update could stamp the wrong one. → Enforced at write time, not just assumed: the close
  update stamps `gs:validTo` on **every** currently-open prior for that evolving form (not "the" one),
  so a stray extra-open version self-heals into a closed one. After closing and before returning, the
  writer **verifies the invariant** — exactly one open version per superseded form — and **fails loud**
  (returns an error, no prov) if more than one open version remains, surfacing the anomaly rather than
  writing ambiguous history. The integration test additionally asserts exactly one open version after
  a change.
- **[Signature scale]** — `GROUP_CONCAT` over the whole corpus in one query. → Corpus is tiny
  (~600 permits, ~630 places; P10 sizing); if it ever grows, chunk the candidate subject set. Logged
  as a future concern, not built now.
- **[Behaviour change vs shipped primitive]** — Callers/tests relying on "every conforming write
  adds a run graph" break. → Only the P8 integration tests assert that; they are updated as part of
  this change, and no external caller yet depends on the additive behaviour (P12b/P13/P14 are
  unbuilt). `Config{Reset}` (full rebuild) is retained unchanged.

## Migration Plan

Not applicable — greenfield POC, nothing released. The additive-write behaviour is replaced in
place (no `Reset`-path change; no data migration). The shipped P8 integration tests that assert
"one new run graph per conforming write" are rewritten to assert the upsert semantics.

## Open Questions

- None blocking. The node-form SCD2 close (for `gs:Assessment`/`AuditLink`-as-node) is deferred to
  its owners (Phase 2 / P14); this change builds the annotation-form open/close and the immutable
  insert-if-absent path, shaped so the node-form close reuses the same "stamp prior `gs:validTo`"
  step.
