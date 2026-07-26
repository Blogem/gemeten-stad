## 1. Change detection (pure Go — unit-tested first)

- [x] 1.1 Define an `entitySignature` type keyed by subject IRI holding its valid-time-agnostic
      content (sorted `(predicate, object)` rows + RDF-star annotation rows), and a helper that
      marks an entity evolving iff a `gs:validFrom` is asserted for it (D4).
- [x] 1.2 Implement `classify(candidate, live map[iri]entitySignature)` returning distinct buckets
      `new, changed, unchanged, immutableConflict []iri` as a pure function: absent in live → new;
      equal signature → unchanged; differing signature on an evolving entity → changed; differing
      signature on an immutable entity → immutableConflict (skip-but-surface, not folded into
      unchanged) (D4).
- [x] 1.3 Unit tests (table-driven, testify) for `classify` covering: new IRI, unchanged re-run,
      changed evolving edge (place change and confidence-only change), immutable re-assert skipped,
      immutable-conflict bucketed separately, and valid-time-only difference classified as unchanged.

## 2. SPARQL signature extraction (in-triplestore diff — D2)

- [x] 2.1 Add a client method to stage the candidate into a dedicated `run:stage-<runID>` graph
      (candidate-only, distinct from the P8 validation-merge scratch graph), dropped in a `defer`
      with a detached context (mirroring `validate`'s cleanup).
- [x] 2.2 Add a SPARQL SELECT that returns, per `data:` subject in the staging graph, its
      `GROUP_CONCAT` signature excluding `gs:validFrom`/`gs:validTo`, plus its annotation rows; parse
      results into `map[iri]entitySignature`.
- [x] 2.3 Add the analogous SELECT over the live **open** versions (union of `run:load-*` graphs;
      "open" = the evolving form carries no `gs:validTo`) into `map[iri]entitySignature`.

## 3. Upsert write path (SCD2 open/close — D3, D5, D6)

- [x] 3.1 Add a batched close: a single SPARQL Update that, for each changed evolving entity, stamps
      `gs:validTo` = the candidate's `gs:validFrom` (contiguous close) on **every** currently-open
      prior for that evolving form (self-healing if a stray extra-open version exists), never deleting
      history.
- [x] 3.2 After the close, verify the single-open-version invariant at write time: a SPARQL query
      SHALL confirm exactly one open version remains per superseded form; if more than one remains,
      `Load` fails loud (returns an error, writes no run graph / no prov).
- [x] 3.3 Add a batched delta copy: a single SPARQL Update copying the new ∪ changed entities'
      triples + annotations from `run:stage-<runID>` into `run:load-<runID>`, then record the
      `prov:Activity` (reusing `provenanceTurtle`).
- [x] 3.4 Rewire `Load` to: validate (unchanged gate) → stage candidate → extract both signatures →
      `classify` → log a warning (`log.Printf`, `graph: …` prefix) for each `immutableConflict`
      naming the subject IRI → if delta (new ∪ changed) empty, drop staging and return `nil` with
      **no run graph / no prov** (D5) → else close priors, copy delta, write prov, drop staging.
      (An immutable conflict alone, with no new/changed entity, is still a no-op write — only a
      warning is emitted.)
- [x] 3.5 Update `doc.go` and the `Load`/`Config` doc comments to describe the idempotent-upsert
      behaviour (replacing the "additive run graphs" wording), keeping `Config{Reset}` as the full
      rebuild.

## 4. Integration tests (isolated gs-test Fuseki — D7)

- [x] 4.1 Rewrite the P8 "one new run graph per conforming write" assertions to the upsert semantics
      (a first load writes a run graph + prov; the caller-supplied `gs:validFrom` is present).
- [x] 4.2 No-op re-run: loading the same candidate twice leaves no second `run:load-…` graph, no new
      `prov:Activity`, and no added triples.
- [x] 4.3 Change-opens-a-new-version: re-load an evolving `locatedAt` edge with a changed
      place/confidence; assert a new version with the new `gs:validFrom`, the prior stamped with
      `gs:validTo`, exactly one open version, and the prior triples still present.
- [x] 4.4 Regression: a malformed candidate is rejected with violation detail and causes no partial
      writes (no run graph, no closed prior, no prov) — extend the existing reject cases.
- [x] 4.5 Immutable conflict: re-load an already-present immutable entity (e.g. a Place) with
      differing content; assert the stored version is unchanged and a warning is emitted (capture
      via a redirected `log` output), and that a conflict-only run mints no run graph / no prov.

## 5. Validation

- [x] 5.1 `go build ./...` and `go vet ./load/graph/...` clean.
- [x] 5.2 `go test ./load/graph/...` (unit) passes.
- [x] 5.3 `go test -tags=integration ./load/graph/...` passes against `GS_TEST_FUSEKI_URL`
      (guarded against production names).
- [x] 5.4 `openspec validate graph-writer-upsert --strict` passes.
