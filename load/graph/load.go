package graph

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Blogem/gemeten-stad/ontology"
)

// Config configures a graph Load run.
type Config struct {
	// Reset clears prior run graphs (every run:load-... graph, plus run:_provenance) before
	// (re)loading the reference model — a clean rebuild. Absent, Load is additive: a conforming
	// candidate gets its own new run:load-... graph alongside any prior ones.
	Reset bool
}

// Load ensures the reference model (ontology.Ontology + ontology.Vocab) is present in the Fuseki
// dataset at fusekiURL — an idempotent PUT into run:_model, so re-running always leaves exactly
// the embedded model there, never an accumulation — then, if candidate is non-empty, validates it
// against ontology.Shapes using the merge-vocab recipe (see doc.go: SHACL controlled-value checks
// only see a concept's skos:inScheme triple when the concept and the candidate are in the SAME
// validated graph) and, on conform, writes it into a run-stamped run:load-<runID> named graph plus
// a prov:Activity in run:_provenance (design.md D5).
//
// On non-conform, nothing is written and the violation detail is returned as part of the error —
// the SHACL check is the loud-failing load gate, the graph analogue of load/geo/gates.go. With
// cfg.Reset, prior run:load-... graphs and run:_provenance are cleared first, before the reference
// model is (re)ensured. With a nil/empty candidate, Load only (re)loads the reference model — the
// primitive the pipeline's load/derive stages call; full derive-stage assembly is out of scope for
// v0.
//
// It returns a non-nil error — the caller should exit non-zero — if any step, including SHACL
// non-conformance, fails.
func Load(ctx context.Context, fusekiURL string, candidate []byte, cfg Config) error {
	c, err := newClient(fusekiURL, os.Getenv)
	if err != nil {
		return fmt.Errorf("graph: load: %w", err)
	}

	if cfg.Reset {
		if err := resetRunGraphs(ctx, c); err != nil {
			return fmt.Errorf("graph: load: reset: %w", err)
		}
	}

	if err := c.putGraph(ctx, referenceModelGraph, ontology.Ontology); err != nil {
		return fmt.Errorf("graph: load: ensure reference model (ontology): %w", err)
	}
	if err := c.postGraph(ctx, referenceModelGraph, ontology.Vocab); err != nil {
		return fmt.Errorf("graph: load: ensure reference model (vocab): %w", err)
	}

	if len(candidate) == 0 {
		return nil
	}

	runID := newRunID(time.Now())
	conforms, detail, err := c.validate(ctx, candidate, scratchGraphFor(runID))
	if err != nil {
		return fmt.Errorf("graph: load: validate: %w", err)
	}
	if !conforms {
		return fmt.Errorf("graph: load: candidate does not conform to shapes: %s", detail)
	}

	if err := c.writeCandidate(ctx, runID, candidate, time.Now()); err != nil {
		return fmt.Errorf("graph: load: %w", err)
	}
	return nil
}

// resetRunGraphs drops every prior run:load-... graph plus run:_provenance, for cfg.Reset's clean
// rebuild. The reference model graph (run:_model) is deliberately not dropped here: Load
// unconditionally rewrites it right after via PUT (itself idempotent), so dropping it first would
// just be redundant churn.
func resetRunGraphs(ctx context.Context, c *client) error {
	graphs, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	if err != nil {
		return fmt.Errorf("list prior run graphs: %w", err)
	}
	for _, g := range graphs {
		if err := c.dropGraph(ctx, g); err != nil {
			return fmt.Errorf("drop prior run graph %s: %w", g, err)
		}
	}
	if err := c.dropGraph(ctx, provenanceGraph); err != nil {
		return fmt.Errorf("drop provenance graph: %w", err)
	}
	return nil
}
