package coverage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// selectBindings runs a SPARQL SELECT against <fusekiURL>/sparql and returns one map (variable name
// -> bound value) per result row. It mirrors the ask/count pattern in
// load/graph/load_integration_test.go: a plain GET with an Accept: application/sparql-results+json
// header, decoding the standard SPARQL 1.1 results JSON. Kept dependency-free (net/http +
// encoding/json only) since this is the only SPARQL the derive step needs (a read-only enumeration
// of Interventions) — writing goes through the existing P12 load/graph gate, not this helper.
func selectBindings(ctx context.Context, fusekiURL, query string) ([]map[string]string, error) {
	base := strings.TrimRight(fusekiURL, "/")
	reqURL := base + "/sparql?query=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("coverage: build SPARQL request: %w", err)
	}
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coverage: SPARQL request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("coverage: read SPARQL response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coverage: SPARQL request failed: %s: %s", resp.Status, string(body))
	}

	var parsed struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("coverage: decode SPARQL response: %w", err)
	}

	rows := make([]map[string]string, 0, len(parsed.Results.Bindings))
	for _, binding := range parsed.Results.Bindings {
		row := make(map[string]string, len(binding))
		for name, v := range binding {
			row[name] = v.Value
		}
		rows = append(rows, row)
	}
	return rows, nil
}
