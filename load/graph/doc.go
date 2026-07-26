// Package graph is the validating, idempotent-upsert load path into the RDF triplestore (Fuseki):
// it takes a candidate graph (Turtle), gates it through SHACL, and — only on conform — diffs it by
// subject IRI against the live graph and writes only the delta (new-or-changed entities) into a
// run-stamped named graph with PROV provenance; an unchanged re-run writes nothing at all. It is
// the sibling of load/geo and load/bomen for the graph store, per
// openspec/changes/p8-modeling/design.md D6, extended by openspec/changes/graph-writer-upsert for
// the SCD2 idempotent-upsert layer (that change's design.md is the authority for D1-D7 below).
//
// Design decisions this package encodes (see the relevant design.md for the full rationale):
//
//   - D2 (p8-modeling) — confidence/evidence are asserted with the `{| … |}` RDF-star annotation
//     form, never bare `<< … >>`; this package never constructs quoted triples itself (candidates
//     arrive already shaped that way from extract/derive) but the SHACL confidence-presence check
//     it gates on, and the change-detection signature (below), both depend on it.
//   - D5 (p8-modeling) — transaction-time = PROV run-stamped named graphs. A Load call whose
//     computed delta is non-empty mints its own `run:load-<runID>` named graph plus a
//     `prov:Activity` / `prov:generatedAtTime` triple in the dedicated `run:_provenance` named
//     graph (never the default graph, which the dataset's unionDefaultGraph setting shadows). A
//     delta-empty run (graph-writer-upsert D5) mints neither — PROV records transaction time only
//     for runs that actually changed the graph.
//   - D6 (p8-modeling) — the load gate is HTTP, in Go, no in-process JVM: a Fuseki client
//     (client.go) POSTs the candidate + the embedded ontology.Shapes to the dataset's `/shacl`
//     endpoint and parses the `sh:conforms` result (shacl.go) as the loud-failing gate — the graph
//     analogue of load/geo/gates.go. On non-conform, nothing is staged or written and the
//     violation detail is returned as an error (graph-writer-upsert D7: validation runs before any
//     mutation, unchanged by the upsert layer below).
//
// The idempotent-upsert layer (openspec/changes/graph-writer-upsert), on a conforming candidate:
//
//   - D1/D2 — change detection is a per-entity (stable subject IRI) subgraph diff, computed in
//     the triplestore via SPARQL rather than a Go RDF-star parser: the candidate is staged into
//     its own `run:stage-<runID>` graph (signature.go), and the same GROUP_CONCAT signature SELECT
//     runs against that staging graph and against the union of every `run:load-*` graph's
//     currently-OPEN versions (a row whose owning triple already carries `gs:validTo` is excluded).
//     Both signature maps feed the pure `classify` function (classify.go), never touching the
//     store.
//   - D3/D4 — the caller owns `gs:validFrom` (world-time); the writer only reads it back to stamp
//     the prior's `gs:validTo` on close, and never invents or overwrites it. `gs:validFrom`
//     presence is the evolving/immutable discriminator: an evolving entity whose content changed
//     opens a new version and closes the prior (never deleting history); an immutable entity is
//     insert-if-absent (write-once), and a content conflict on re-assertion is skipped but logged
//     (`log.Printf`, `graph: …` prefix) rather than silently dropped or overwritten.
//   - D6 — whole-candidate batching: classification runs as one pair of SELECTs (staging + live),
//     and at most two SPARQL Updates follow — one closing every superseded prior, one copying the
//     delta's triples + RDF-star annotations into the fresh run graph — never per-entity
//     round-trips (write.go's `upsert`, `closePriors`, `copyDeltaAndRecordProvenance`).
//
// The merge-vocab validation recipe (load-bearing, proven live against Fuseki 5.5.0): SHACL's
// controlled-value-set constraints (species/activity/status bound to domain-vocabulary concepts
// via `skos:inScheme`) only pass when the concept's `skos:inScheme` triple is visible IN THE SAME
// graph being validated — cross-graph validation (shapes referencing concepts that live in a
// different named graph than the data being checked) silently fails, because the concepts are
// invisible to the validated graph's own `sh:node`/`sh:class` checks. So (*client).validate merges
// ontology.Ontology + ontology.Vocab + the candidate into one transient scratch named graph
// (`run:validate-<runID>`), validates THAT graph, and always drops it afterward — regardless of
// conform — leaving only the final `run:load-<runID>` graph (candidate only, no merged
// ontology/vocab) once the candidate is confirmed conforming.
//
// Load also maintains a persistent `run:_model` named graph holding the reference model
// (ontology.Ontology + ontology.Vocab) — idempotent (rewritten unconditionally on every Load call
// via PUT, so it always holds exactly the embedded model, never an accumulation) — for anything
// downstream that wants to query the model directly, distinct from the transient per-validation
// scratch graph above.
package graph
