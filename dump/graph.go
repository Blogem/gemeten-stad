package dump

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ExportGraph fetches the whole Fuseki dataset at fusekiURL (every named graph plus the default
// graph) as N-Quads-star and writes it, gzip-compressed, to w. This is Jena's dataset-level GET
// (design D2) — the same operation behind RDFConnection.fetchDataset() — so PROV named graphs and
// RDF-star confidence annotations both survive on the wire: Jena's N-Quads writer expands `{| … |}`
// annotations into their `rdf:reifies`/quoted-triple quads.
func ExportGraph(ctx context.Context, fusekiURL string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fusekiURL, nil)
	if err != nil {
		return fmt.Errorf("dump: build graph export request: %w", err)
	}
	req.Header.Set("Accept", "application/n-quads")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("dump: fetch graph from %s: %w", fusekiURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dump: fetch graph from %s: %w", fusekiURL, statusError(resp))
	}

	gz := gzip.NewWriter(w)
	if _, err := io.Copy(gz, resp.Body); err != nil {
		return fmt.Errorf("dump: write graph export: %w", err)
	}
	return gz.Close()
}

// NamedGraphs queries fusekiURL for the distinct named graph IRIs currently present in the
// dataset — used at export time to record which graphs the bundle carries (manifest), so restore
// knows exactly which graphs to DROP without re-parsing the N-Quads-star artifact.
func NamedGraphs(ctx context.Context, fusekiURL string) ([]string, error) {
	const query = "SELECT DISTINCT ?g WHERE { GRAPH ?g { ?s ?p ?o } }"
	var result struct {
		Results struct {
			Bindings []struct {
				G struct {
					Value string `json:"value"`
				} `json:"g"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := sparqlQuery(ctx, fusekiURL, query, &result); err != nil {
		return nil, err
	}

	graphs := make([]string, 0, len(result.Results.Bindings))
	for _, b := range result.Results.Bindings {
		graphs = append(graphs, b.G.Value)
	}
	return graphs, nil
}

// HasDefaultGraphData reports whether fusekiURL's default graph (excluding every named graph)
// holds any triples.
func HasDefaultGraphData(ctx context.Context, fusekiURL string) (bool, error) {
	const query = "ASK { ?s ?p ?o }"
	var result struct {
		Boolean bool `json:"boolean"`
	}
	if err := sparqlQuery(ctx, fusekiURL, query, &result); err != nil {
		return false, err
	}
	return result.Boolean, nil
}

// RestoreGraph clears each named graph in namedGraphs (and the default graph, if hasDefaultGraph)
// via the update endpoint, then loads r — raw, already-decompressed N-Quads-star — back into
// fusekiURL via the dataset-level POST. Restore is additive (design D6): graphs not named here are
// left untouched, and DROP/CLEAR use SILENT so restoring into a fresh, graph-less target (the
// clean-volume round-trip) is not an error.
func RestoreGraph(ctx context.Context, fusekiURL string, namedGraphs []string, hasDefaultGraph bool, r io.Reader) error {
	if err := clearGraphs(ctx, fusekiURL, namedGraphs, hasDefaultGraph); err != nil {
		return err
	}
	return loadGraph(ctx, fusekiURL, r)
}

func clearGraphs(ctx context.Context, fusekiURL string, namedGraphs []string, hasDefaultGraph bool) error {
	stmts := make([]string, 0, len(namedGraphs)+1)
	for _, g := range namedGraphs {
		stmts = append(stmts, fmt.Sprintf("DROP SILENT GRAPH <%s>", g))
	}
	if hasDefaultGraph {
		stmts = append(stmts, "CLEAR SILENT DEFAULT")
	}
	if len(stmts) == 0 {
		return nil
	}

	endpoint := strings.TrimRight(fusekiURL, "/") + "/update"
	body := url.Values{"update": {strings.Join(stmts, " ; ")}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("dump: build graph clear request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("dump: clear graphs at %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dump: clear graphs at %s: %w", endpoint, statusError(resp))
	}
	return nil
}

func loadGraph(ctx context.Context, fusekiURL string, r io.Reader) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fusekiURL, r)
	if err != nil {
		return fmt.Errorf("dump: build graph load request: %w", err)
	}
	req.Header.Set("Content-Type", "application/n-quads")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("dump: load graph into %s: %w", fusekiURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dump: load graph into %s: %w", fusekiURL, statusError(resp))
	}
	return nil
}

// sparqlQuery issues a SPARQL query (SELECT or ASK) at <fusekiURL>/sparql and decodes the
// application/sparql-results+json response into out.
func sparqlQuery(ctx context.Context, fusekiURL, query string, out any) error {
	endpoint := strings.TrimRight(fusekiURL, "/") + "/sparql?" + url.Values{"query": {query}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("dump: build SPARQL query request: %w", err)
	}
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("dump: query %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dump: query %s: %w", endpoint, statusError(resp))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("dump: decode SPARQL result from %s: %w", endpoint, err)
	}
	return nil
}

// statusError formats a non-2xx HTTP response as an error, including a bounded tail of the body.
func statusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("status %d: %s", resp.StatusCode, body)
}
