package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// fusekiAdminUser is the fixed admin username Fuseki's admin API and data endpoints expect; the
// dev-compose stack (deploy/compose/compose.yaml) only configures the password via
// FUSEKI_ADMIN_PASSWORD — mirrors internal/testdb/fuseki.go's own convention.
const fusekiAdminUser = "admin"

// client is a minimal HTTP client for one Fuseki dataset (baseURL, e.g. "http://fuseki:3030/ds"):
// SPARQL Graph Store read/write, SPARQL Update, and the dataset's /shacl validation endpoint. No
// in-process JVM — design.md D6.
type client struct {
	baseURL    string
	password   string
	httpClient *http.Client
}

// newClient builds a client for the Fuseki dataset at baseURL, reading the admin password from
// FUSEKI_ADMIN_PASSWORD via env (fail loud if unset — every write/validate call needs it).
func newClient(baseURL string, env func(string) string) (*client, error) {
	password := env("FUSEKI_ADMIN_PASSWORD")
	if password == "" {
		return nil, fmt.Errorf("graph: FUSEKI_ADMIN_PASSWORD is not set")
	}
	return &client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		password:   password,
		httpClient: http.DefaultClient,
	}, nil
}

// newRequest builds an authenticated request against the dataset (method + path, where path
// already includes any query string, e.g. "/data?graph=...").
func (c *client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("graph: build %s %s request: %w", method, path, err)
	}
	req.SetBasicAuth(fusekiAdminUser, c.password)
	return req, nil
}

// do executes req, reads the body, and turns a non-2xx status into an error carrying a body
// snippet — mirrors internal/testdb/fuseki.go's doAdminRequest.
func (c *client) do(req *http.Request, action string) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graph: %s: %w", action, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("graph: %s: read response body: %w", action, err)
	}
	if resp.StatusCode/100 != 2 {
		snippet := body
		if len(snippet) > 4096 {
			snippet = snippet[:4096]
		}
		return nil, fmt.Errorf("graph: %s: status %d: %s", action, resp.StatusCode, snippet)
	}
	return body, nil
}

// putGraph replaces graphIRI's entire content with turtle (PUT ".../data?graph=...") — used for
// the idempotent reference-model write: re-running Load always leaves exactly the embedded model
// in that graph, never an accumulation of duplicates.
func (c *client) putGraph(ctx context.Context, graphIRI string, turtle []byte) error {
	req, err := c.newRequest(ctx, http.MethodPut, "/data?graph="+url.QueryEscape(graphIRI), bytes.NewReader(turtle))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/turtle")
	_, err = c.do(req, "PUT graph "+graphIRI)
	return err
}

// postGraph merges turtle into graphIRI's existing content (POST ".../data?graph=...") — used to
// build the scratch validation graph (ontology + vocab + candidate) and to write the final,
// additive run:load-... graph and run:_provenance triple.
func (c *client) postGraph(ctx context.Context, graphIRI string, turtle []byte) error {
	req, err := c.newRequest(ctx, http.MethodPost, "/data?graph="+url.QueryEscape(graphIRI), bytes.NewReader(turtle))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/turtle")
	_, err = c.do(req, "POST graph "+graphIRI)
	return err
}

// update executes a SPARQL Update against the dataset's /update endpoint — the primitive behind
// dropGraph and, in write.go, the SCD2 close/copy operations (design.md D6): anything the Graph
// Store protocol (putGraph/postGraph) can't express because it needs a WHERE pattern rather than a
// whole-graph replace/merge.
func (c *client) update(ctx context.Context, sparqlUpdate, action string) error {
	req, err := c.newRequest(ctx, http.MethodPost, "/update", strings.NewReader("update="+url.QueryEscape(sparqlUpdate)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err = c.do(req, action)
	return err
}

// dropGraph removes graphIRI entirely via SPARQL Update. DROP SILENT is a no-op — not an error —
// if the graph does not exist, so cleanup (scratch/staging graphs) and Reset (prior run graphs)
// never fail on an absent graph.
func (c *client) dropGraph(ctx context.Context, graphIRI string) error {
	return c.update(ctx, "DROP SILENT GRAPH <"+graphIRI+">", "DROP GRAPH "+graphIRI)
}

// selectQuery executes a SPARQL SELECT via POST, body-carried, per the SPARQL 1.1 Protocol —
// mirrors update's request construction (POST + application/x-www-form-urlencoded "query="+body)
// but against /sparql rather than /update. Every SELECT in this package goes through here rather
// than GET-in-URI: a GET request encodes the entire query (including any VALUES list of subject
// IRIs) into the request URI, and Fuseki's default Jetty URI-length limit is easily overrun by a
// VALUES list over a few hundred IRIs — a real production failure (a ~16k-subject felled-trees
// load's series-close step 414'd). POST has no such limit tied to query size.
func (c *client) selectQuery(ctx context.Context, query, action string) ([]byte, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/sparql", strings.NewReader("query="+url.QueryEscape(query)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")
	return c.do(req, action)
}

// selectColumn runs a SPARQL SELECT and returns every value bound to variable varName across all
// solutions, in result order — a small generic reader shared by the signature extraction (2.2/2.3)
// and the SCD2 open-version invariant check (write.go), which both consume the SPARQL 1.1 JSON
// results format.
func (c *client) selectColumn(ctx context.Context, query, varName string) ([]string, error) {
	body, err := c.selectQuery(ctx, query, "SPARQL SELECT")
	if err != nil {
		return nil, err
	}

	var result struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("graph: SPARQL SELECT: parse SPARQL JSON results: %w", err)
	}

	var values []string
	for _, b := range result.Results.Bindings {
		if v, ok := b[varName]; ok {
			values = append(values, v.Value)
		}
	}
	return values, nil
}

// shaclValidate posts shapes against graphIRI's current content (the Fuseki /shacl endpoint —
// design.md D6) and returns the raw Turtle SHACL validation report.
func (c *client) shaclValidate(ctx context.Context, graphIRI string, shapes []byte) ([]byte, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/shacl?graph="+url.QueryEscape(graphIRI), bytes.NewReader(shapes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/turtle")
	return c.do(req, "SHACL validate "+graphIRI)
}

// graphListResult is the shape of a SPARQL JSON-results response for a single "?g" binding —
// enough of the W3C SPARQL 1.1 Query Results JSON format to read graphsWithPrefix's query back.
type graphListResult struct {
	Results struct {
		Bindings []struct {
			G struct {
				Value string `json:"value"`
			} `json:"g"`
		} `json:"bindings"`
	} `json:"results"`
}

// graphsWithPrefix returns every named graph currently present in the dataset whose IRI starts
// with prefix — used by Reset to discover prior run:load-... graphs to drop, without hardcoding or
// tracking run IDs across process runs.
func (c *client) graphsWithPrefix(ctx context.Context, prefix string) ([]string, error) {
	const query = `SELECT DISTINCT ?g WHERE { GRAPH ?g { ?s ?p ?o } }`
	body, err := c.selectQuery(ctx, query, "list named graphs")
	if err != nil {
		return nil, err
	}

	var result graphListResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("graph: list named graphs: parse SPARQL JSON results: %w", err)
	}

	var graphs []string
	for _, b := range result.Results.Bindings {
		if strings.HasPrefix(b.G.Value, prefix) {
			graphs = append(graphs, b.G.Value)
		}
	}
	return graphs, nil
}
