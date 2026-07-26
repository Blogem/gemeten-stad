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
		return fmt.Errorf("stage candidate: %w", stageErr)
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

	newIRIs, changed, _, immutableConflict := classify(stagingSigs, liveSigs)

	// D4: an immutable conflict is skip-but-surface — never overwritten, but logged so the
	// anomaly can be investigated later.
	for _, id := range immutableConflict {
		log.Printf("graph: immutable content conflict for %s: candidate content differs from the stored version; stored version retained", id)
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
			return fmt.Errorf("close prior versions: %w", err)
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
func (c *client) copyDeltaAndRecordProvenance(ctx context.Context, runID, stageGraph string, deltaIRIs []iri, generatedAt time.Time) error {
	dest := runGraph(runID)
	values := iriValuesList(deltaIRIs)

	update := fmt.Sprintf(`
PREFIX gs: <%s>
INSERT { GRAPH <%s> { ?s ?p ?o } }
WHERE {
  VALUES ?s { %s }
  GRAPH <%s> { ?s ?p ?o }
} ;
INSERT { GRAPH <%s> { <<?s ?p ?o>> ?ap ?av } }
WHERE {
  VALUES ?s { %s }
  GRAPH <%s> { <<?s ?p ?o>> ?ap ?av }
}`, gsNS, dest, values, stageGraph, dest, values, stageGraph)

	if err := c.update(ctx, update, "copy delta into "+dest); err != nil {
		return fmt.Errorf("copy delta into %s: %w", dest, err)
	}
	if err := c.postGraph(ctx, provenanceGraph, provenanceTurtle(runID, generatedAt)); err != nil {
		return fmt.Errorf("write provenance for run %s: %w", runID, err)
	}
	return nil
}
