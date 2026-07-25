// Command pipeline is the batch backbone of Gemeten Stad. It exposes the
// raw → conformed → derived stages (plus snapshotting) as subcommands. Every
// subcommand is idempotent and incremental; see docs/IMPLEMENTATION_PLAN.md §5.
//
// ingest and load are wired to the geo backbone (BAG + gebieden landing, and
// the PostGIS geo load). extract/derive/dump remain no-op stubs — later
// Phase-0/1 work items fill in their behaviour.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/Blogem/gemeten-stad/ingest/bag"
	"github.com/Blogem/gemeten-stad/ingest/gebieden"
	"github.com/Blogem/gemeten-stad/ingest/shared"
	"github.com/Blogem/gemeten-stad/load/geo"
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
		newIngestCmd(),
		newStageCmd("extract", "Extract structured facts from unstructured text (cached)"),
		newLoadCmd(),
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

// newIngestCmd lands raw source data verbatim + provenance (bronze): the BAG
// LV extract and the Amsterdam gebieden / CBS boundary geometries.
func newIngestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ingest",
		Short: "Land raw source data verbatim + provenance (bronze)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIngest(cmd.Context())
		},
	}
}

func runIngest(ctx context.Context) error {
	rawPath, err := shared.RawDataPath(os.Getenv)
	if err != nil {
		return fmt.Errorf("resolve raw data path: %w", err)
	}
	store := shared.NewRawStore(rawPath)

	if err := bag.Ingest(ctx, store, nil); err != nil {
		return fmt.Errorf("ingest bag: %w", err)
	}
	if err := gebieden.Ingest(ctx, store, gebiedenHTTPGet); err != nil {
		return fmt.Errorf("ingest gebieden: %w", err)
	}
	return nil
}

// gebiedenHTTPGet is a real HTTP getter for ingest/gebieden.Ingest, which
// requires per-request headers and does not default a nil getter.
func gebiedenHTTPGet(ctx context.Context, url string, headers map[string]string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("get %s: unexpected status %s", url, resp.Status)
	}
	return resp.Body, nil
}

// newLoadCmd maps, resolves location, and assembles entities into the stores
// (silver): the geo load of BAG + gebieden raw data into PostGIS.
func newLoadCmd() *cobra.Command {
	var reset bool

	cmd := &cobra.Command{
		Use:   "load",
		Short: "Map, resolve location, and assemble entities into the stores (silver)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLoad(cmd.Context(), reset)
		},
	}
	cmd.Flags().BoolVar(&reset, "reset", false, "drop and recreate the geo tables before loading")
	return cmd
}

func runLoad(ctx context.Context, reset bool) error {
	dbURL, err := shared.DatabaseURL(os.Getenv)
	if err != nil {
		return fmt.Errorf("resolve database url: %w", err)
	}
	pool, err := shared.ConnectPostgres(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	sc := shared.NewSidecar(shared.GDALPrefix(os.Getenv))

	rawPath, err := shared.RawDataPath(os.Getenv)
	if err != nil {
		return fmt.Errorf("resolve raw data path: %w", err)
	}
	store := shared.NewRawStore(rawPath)

	cfg := geo.Config{
		Reset:  reset,
		PGConn: geo.PGConnFromEnv(os.Getenv),
	}
	if err := geo.Load(ctx, pool, sc, store, cfg); err != nil {
		return fmt.Errorf("load geo: %w", err)
	}
	return nil
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
