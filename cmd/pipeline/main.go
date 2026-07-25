// Command pipeline is the batch backbone of Gemeten Stad. It exposes the
// raw → conformed → derived stages (plus snapshotting) as subcommands. Every
// subcommand is idempotent and incremental; see docs/IMPLEMENTATION_PLAN.md §5.
//
// The subcommands are wired up but not yet implemented — later Phase-0/1 work
// items fill in their behaviour.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "pipeline",
		Short: "Gemeten Stad batch pipeline (ingest → extract → load → derive; dump)",
		Long: "pipeline runs the Gemeten Stad data backbone as idempotent, incremental " +
			"stages: ingest raw sources, extract from unstructured text, load into the " +
			"stores, derive cross-source audit results, and dump snapshots.",
	}
	root.AddCommand(
		newStageCmd("ingest", "Land raw source data verbatim + provenance (bronze)"),
		newStageCmd("extract", "Extract structured facts from unstructured text (cached)"),
		newStageCmd("load", "Map, resolve location, and assemble entities into the stores (silver)"),
		newStageCmd("derive", "Compute cross-source AuditLinks and replant progress (gold)"),
		newStageCmd("dump", "Snapshot the graph, PostGIS, and the NER cache"),
	)
	return root
}

// newStageCmd builds a no-op subcommand. Behaviour is implemented in later work items.
func newStageCmd(name, short string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: not yet implemented\n", name)
			return err
		},
	}
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
