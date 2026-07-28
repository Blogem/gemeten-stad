//go:build integration

// model-felled-trees task 2.4: integration test for the bomen -> graph felling projection (task
// 2.1/2.2), against a real isolated internal/testdb Postgres schema (seeded through the real
// ingest/shared.RawStore.LandVersion landing seam, exactly like load_integration_test.go's own
// TestBomenLoad_EndToEnd) AND the shared "gs-test" SHACL-enabled Fuseki dataset load/graph's own
// integration tests use — mirroring load/koop/integration_harness_test.go's
// newSchemaPool/fusekiDatasetURL/resetFusekiRunGraphs pattern, which load/bomen does not otherwise
// need (its own tests are Postgres-only today).
//
// TODO(pass-2): this file assumes the bomen graph projection is wired into Load itself via a new
// fusekiURL parameter — Load(ctx, pool, store, fusekiURL, cfg) — mirroring load/koop.Load's exact
// precedent (the most recent sibling package to add "write a SHACL-gated turtle candidate as part
// of Load"), per task 2.2 ("wire it into the bomen load path (runs as part of load bomen)"). If the
// coder instead added a separate exported entry point (e.g. a dedicated LoadGraph function called
// alongside Load), replace every `Load(ctx, pool, store, fusekiURL, cfg)` call below with the two
// real calls in the real order, and drop the now-unused fusekiURL parameter from the Load(...)
// calls that don't need it. The SPARQL-observable assertions themselves (gs:Tree/gs:Felling
// presence, no-op re-run mints no new run graph) are spec-derived and should not need to change.
package bomen

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
	"github.com/Blogem/gemeten-stad/internal/testdb"
)

// -- Fuseki: the shared gs-test dataset (mirrors load/koop/integration_harness_test.go) ----------

const fusekiTestDataset = "gs-test"

// gsRunGraphPrefix mirrors load/graph/write.go's own (private) runGraphPrefix constant
// ("http://gemetenstad.nl/run/load-") — load/bomen can only observe graph.Load's run graphs via
// SPARQL, not by importing load/graph's unexported internals, so the prefix is duplicated here
// deliberately, exactly as load/koop/integration_harness_test.go and
// derive/coverage/integration_harness_test.go both already do.
const gsRunGraphPrefix = "http://gemetenstad.nl/run/load-"

// fusekiDatasetURL returns the gs-test dataset URL (GS_TEST_FUSEKI_URL + "/gs-test"), guarded
// against ever being a reserved production/dev name.
func fusekiDatasetURL(t *testing.T) string {
	t.Helper()
	root := requireEnv(t, "GS_TEST_FUSEKI_URL")
	require.NotEmptyf(t, os.Getenv("FUSEKI_ADMIN_PASSWORD"), "FUSEKI_ADMIN_PASSWORD must be set")
	require.NoError(t, testdb.AssertNotProduction("", fusekiTestDataset), "must not target a production dataset")
	return strings.TrimRight(root, "/") + "/" + fusekiTestDataset
}

// doSPARQLGet runs a plain unauthenticated GET against dsURL's /sparql endpoint, mirroring
// derive/coverage/integration_harness_test.go's doSPARQLGet.
func doSPARQLGet(t *testing.T, ctx context.Context, dsURL, query string) []byte {
	t.Helper()
	reqURL := strings.TrimRight(dsURL, "/") + "/sparql?query=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "SPARQL query failed: %s", body)
	return body
}

// sparqlAsk runs a SPARQL ASK query against dsURL and returns the boolean result.
func sparqlAsk(t *testing.T, ctx context.Context, dsURL, query string) bool {
	t.Helper()
	body := doSPARQLGet(t, ctx, dsURL, query)
	var res struct {
		Boolean bool `json:"boolean"`
	}
	require.NoError(t, json.Unmarshal(body, &res))
	return res.Boolean
}

// graphsWithPrefix returns every named graph in dsURL whose IRI starts with prefix — used to assert
// an unchanged re-run mints no new run:load-... graph (mirrors
// load/koop/integration_harness_test.go's sparqlGraphsWithPrefix).
func graphsWithPrefix(t *testing.T, ctx context.Context, dsURL, prefix string) []string {
	t.Helper()
	body := doSPARQLGet(t, ctx, dsURL, `SELECT DISTINCT ?g WHERE { GRAPH ?g { ?s ?p ?o } }`)
	var result struct {
		Results struct {
			Bindings []struct {
				G struct {
					Value string `json:"value"`
				} `json:"g"`
			} `json:"bindings"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(body, &result))

	var graphs []string
	for _, b := range result.Results.Bindings {
		if strings.HasPrefix(b.G.Value, prefix) {
			graphs = append(graphs, b.G.Value)
		}
	}
	return graphs
}

// -- Fixture: one felled row, one never-felled row -------------------------------------------------

// fixtureKapenherplantForGraph is a minimal kapenherplant export for the graph-projection tests:
// kap-graph-felled carries a kapmaatregelDatumUitgevoerd (must be projected into the graph as a
// gs:Tree + gs:Felling); kap-graph-neverfelled carries none (must never enter the graph, per
// bomen-load/spec.md's "Only felled trees are loaded" scenario). Distinct ids/boomIds from every
// other fixture in this package (load_integration_test.go's fixtureKapenherplantV1/V2,
// isolation_integration_test.go's decoys) so this test's own isolated schema+store never collides
// with another test's fixture content.
func fixtureKapenherplantForGraph() []map[string]any {
	return []map[string]any{
		{
			"id": "kap-graph-felled", "boomId": "stam-graph-felled", "boomNieuwId": "", "gbdBuurtId": "A09",
			"dichtstbijzijndeBagAdres": "Grafteststraat 1", "postcode": "1099ZZ",
			"soortnaam": "Tilia", "toeTePassenBoomsoort": "Tilia",
			"datumVergunningsaanvraag":      "2023-01-01T00:00:00Z",
			"kapmaatregelDatumUitgevoerd":   "2023-02-01T00:00:00Z",
			"plantmaatregelDatumUitgevoerd": "",
		},
		{
			"id": "kap-graph-neverfelled", "boomId": "stam-graph-neverfelled", "boomNieuwId": "", "gbdBuurtId": "A09",
			"dichtstbijzijndeBagAdres": "Grafteststraat 2", "postcode": "1099ZZ",
			"soortnaam": "Quercus", "toeTePassenBoomsoort": "Quercus",
			"datumVergunningsaanvraag":      "2023-01-01T00:00:00Z",
			"kapmaatregelDatumUitgevoerd":   "",
			"plantmaatregelDatumUitgevoerd": "",
		},
	}
}

// fixtureStamgegevensForGraph provides the matching stamgegevens rows so both kapenherplant rows
// above resolve their point (irrelevant to the graph projection itself, but keeps the fixture
// internally consistent with the real join contract, per load_integration_test.go's convention).
func fixtureStamgegevensForGraph() []map[string]any {
	return []map[string]any{
		{"id": "stam-graph-felled", "gbdBuurtId": "A09", "soortnaam": "Tilia", "geometrie": geoJSONPoint(4.91, 52.39)},
		{"id": "stam-graph-neverfelled", "gbdBuurtId": "A09", "soortnaam": "Quercus", "geometrie": geoJSONPoint(4.92, 52.40)},
	}
}

// TestBomenLoad_GraphProjection_FelledTreeAndFelling covers bomen-load/spec.md's scenario "A felled
// tree is projected as a felling event": after Load runs against the fixture above, the graph gains
// a gs:Tree for the felled row's boomId and a gs:Felling (gs:felledTree -> that tree, gs:felledOn
// the kapmaatregelDatumUitgevoerd date), written through the SHACL gate — while the never-felled
// row contributes neither.
func TestBomenLoad_GraphProjection_FelledTreeAndFelling(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	dsURL := fusekiDatasetURL(t)
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	store := shared.NewRawStore(t.TempDir())

	fetchedAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	seed(t, store, fixtureKapenherplantForGraph(), fixtureStamgegevensForGraph(), fetchedAt)

	// gs-test is a single shared dataset (no per-test isolation) — clear prior run graphs first,
	// mirroring load/koop/integration_harness_test.go's resetFusekiRunGraphs contract.
	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: true}))

	assert.True(t, sparqlAsk(t, ctx, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <http://gemetenstad.nl/id/tree/stam-graph-felled> a gs:Tree } }`),
		"the felled row's tree must be projected as a gs:Tree")

	assert.True(t, sparqlAsk(t, ctx, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
PREFIX xsd: <http://www.w3.org/2001/XMLSchema#>
ASK { GRAPH ?g { <http://gemetenstad.nl/id/felling/kap-graph-felled> a gs:Felling ;
                 gs:felledTree <http://gemetenstad.nl/id/tree/stam-graph-felled> ;
                 gs:felledOn "2023-02-01"^^xsd:date } }`),
		"the felled row must be projected as a gs:Felling referencing its tree and felling date")

	assert.False(t, sparqlAsk(t, ctx, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <http://gemetenstad.nl/id/felling/kap-graph-neverfelled> a gs:Felling } }`),
		"a never-felled row must not be projected as a gs:Felling")
	assert.False(t, sparqlAsk(t, ctx, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <http://gemetenstad.nl/id/tree/stam-graph-neverfelled> a gs:Tree } }`),
		"a never-felled row's tree must not be projected into the graph")
}

// TestBomenLoad_GraphProjection_UnchangedRerunIsNoOp covers bomen-load/spec.md's scenario
// "Unchanged re-projection is a no-op": re-running the load against an unchanged registry export
// mints no new run graph (idempotent via the load gate's SCD2 signature, design.md/load/graph's own
// upsert contract).
func TestBomenLoad_GraphProjection_UnchangedRerunIsNoOp(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	dsURL := fusekiDatasetURL(t)
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	store := shared.NewRawStore(t.TempDir())

	fetchedAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	seed(t, store, fixtureKapenherplantForGraph(), fixtureStamgegevensForGraph(), fetchedAt)

	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: true}))

	before := graphsWithPrefix(t, ctx, dsURL, gsRunGraphPrefix)
	require.NotEmpty(t, before, "sanity: the first load must have minted at least one run graph")

	// Re-land the exact same content (LandVersion is content-hash idempotent) and re-run Load with
	// Reset:false — a true no-op re-run.
	seed(t, store, fixtureKapenherplantForGraph(), fixtureStamgegevensForGraph(), fetchedAt.Add(time.Hour))
	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: false}))

	after := graphsWithPrefix(t, ctx, dsURL, gsRunGraphPrefix)
	assert.ElementsMatch(t, before, after, "an unchanged re-run must mint no new run:load-... graph")

	// The felled tree/felling must still be present (untouched, not merely "not duplicated").
	assert.True(t, sparqlAsk(t, ctx, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <http://gemetenstad.nl/id/felling/kap-graph-felled> a gs:Felling } }`),
		"the felling must remain present across the no-op re-run")
}
