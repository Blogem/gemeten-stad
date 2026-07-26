// Package graph is the validating load path into the RDF triplestore (Fuseki): it takes a
// candidate graph (Turtle), gates it through SHACL, and — only on conform — writes it into a
// run-stamped named graph with PROV provenance. It is the sibling of load/geo and load/bomen for
// the graph store, per openspec/changes/p8-modeling/design.md D6.
//
// Design decisions this package encodes (see design.md for the full rationale):
//
//   - D2 — confidence/evidence are asserted with the `{| … |}` RDF-star annotation form, never
//     bare `<< … >>`; this package never constructs quoted triples itself (candidates arrive
//     already shaped that way from extract/derive) but the SHACL confidence-presence check it
//     gates on depends on it.
//   - D5 — transaction-time = PROV run-stamped named graphs. Each Load call that writes a
//     candidate mints its own `run:load-<runID>` named graph plus a `prov:Activity` /
//     `prov:generatedAtTime` triple in the dedicated `run:_provenance` named graph (never the
//     default graph, which the dataset's unionDefaultGraph setting shadows).
//   - D6 — the load gate is HTTP, in Go, no in-process JVM: a Fuseki client (client.go) POSTs the
//     candidate + the embedded ontology.Shapes to the dataset's `/shacl` endpoint and parses the
//     `sh:conforms` result (shacl.go) as the loud-failing gate — the graph analogue of
//     load/geo/gates.go. On conform, the candidate is written under its run graph (write.go); on
//     non-conform, nothing is written and the violation detail is returned as an error.
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
