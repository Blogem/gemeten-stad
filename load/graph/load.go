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
	// (re)loading the reference model — a full rebuild from scratch. Absent, Load is an
	// idempotent upsert: a conforming candidate writes only the entities that are genuinely new
	// or changed relative to what the graph already holds (design.md D1-D6); an unchanged
	// re-run writes nothing at all (D5).
	Reset bool
}

// Load ensures the reference model (ontology.Ontology + ontology.Vocab) is present in the Fuseki
// dataset at fusekiURL — an idempotent PUT into run:_model, so re-running always leaves exactly
// the embedded model there, never an accumulation — then, if candidate is non-empty, validates it
// against ontology.Shapes using the merge-vocab recipe (see doc.go: SHACL controlled-value checks
// only see a concept's skos:inScheme triple when the concept and the candidate are in the SAME
// validated graph) and, on conform, runs the SCD2 idempotent-upsert write path (design.md D1-D6,
// write.go's upsert): the candidate is staged, diffed by subject IRI against the live open graph,
// classified into new/changed/unchanged/immutableConflict, and only the delta (new ∪ changed) is
// ever written — a changed evolving entity opens a new version and closes its prior (stamping
// gs:validTo), an unchanged entity is skipped entirely, and a delta-empty run leaves no trace: no
// run:load-<runID> graph, no prov:Activity (D5). Immutable content conflicts are skipped (never
// overwritten) but logged via log.Printf so the anomaly is surfaced (D4).
//
// On non-conform, nothing is written and the violation detail is returned as part of the error —
// the SHACL check is the loud-failing load gate, the graph analogue of load/geo/gates.go, and it
// runs before any staging/mutation (D7). With cfg.Reset, prior run:load-... graphs and
// run:_provenance are cleared first, before the reference model is (re)ensured — a clean rebuild,
// unaffected by the upsert logic above. With a nil/empty candidate, Load only (re)loads the
// reference model — the primitive the pipeline's load/derive stages call; full derive-stage
// assembly is out of scope for v0.
//
// It returns a non-nil error — the caller should exit non-zero — if any step fails: SHACL
// non-conformance, or the SCD2 open-version invariant check failing after a close (design.md risk
// "Open-version ambiguity") — in both cases no run:load-... graph and no prov:Activity are written
// for that run.
//
// Load is NOT safe for concurrent calls against the same dataset: the reference-model and
// provenance graphs are fixed names and the per-run scratch/stage/run graphs key off a timestamp,
// so two Loads racing within one clock tick could collide. The pipeline calls it sequentially; a
// future parallel caller must serialize per dataset (or mint collision-proof run IDs first).
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

	if err := c.upsert(ctx, runID, candidate, time.Now()); err != nil {
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
