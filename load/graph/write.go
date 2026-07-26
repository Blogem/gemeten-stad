package graph

import (
	"context"
	"fmt"
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

// writeCandidate writes a conforming candidate graph into its own run-stamped named graph
// (run:load-<runID>) and records the run's prov:Activity in the shared run:_provenance graph. Both
// writes POST (merge) rather than PUT: run:load-<runID> is fresh per call (a new runID), and
// run:_provenance accumulates one triple per run rather than being replaced.
func (c *client) writeCandidate(ctx context.Context, runID string, candidate []byte, generatedAt time.Time) error {
	if err := c.postGraph(ctx, runGraph(runID), candidate); err != nil {
		return fmt.Errorf("write run graph %s: %w", runGraph(runID), err)
	}
	if err := c.postGraph(ctx, provenanceGraph, provenanceTurtle(runID, generatedAt)); err != nil {
		return fmt.Errorf("write provenance for run %s: %w", runID, err)
	}
	return nil
}
