package graph

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

const (
	// gsRunNS is the run: namespace for run-stamped named graphs and provenance (settled
	// namespace: http://gemetenstad.nl/run/).
	gsRunNS = "http://gemetenstad.nl/run/"

	// referenceModelGraph is the persistent named graph Load ensures holds the reference model
	// (ontology + vocab) — design.md D6. It's a "run:" operational graph, not a data graph, kept
	// distinct from the transient per-validation scratch graph (see (*client).validate).
	referenceModelGraph = gsRunNS + "_model"

	// provenanceGraph is the dedicated named graph run provenance lives in (design.md D5): the
	// dataset runs with unionDefaultGraph=on, which shadows the stored default graph, so
	// provenance needs its own named graph to stay queryable.
	provenanceGraph = gsRunNS + "_provenance"

	// runGraphPrefix identifies a run's own named graph: run:load-<runID>.
	runGraphPrefix = gsRunNS + "load-"

	// scratchGraphPrefix identifies the transient validation-merge graph: run:validate-<runID>,
	// always dropped again once validation finishes (see (*client).validate).
	scratchGraphPrefix = gsRunNS + "validate-"

	// provNS/xsdNS namespace the provenance triple provenanceTurtle builds.
	provNS = "http://www.w3.org/ns/prov#"
	xsdNS  = "http://www.w3.org/2001/XMLSchema#"
)

// runIDLayout is the UTC timestamp layout run IDs are stamped with — nanosecond precision so two
// Load calls within the same process (e.g. back-to-back in a test) never collide on a run graph.
const runIDLayout = "20060102T150405.000000000Z"

// newRunID returns a UTC-timestamp run ID derived from now, used both for a run's own named graph
// (run:load-<runID>) and its scratch validation graph (run:validate-<runID>).
func newRunID(now time.Time) string {
	return now.UTC().Format(runIDLayout)
}

// runGraph returns the run-stamped named graph IRI for runID (run:load-<runID>).
func runGraph(runID string) string {
	return runGraphPrefix + runID
}

// scratchGraphFor returns the transient validation-merge graph IRI for runID
// (run:validate-<runID>).
func scratchGraphFor(runID string) string {
	return scratchGraphPrefix + runID
}

// provenanceTurtle builds the run's prov:Activity triple (design.md D5):
// run:load-<runID> a prov:Activity ; prov:generatedAtTime "<generatedAt>"^^xsd:dateTime .
func provenanceTurtle(runID string, generatedAt time.Time) []byte {
	turtle := fmt.Sprintf(
		"@prefix prov: <%s> .\n@prefix xsd: <%s> .\n<%s> a prov:Activity ; prov:generatedAtTime \"%s\"^^xsd:dateTime .\n",
		provNS, xsdNS, runGraph(runID), generatedAt.UTC().Format(time.RFC3339Nano),
	)
	return []byte(turtle)
}

// upsert runs the SCD2 idempotent-upsert pipeline (design.md D1-D6) for a candidate that has
// already passed the SHACL gate (D7 — validation runs before this is ever called): stage the
// candidate → extract its signatures (staging + live-open) → classify → close superseded priors →
// verify the open-version invariant → copy the delta into a fresh run:load-<runID> graph and record
// provenance. The staging graph is always dropped again before returning (defer, detached context,
// mirroring (*client).validate's own cleanup) — including when the delta is empty, so a true no-op
// run (D5) leaves no trace at all, not even a lingering staging graph.
func (c *client) upsert(ctx context.Context, runID string, candidate []byte, generatedAt time.Time) (err error) {
	stageGraph := stageGraphFor(runID)
	if stageErr := c.stageCandidate(ctx, runID, candidate); stageErr != nil {
		// stageCandidate's own error already names what failed ("graph: stage candidate into
		// %s: ...") — do not re-wrap with the same phrase.
		return stageErr
	}
	defer func() {
		if dropErr := c.dropGraph(context.WithoutCancel(ctx), stageGraph); dropErr != nil && err == nil {
			err = fmt.Errorf("clean up staging graph %s: %w", stageGraph, dropErr)
		}
	}()

	stagingSigs, err := c.stagingSignatures(ctx, runID)
	if err != nil {
		return err
	}
	liveSigs, err := c.liveOpenSignatures(ctx)
	if err != nil {
		return err
	}

	// Security gate: every subject IRI in either signature map was read back out of the store
	// (STR(?s) in signatureQuery) and is about to be re-embedded, unescaped, into `<...>` tokens
	// by iriValuesList across closePriors/verifyOpenInvariant/copyDeltaAndRecordProvenance/
	// stagingValidFrom below. The SHACL gate upstream does not constrain IRI/literal character
	// content, so a conformant-looking candidate can still smuggle a Turtle IRIREF UCHAR payload
	// that decodes to raw `>`/`}`/`;` sequences able to break out of a VALUES clause and chain
	// arbitrary SPARQL Update. Validate BEFORE any query or update text is built, and write
	// NOTHING on failure (no close, no run graph, no provenance) — the same no-partial-writes
	// guarantee the SHACL gate gives. delta/changed are always subsets of these two maps' keys,
	// so this single check covers every downstream iriValuesList call in this function.
	for id := range stagingSigs {
		if err := assertSafeIRI(id); err != nil {
			return fmt.Errorf("graph: reject unsafe candidate subject IRI: %w", err)
		}
	}
	for id := range liveSigs {
		if err := assertSafeIRI(id); err != nil {
			return fmt.Errorf("graph: reject unsafe stored subject IRI: %w", err)
		}
	}

	newIRIs, changed, _, immutableConflict := classify(stagingSigs, liveSigs)

	// D4: an immutable conflict is skip-but-surface — never overwritten, but logged so the
	// anomaly can be investigated later. Aggregated into ONE line, not one per subject: the
	// expected koop-Place-vs-seeded-skeleton collision (koop re-asserts a bare `a gs:Place` for
	// every buurt it touches, design.md D5) yields one conflict per Noord buurt, and a line each
	// buried the load's real output. The full IRI list stays on the single line for investigation.
	if len(immutableConflict) > 0 {
		ids := make([]string, len(immutableConflict))
		for i, id := range immutableConflict {
			ids[i] = string(id)
		}
		log.Printf("graph: %d immutable content conflict(s), stored version(s) retained (candidate content differs): %s",
			len(immutableConflict), strings.Join(ids, ", "))
	}

	delta := make([]iri, 0, len(newIRIs)+len(changed))
	delta = append(delta, newIRIs...)
	delta = append(delta, changed...)
	if len(delta) == 0 {
		// D5: a no-op run leaves no trace — no run:load-... graph, no prov:Activity. The
		// staging graph is still dropped by the deferred cleanup above.
		return nil
	}

	if len(changed) > 0 {
		validFrom, vfErr := c.stagingValidFrom(ctx, runID, changed)
		if vfErr != nil {
			return fmt.Errorf("read candidate validFrom for closing priors: %w", vfErr)
		}
		closeStamps := make(map[iri]string, len(changed))
		for _, id := range changed {
			vf, ok := validFrom[id]
			if !ok {
				return fmt.Errorf("changed entity %s carries no gs:validFrom in the candidate — the writer cannot close its prior without the caller-owned world-time stamp (design.md D3)", id)
			}
			closeStamps[id] = vf
		}
		if err := c.closePriors(ctx, closeStamps); err != nil {
			// closePriors already passes "close prior versions" as the (*client).update action —
			// do not re-wrap with the same phrase.
			return err
		}
		// Verify BEFORE writing the new run graph / provenance (design.md risk "Open-version
		// ambiguity"): on failure, Load has still written no run:load-... graph and no
		// prov:Activity for this run — only the (correct, non-destructive) validTo stamps
		// from the close above.
		if err := c.verifyOpenInvariant(ctx, runID, changed); err != nil {
			return err
		}
	}

	if err := c.copyDeltaAndRecordProvenance(ctx, runID, stageGraph, delta, generatedAt); err != nil {
		return err
	}
	return nil
}

// closePriors stamps gs:validTo = the candidate's new gs:validFrom onto EVERY currently-open prior
// version of each changed evolving entity, across every run:load-* graph — self-healing (design.md
// risk "Open-version ambiguity"): if a stray extra-open version exists, it too gets closed rather
// than being left ambiguous. It never DELETEs; only INSERTs the validTo stamp, so history is never
// destroyed (design.md D3). closeStamps maps each changed subject IRI to its new version's
// gs:validFrom, already rendered as a SPARQL literal term (see literalTerm) — read back from the
// staging graph by the caller, never invented by the writer itself.
func (c *client) closePriors(ctx context.Context, closeStamps map[iri]string) error {
	if len(closeStamps) == 0 {
		return nil
	}

	pairs := make([]string, 0, len(closeStamps))
	for id, validTo := range closeStamps {
		pairs = append(pairs, "(<"+string(id)+"> "+validTo+")")
	}
	sort.Strings(pairs) // deterministic query text; not semantically required.

	update := fmt.Sprintf(`
PREFIX gs: <%s>
INSERT {
  GRAPH ?g { <<?s ?p ?o>> gs:validTo ?newValidTo }
}
WHERE {
  VALUES (?s ?newValidTo) { %s }
  GRAPH ?g {
    <<?s ?p ?o>> gs:validFrom ?existingValidFrom .
    FILTER NOT EXISTS { <<?s ?p ?o>> gs:validTo ?anyValidTo }
  }
  FILTER(STRSTARTS(STR(?g), "%s"))
}`, gsNS, strings.Join(pairs, " "), runGraphPrefix)

	return c.update(ctx, update, "close prior versions")
}

// verifyOpenInvariant confirms, for each changed subject, that exactly one open version would exist
// once the delta copy below actually runs: it counts open annotated edges across every run:load-*
// graph (post-close — should be zero for a healthy close) UNION the not-yet-copied new version
// still sitting in the run's staging graph (always exactly one). Running BEFORE the delta
// copy/provenance write means a failure here still leaves no run:load-... graph and no
// prov:Activity for this run (task 3.2) — the anomaly is surfaced loudly instead of writing
// ambiguous history.
func (c *client) verifyOpenInvariant(ctx context.Context, runID string, changed []iri) error {
	if len(changed) == 0 {
		return nil
	}

	query := fmt.Sprintf(`
PREFIX gs: <%s>
SELECT ?s (COUNT(*) AS ?openCount) WHERE {
  VALUES ?s { %s }
  {
    GRAPH ?g {
      <<?s ?p ?o>> gs:validFrom ?vf .
      FILTER NOT EXISTS { <<?s ?p ?o>> gs:validTo ?vt }
    }
    FILTER(STRSTARTS(STR(?g), "%s"))
  }
  UNION
  {
    GRAPH <%s> {
      <<?s ?sp ?so>> gs:validFrom ?vf2 .
    }
  }
}
GROUP BY ?s
HAVING (COUNT(*) != 1)
`, gsNS, iriValuesList(changed), runGraphPrefix, stageGraphFor(runID))

	violations, err := c.selectColumn(ctx, query, "s")
	if err != nil {
		return fmt.Errorf("verify open-version invariant: %w", err)
	}
	if len(violations) > 0 {
		return fmt.Errorf("open-version invariant violated for %d entit(ies), expected exactly one open version each: %v", len(violations), violations)
	}
	return nil
}

// copyDeltaAndRecordProvenance copies every delta (new ∪ changed) entity's triples and RDF-star
// annotations from the staging graph into the run's own fresh run:load-<runID> graph, in one
// batched SPARQL Update (design.md D6 — no per-entity round-trips), then records the run's
// prov:Activity (design.md D5). The writer never constructs the RDF-star annotation itself here
// either — it only copies whatever the (already-validated) candidate carries.
//
// Jena 5.5 represents an RDF-star/1.2 annotation as a blank-node reifier: the annotation
// properties (gs:confidence, gs:evidence, gs:validFrom, ...) live on a blank node ?r, keyed to the
// base triple via `?r rdf:reifies <<( ?s ?p ?o )>>` (SPARQL/Turtle 1.2 triple-term syntax). That
// reifier is never itself one of the delta subjects (it's a blank node, not a data: IRI), so the
// second INSERT copies it explicitly by matching the reifier pattern in staging and re-asserting
// it verbatim in the destination graph — this reproduces the exact reifier structure (same blank
// node identity is not required; only the reifies-linkage + annotation properties need to survive)
// so the destination graph is byte-identical in content to staging.
func (c *client) copyDeltaAndRecordProvenance(ctx context.Context, runID, stageGraph string, deltaIRIs []iri, generatedAt time.Time) error {
	dest := runGraph(runID)
	values := iriValuesList(deltaIRIs)

	update := fmt.Sprintf(`
PREFIX gs: <%s>
PREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#>
INSERT { GRAPH <%s> { ?s ?p ?o } }
WHERE {
  VALUES ?s { %s }
  GRAPH <%s> { ?s ?p ?o }
} ;
INSERT { GRAPH <%s> { ?r rdf:reifies <<( ?s ?p ?o )>> . ?r ?ap ?av } }
WHERE {
  VALUES ?s { %s }
  GRAPH <%s> {
    ?r rdf:reifies <<( ?s ?p ?o )>> .
    ?r ?ap ?av .
  }
}`, gsNS, dest, values, stageGraph, dest, values, stageGraph)

	if err := c.update(ctx, update, "copy delta into "+dest); err != nil {
		// The action string above already names what failed ("graph: copy delta into %s: ...")
		// — do not re-wrap with the same phrase.
		return err
	}
	if err := c.postGraph(ctx, provenanceGraph, provenanceTurtle(runID, generatedAt)); err != nil {
		return fmt.Errorf("write provenance for run %s: %w", runID, err)
	}
	return nil
}
