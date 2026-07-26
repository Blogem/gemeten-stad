// Command pipeline is the batch backbone of Gemeten Stad. It exposes the
// raw → conformed → derived stages (plus snapshotting) as subcommands. Every
// subcommand is idempotent and incremental; see docs/IMPLEMENTATION_PLAN.md §5.
//
// ingest and load are wired to the geo backbone (BAG + gebieden landing, and
// the PostGIS geo load) and the bomen tree registry (kapenherplant +
// stamgegevens landing and load). extract/derive/dump remain no-op stubs —
// later Phase-0/1 work items fill in their behaviour.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Blogem/gemeten-stad/ingest/bag"
	"github.com/Blogem/gemeten-stad/ingest/bomen"
	"github.com/Blogem/gemeten-stad/ingest/gebieden"
	"github.com/Blogem/gemeten-stad/ingest/shared"
	loadbomen "github.com/Blogem/gemeten-stad/load/bomen"
	"github.com/Blogem/gemeten-stad/load/geo"
)

// ingestSource pairs a registered ingest source name with its ingester
// function. ingestRegistry below fixes the registration order that "run all
// sources" follows.
type ingestSource struct {
	name string
	fn   func(context.Context, *shared.RawStore) error
}

// ingestRegistry is the single, ordered source of truth for ingest sources:
// name → ingester. BAG runs before gebieden/CBS, then bomen, when all
// sources are ingested.
var ingestRegistry = []ingestSource{
	{name: "bag", fn: func(ctx context.Context, store *shared.RawStore) error {
		return bag.Ingest(ctx, store, nil)
	}},
	{name: "gebieden", fn: func(ctx context.Context, store *shared.RawStore) error {
		return gebieden.Ingest(ctx, store, gebiedenHTTPGet)
	}},
	{name: "bomen", fn: func(ctx context.Context, store *shared.RawStore) error {
		return bomen.Ingest(ctx, store, bomenHTTPGet)
	}},
}

// ingestRegistryNames returns the ingestRegistry's source names, in registration order.
func ingestRegistryNames() []string {
	names := make([]string, 0, len(ingestRegistry))
	for _, s := range ingestRegistry {
		names = append(names, s.name)
	}
	return names
}

// selectSources resolves requested ingest source names against ingestRegistry. See
// resolveSources for the resolution rules.
func selectSources(args []string) (names []string, err error) {
	return resolveSources(args, ingestRegistryNames())
}

// resolveSources resolves requested source names against valid. Empty args -> all names in
// valid's order. Named args -> those names, in the given order. An unknown name -> error listing
// the valid names (and nothing selected).
func resolveSources(args []string, valid []string) (names []string, err error) {
	if len(args) == 0 {
		return valid, nil
	}

	known := make(map[string]bool, len(valid))
	for _, name := range valid {
		known[name] = true
	}

	for _, arg := range args {
		if !known[arg] {
			return nil, fmt.Errorf("unknown source %q (valid: %s)", arg, strings.Join(valid, ", "))
		}
	}
	return args, nil
}

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
// LV extract and the Amsterdam gebieden / CBS boundary geometries. With no
// arguments it ingests all registered sources; named arguments ingest only
// those sources, in the given order.
func newIngestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ingest [source ...]",
		Short: "Land raw source data verbatim + provenance (bronze)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIngest(cmd.Context(), args)
		},
	}
}

func runIngest(ctx context.Context, args []string) error {
	names, err := selectSources(args)
	if err != nil {
		return err
	}

	rawPath, err := shared.RawDataPath(os.Getenv)
	if err != nil {
		return fmt.Errorf("resolve raw data path: %w", err)
	}
	store := shared.NewRawStore(rawPath)

	sources := make(map[string]ingestSource, len(ingestRegistry))
	for _, s := range ingestRegistry {
		sources[s.name] = s
	}

	for _, name := range names {
		if err := sources[name].fn(ctx, store); err != nil {
			return fmt.Errorf("ingest %s: %w", name, err)
		}
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
		_ = resp.Body.Close()
		return nil, fmt.Errorf("get %s: unexpected status %s", url, resp.Status)
	}
	return resp.Body, nil
}

// bomenHTTPGet is a real HTTP getter for ingest/bomen.Ingest. GS_BOMEN_API_KEY is read only
// here: when set, it is sent as the X-Api-Key header on every request.
func bomenHTTPGet(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}
	if apiKey := os.Getenv("GS_BOMEN_API_KEY"); apiKey != "" {
		req.Header.Set("X-Api-Key", apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("get %s: unexpected status %s", url, resp.Status)
	}
	return resp.Body, nil
}

// loadSource pairs a registered load source name with its loader function. loadRegistry below
// fixes the registration order that "run all sources" follows.
type loadSource struct {
	name string
	fn   func(ctx context.Context, reset bool) error
}

// loadRegistry is the single, ordered source of truth for load sources: name → loader. geo (BAG +
// gebieden) runs before bomen when all sources are loaded.
var loadRegistry = []loadSource{
	{name: "geo", fn: runGeoLoad},
	{name: "bomen", fn: runBomenLoad},
}

// loadRegistryNames returns the loadRegistry's source names, in registration order.
func loadRegistryNames() []string {
	names := make([]string, 0, len(loadRegistry))
	for _, s := range loadRegistry {
		names = append(names, s.name)
	}
	return names
}

// newLoadCmd maps, resolves location, and assembles entities into the stores (silver): the geo
// load of BAG + gebieden raw data into PostGIS, and the bomen load of the tree registry. With no
// arguments it loads all registered sources; named arguments load only those sources, in the
// given order.
func newLoadCmd() *cobra.Command {
	var reset bool

	cmd := &cobra.Command{
		Use:   "load [source ...]",
		Short: "Map, resolve location, and assemble entities into the stores (silver)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLoad(cmd.Context(), args, reset)
		},
	}
	cmd.Flags().BoolVar(&reset, "reset", false, "drop and recreate the target tables before loading")
	return cmd
}

func runLoad(ctx context.Context, args []string, reset bool) error {
	names, err := resolveSources(args, loadRegistryNames())
	if err != nil {
		return err
	}

	sources := make(map[string]loadSource, len(loadRegistry))
	for _, s := range loadRegistry {
		sources[s.name] = s
	}

	for _, name := range names {
		if err := sources[name].fn(ctx, reset); err != nil {
			return fmt.Errorf("load %s: %w", name, err)
		}
	}
	return nil
}

// runGeoLoad loads the BAG + gebieden raw data into PostGIS via the gdal sidecar's ogr2ogr.
func runGeoLoad(ctx context.Context, reset bool) error {
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

// runBomenLoad loads the landed bomen tree-registry export into PostGIS.
func runBomenLoad(ctx context.Context, reset bool) error {
	dbURL, err := shared.DatabaseURL(os.Getenv)
	if err != nil {
		return fmt.Errorf("resolve database url: %w", err)
	}
	pool, err := shared.ConnectPostgres(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	rawPath, err := shared.RawDataPath(os.Getenv)
	if err != nil {
		return fmt.Errorf("resolve raw data path: %w", err)
	}
	store := shared.NewRawStore(rawPath)

	cfg := loadbomen.Config{Reset: reset}
	if err := loadbomen.Load(ctx, pool, store, cfg); err != nil {
		return fmt.Errorf("load bomen: %w", err)
	}
	return nil
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
