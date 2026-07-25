// Command server hosts the Gemeten Stad API and the SvelteKit SPA. It is
// layered controller → service → repository (see docs/IMPLEMENTATION_PLAN.md §5)
// and is deployed separately from the batch pipeline.
//
// This is the minimal reachable shell: it reads its store-connection config from
// the environment and serves a /healthz probe so it can run as a health-checked
// dev-compose service. The real HTTP surface (and the graph/value-store clients
// the config points at) lands in a later phase.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

// config holds the env-driven runtime configuration. The GS_* names are the
// convention the rest of the stack (pipeline, later server layers) reuses.
type config struct {
	addr        string // GS_SERVER_ADDR — listen address, e.g. ":8080"
	fusekiURL   string // GS_FUSEKI_URL — triplestore dataset endpoint
	databaseURL string // GS_DATABASE_URL — PostGIS connection string
}

// configFromEnv resolves configuration from the environment, applying dev
// defaults. env is injected for testability (pass os.Getenv in main).
func configFromEnv(env func(string) string) config {
	addr := env("GS_SERVER_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	return config{
		addr:        addr,
		fusekiURL:   env("GS_FUSEKI_URL"),
		databaseURL: env("GS_DATABASE_URL"),
	}
}

// newHandler builds the HTTP surface. For now that is only the /healthz probe.
func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	return mux
}

// healthHandler reports liveness. It does not (yet) check the stores — it only
// confirms the process is up and serving.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(w, "ok")
}

// runHealthcheck probes the local /healthz endpoint and returns an error if it
// is not healthy. It is used by the container healthcheck (the distroless image
// has no shell/curl), driven by the -healthcheck flag.
func runHealthcheck(addr string) error {
	// addr is a listen address like ":8080"; probe it on the loopback host.
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address %q: %w", addr, err)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%s/healthz", net.JoinHostPort(host, port))

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: status %d", resp.StatusCode)
	}
	return nil
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local /healthz endpoint and exit")
	flag.Parse()

	cfg := configFromEnv(os.Getenv)

	if *healthcheck {
		if err := runHealthcheck(cfg.addr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	log.Printf("server: listening on %s", cfg.addr)
	log.Printf("server: fuseki=%q database=%q", cfg.fusekiURL, cfg.databaseURL)

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           newHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
