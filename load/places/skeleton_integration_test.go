//go:build integration

package places

// Integration tests for seed-place-skeleton (P12b) tasks 5.1-5.4: the real load/places ->
// loadgraph.Load seam, exercised against the isolated gs-test Fuseki dataset AND an isolated
// internal/testdb Postgres schema (P5 harness). Mirrors two existing templates:
//
//   - load/graph/load_integration_test.go for the Fuseki side (dataset constant, ASK/count
//     helpers, run-graph cleanup). load/graph's own client and its run-graph constants are
//     unexported, so this file re-declares a small SPARQL client and the run:* namespace shapes
//     it needs (gsRunNS/referenceModelGraph/provenanceGraph/runGraphPrefix mirror
//     load/graph/write.go's unexported constants of the same names).
//   - load/geo/load_integration_test.go for the Postgres side (newSchemaPool: a fresh
//     internal/testdb schema per test, dropped in cleanup, with search_path pinned so
//     BuildCandidate's unqualified... actually schema-qualified reads land in the right place).
//
// gs-test's in-memory store is shared and tests run serially (see load/graph's comment); each
// test here drops every run:*, run:_model, and run:_provenance graph it touches, both before
// (clean slate) and after (t.Cleanup) itself.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/internal/testdb"
	loadgraph "github.com/Blogem/gemeten-stad/load/graph"
)

// testDataset is the dedicated, SHACL-enabled Fuseki dataset integration tests target — never the
// runtime `ds` (enforced by testdb.AssertNotProduction).
const testDataset = "gs-test"

// gsRunNS/referenceModelGraph/provenanceGraph/runGraphPrefix mirror load/graph/write.go's
// unexported constants of the same names: the run: namespace load/graph.Load writes its
// run-stamped named graphs, reference model, and provenance into. They cannot be imported (they
// are unexported in package graph), so this test re-declares the same fixed strings.
const (
	gsRunNS             = "http://gemetenstad.nl/run/"
	referenceModelGraph = gsRunNS + "_model"
	provenanceGraph     = gsRunNS + "_provenance"
	runGraphPrefix      = gsRunNS + "load-"
)

// fusekiAdminUser is the fixed admin username Fuseki's data/admin endpoints expect (mirrors
// load/graph/client.go's own constant of the same name).
const fusekiAdminUser = "admin"

// sparqlClient is a minimal HTTP client for the gs-test Fuseki dataset: SPARQL query (ASK/SELECT)
// and SPARQL Update (DROP GRAPH), enough for this file's assertions and cleanup. load/graph's own
// client type is unexported and cannot be reused across packages, so this mirrors its shape
// (load/graph/client.go) rather than importing it.
type sparqlClient struct {
	baseURL    string
	password   string
	httpClient *http.Client
}

func (c *sparqlClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("places integration test: build %s %s request: %w", method, path, err)
	}
	req.SetBasicAuth(fusekiAdminUser, c.password)
	return req, nil
}

func (c *sparqlClient) do(req *http.Request, action string) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("places integration test: %s: %w", action, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("places integration test: %s: read response body: %w", action, err)
	}
	if resp.StatusCode/100 != 2 {
		snippet := body
		if len(snippet) > 4096 {
			snippet = snippet[:4096]
		}
		return nil, fmt.Errorf("places integration test: %s: status %d: %s", action, resp.StatusCode, snippet)
	}
	return body, nil
}

// ask runs a SPARQL ASK against the whole dataset and returns the boolean result.
func (c *sparqlClient) ask(t *testing.T, query string) bool {
	t.Helper()
	req, err := c.newRequest(context.Background(), http.MethodGet, "/sparql?query="+url.QueryEscape(query), nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/sparql-results+json")
	body, err := c.do(req, "ASK")
	require.NoError(t, err)
	var res struct {
		Boolean bool `json:"boolean"`
	}
	require.NoError(t, json.Unmarshal(body, &res))
	return res.Boolean
}

// count runs a SPARQL SELECT whose sole projection is a single `(... AS ?n)` binding and returns
// it as an int.
func (c *sparqlClient) count(t *testing.T, query string) int {
	t.Helper()
	req, err := c.newRequest(context.Background(), http.MethodGet, "/sparql?query="+url.QueryEscape(query), nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/sparql-results+json")
	body, err := c.do(req, "COUNT")
	require.NoError(t, err)
	var res struct {
		Results struct {
			Bindings []struct {
				N struct {
					Value string `json:"value"`
				} `json:"n"`
			} `json:"bindings"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(body, &res))
	require.Len(t, res.Results.Bindings, 1, "count query must return exactly one row")
	n, err := strconv.Atoi(res.Results.Bindings[0].N.Value)
	require.NoError(t, err)
	return n
}

// update executes a SPARQL Update against the dataset's /update endpoint.
func (c *sparqlClient) update(ctx context.Context, sparqlUpdate, action string) error {
	req, err := c.newRequest(ctx, http.MethodPost, "/update", strings.NewReader("update="+url.QueryEscape(sparqlUpdate)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err = c.do(req, action)
	return err
}

// dropGraph removes graphIRI entirely. DROP SILENT is a no-op (not an error) if the graph does
// not exist, so cleanup never fails on an already-absent graph.
func (c *sparqlClient) dropGraph(ctx context.Context, graphIRI string) error {
	return c.update(ctx, "DROP SILENT GRAPH <"+graphIRI+">", "DROP GRAPH "+graphIRI)
}

// graphsWithPrefix returns every named graph currently present in the dataset whose IRI starts
// with prefix.
func (c *sparqlClient) graphsWithPrefix(t *testing.T, prefix string) []string {
	t.Helper()
	const query = `SELECT DISTINCT ?g WHERE { GRAPH ?g { ?s ?p ?o } }`
	req, err := c.newRequest(context.Background(), http.MethodGet, "/sparql?query="+url.QueryEscape(query), nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/sparql-results+json")
	body, err := c.do(req, "list named graphs")
	require.NoError(t, err)

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

// countOpenActive returns how many currently-open (no gs:validTo) gs:active annotated versions
// exist for placeIRI, across every named graph — the Place analogue of load/graph's
// countOpenLocatedAt, used to assert the single-open-version invariant after a supersede.
func (c *sparqlClient) countOpenActive(t *testing.T, placeIRI string) int {
	t.Helper()
	query := `PREFIX gs: <` + gsNS + `>
SELECT (COUNT(?a) AS ?n) WHERE {
  GRAPH ?g {
    <` + placeIRI + `> gs:active ?a .
    << <` + placeIRI + `> gs:active ?a >> gs:validFrom ?vf .
    FILTER NOT EXISTS { << <` + placeIRI + `> gs:active ?a >> gs:validTo ?vt }
  }
}`
	return c.count(t, query)
}

// countProvActivities returns how many prov:Activity individuals exist in run:_provenance.
func (c *sparqlClient) countProvActivities(t *testing.T) int {
	t.Helper()
	return c.count(t, `PREFIX prov: <http://www.w3.org/ns/prov#>
SELECT (COUNT(?a) AS ?n) WHERE { GRAPH <`+provenanceGraph+`> { ?a a prov:Activity } }`)
}

// countRunTriples returns the total triple count across every run:load-... named graph — used to
// assert a true no-op re-seed adds literally nothing to the store.
func (c *sparqlClient) countRunTriples(t *testing.T) int {
	t.Helper()
	return c.count(t, `SELECT (COUNT(*) AS ?n) WHERE {
  GRAPH ?g { ?s ?p ?o }
  FILTER(STRSTARTS(STR(?g), "`+runGraphPrefix+`"))
}`)
}

// dropAllRunGraphs drops run:_model, run:_provenance, and every run:load-... graph — the full
// clean-slate this suite's tests rely on, since gs-test's in-memory store is shared across tests.
func dropAllRunGraphs(t *testing.T, c *sparqlClient) {
	t.Helper()
	ctx := context.Background()
	for _, g := range []string{referenceModelGraph, provenanceGraph} {
		assert.NoError(t, c.dropGraph(ctx, g))
	}
	for _, g := range c.graphsWithPrefix(t, runGraphPrefix) {
		assert.NoError(t, c.dropGraph(ctx, g))
	}
}

// fusekiTestClient returns a client bound to the gs-test dataset, guarded against ever targeting a
// production dataset name, with a clean-slate drop of every run graph both before the test and on
// cleanup. Skips (does not fail) if GS_TEST_FUSEKI_URL/FUSEKI_ADMIN_PASSWORD are unset, per this
// change's task contract — the orchestrator's pass-2 wave runs these once services are up.
func fusekiTestClient(t *testing.T) (*sparqlClient, string) {
	t.Helper()
	root := os.Getenv("GS_TEST_FUSEKI_URL")
	if root == "" {
		t.Skip("GS_TEST_FUSEKI_URL not set; skipping load/places skeleton integration test")
	}
	password := os.Getenv("FUSEKI_ADMIN_PASSWORD")
	if password == "" {
		t.Skip("FUSEKI_ADMIN_PASSWORD not set; skipping load/places skeleton integration test")
	}
	require.NoError(t, testdb.AssertNotProduction("", testDataset), "must not target a production dataset")

	dsURL := strings.TrimRight(root, "/") + "/" + testDataset
	c := &sparqlClient{baseURL: dsURL, password: password, httpClient: http.DefaultClient}

	dropAllRunGraphs(t, c)
	t.Cleanup(func() { dropAllRunGraphs(t, c) })
	return c, dsURL
}

// pgTestDSN returns GS_TEST_DATABASE_URL, or skips the test if unset (mirrors fusekiTestClient's
// skip-not-fail gating).
func pgTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("GS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GS_TEST_DATABASE_URL not set; skipping load/places skeleton integration test")
	}
	return dsn
}

// newSchemaPool creates a fresh internal/testdb schema (dropped on cleanup) and returns a pool
// whose every connection has search_path set to <schema>,public. Mirrors load/geo's own helper of
// the same name (unexported there too, so this package needs its own copy).
func newSchemaPool(t *testing.T, ctx context.Context, dsn string) (*pgxpool.Pool, string) {
	t.Helper()

	schema, err := testdb.NewSchemaName()
	require.NoError(t, err)
	require.NoError(t, testdb.CreateSchema(ctx, dsn, schema))
	t.Cleanup(func() {
		assert.NoError(t, testdb.DropSchema(ctx, dsn, schema), "DropSchema cleanup")
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path = "+pgx.Identifier{schema}.Sanitize()+", public")
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, pool.Ping(ctx))
	require.NoError(t, testdb.AssertIsolatedSchema(ctx, pool, schema),
		"harness guard: pool's search_path must resolve current_schema() to the isolated test schema")
	return pool, schema
}

// seedGebiedenFixture creates minimal gebieden_wijken/gebieden_buurten tables in the isolated
// schema — only the columns BuildCandidate's reads select (load/places/read.go) — and inserts one
// wijk with two buurten within it.
func seedGebiedenFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema string) {
	t.Helper()

	wijkTable := pgx.Identifier{schema, "gebieden_wijken"}.Sanitize()
	buurtTable := pgx.Identifier{schema, "gebieden_buurten"}.Sanitize()

	_, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (
		identificatie text PRIMARY KEY, naam text NOT NULL, source_deleted_at timestamptz)`, wijkTable))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (
		identificatie text PRIMARY KEY, naam text NOT NULL, ligtinwijkid text, source_deleted_at timestamptz)`, buurtTable))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, fmt.Sprintf("INSERT INTO %s (identificatie, naam) VALUES ('wijk-1', 'Testwijk')", wijkTable))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, fmt.Sprintf(
		"INSERT INTO %s (identificatie, naam, ligtinwijkid) VALUES ('buurt-1', 'Testbuurt Een', 'wijk-1'), ('buurt-2', 'Testbuurt Twee', 'wijk-1')",
		buurtTable))
	require.NoError(t, err)
}

// setBuurtDeleted soft-deletes a single buurt row (sets source_deleted_at), for the deprecation
// supersede scenario (5.3).
func setBuurtDeleted(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema, identificatie string, deletedAt time.Time) {
	t.Helper()
	buurtTable := pgx.Identifier{schema, "gebieden_buurten"}.Sanitize()
	_, err := pool.Exec(ctx, fmt.Sprintf("UPDATE %s SET source_deleted_at = $1 WHERE identificatie = $2", buurtTable),
		deletedAt, identificatie)
	require.NoError(t, err)
}

// 5.1: seeding the fixture skeleton into empty run graphs writes the buurt and wijk Places (with
// identity, label, gs:within containment, and the gs:active/gs:validFrom annotation) into a
// run:load-... named graph, alongside a matching prov:Activity in run:_provenance (spec.md "Seed
// the skeleton through the SHACL-gated graph load with SCD2 active-status", scenario "First
// seeding writes the skeleton with provenance").
func TestSeedSkeletonWritesProvenance(t *testing.T) {
	dsn := pgTestDSN(t)
	ctx := context.Background()
	pool, schema := newSchemaPool(t, ctx, dsn)
	seedGebiedenFixture(t, ctx, pool, schema)

	candidate, err := BuildCandidate(ctx, pool, schema)
	require.NoError(t, err)
	require.NotEmpty(t, candidate)

	c, base := fusekiTestClient(t)
	require.NoError(t, loadgraph.Load(ctx, base, nil, loadgraph.Config{Reset: true}))

	before := c.graphsWithPrefix(t, runGraphPrefix)
	require.NoError(t, loadgraph.Load(ctx, base, candidate, loadgraph.Config{}))
	after := c.graphsWithPrefix(t, runGraphPrefix)
	assert.Len(t, after, len(before)+1, "one new run graph on the skeleton's first write")

	wijkIRI := mintPlaceIRI("wijk-1")
	buurt1IRI := mintPlaceIRI("buurt-1")
	buurt2IRI := mintPlaceIRI("buurt-2")

	assert.True(t, c.ask(t, `PREFIX gs: <`+gsNS+`>
ASK { GRAPH ?g { <`+wijkIRI+`> a gs:Place } }`), "wijk written as a gs:Place")
	assert.True(t, c.ask(t, `PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
ASK { GRAPH ?g { <`+wijkIRI+`> rdfs:label "Testwijk" } }`), "wijk label matches naam")

	assert.True(t, c.ask(t, `PREFIX gs: <`+gsNS+`>
ASK { GRAPH ?g { <`+buurt1IRI+`> a gs:Place } }`), "buurt-1 written as a gs:Place")
	assert.True(t, c.ask(t, `PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
ASK { GRAPH ?g { <`+buurt1IRI+`> rdfs:label "Testbuurt Een" } }`), "buurt-1 label matches naam")

	assert.True(t, c.ask(t, `PREFIX gs: <`+gsNS+`>
ASK { GRAPH ?g { <`+buurt1IRI+`> gs:within <`+wijkIRI+`> } }`), "buurt-1 within wijk edge present")
	assert.True(t, c.ask(t, `PREFIX gs: <`+gsNS+`>
ASK { GRAPH ?g { <`+buurt2IRI+`> gs:within <`+wijkIRI+`> } }`), "buurt-2 within wijk edge present")

	assert.True(t, c.ask(t, `PREFIX gs: <`+gsNS+`>
ASK { GRAPH ?g { <`+buurt1IRI+`> gs:active true . << <`+buurt1IRI+`> gs:active true >> gs:validFrom ?vf } }`),
		"buurt-1's gs:active true carries a gs:validFrom annotation")

	assert.True(t, c.ask(t, `PREFIX prov: <http://www.w3.org/ns/prov#>
ASK { GRAPH <`+provenanceGraph+`> { ?run a prov:Activity ; prov:generatedAtTime ?t } }`),
		"run provenance recorded")
}

// 5.2: re-seeding the SAME skeleton a second time with no Reset (only the emitted gs:validFrom
// differs, since BuildCandidate stamps time.Now()) must be a true no-op end to end — no second
// run:load-... graph, no new prov:Activity, no triples added anywhere under run:load-... (spec.md
// scenario "Re-seeding the unchanged skeleton is a no-op"; rests on P12's valid-time-excluded
// change detection).
func TestSeedSkeletonNoOpReseed(t *testing.T) {
	dsn := pgTestDSN(t)
	ctx := context.Background()
	pool, schema := newSchemaPool(t, ctx, dsn)
	seedGebiedenFixture(t, ctx, pool, schema)

	c, base := fusekiTestClient(t)
	require.NoError(t, loadgraph.Load(ctx, base, nil, loadgraph.Config{Reset: true}))

	candidate1, err := BuildCandidate(ctx, pool, schema)
	require.NoError(t, err)
	require.NoError(t, loadgraph.Load(ctx, base, candidate1, loadgraph.Config{}))

	beforeGraphs := c.graphsWithPrefix(t, runGraphPrefix)
	beforeProv := c.countProvActivities(t)
	beforeTriples := c.countRunTriples(t)

	// Rebuild the candidate from the UNCHANGED fixture: the read + render happen again, so only
	// the emitted gs:validFrom timestamp can differ from candidate1.
	candidate2, err := BuildCandidate(ctx, pool, schema)
	require.NoError(t, err)
	require.NoError(t, loadgraph.Load(ctx, base, candidate2, loadgraph.Config{}))

	afterGraphs := c.graphsWithPrefix(t, runGraphPrefix)
	assert.ElementsMatch(t, beforeGraphs, afterGraphs, "no second run:load-... graph on a true no-op re-seed")
	assert.Equal(t, beforeProv, c.countProvActivities(t), "no new prov:Activity on a true no-op re-seed")
	assert.Equal(t, beforeTriples, c.countRunTriples(t), "no triples added on a true no-op re-seed")
}

// 5.3: after an initial seed, soft-deleting one buurt (setting source_deleted_at) and re-seeding
// (no Reset) must open a new gs:active false version carrying its gs:validFrom, close the prior
// gs:active true version by stamping its gs:validTo equal to the new version's gs:validFrom,
// retain the prior interval, and leave exactly one open version for that Place (spec.md scenario
// "An area going inactive opens a new version and closes the prior").
func TestSeedSkeletonDeprecationSupersede(t *testing.T) {
	dsn := pgTestDSN(t)
	ctx := context.Background()
	pool, schema := newSchemaPool(t, ctx, dsn)
	seedGebiedenFixture(t, ctx, pool, schema)

	c, base := fusekiTestClient(t)
	require.NoError(t, loadgraph.Load(ctx, base, nil, loadgraph.Config{Reset: true}))

	candidate1, err := BuildCandidate(ctx, pool, schema)
	require.NoError(t, err)
	require.NoError(t, loadgraph.Load(ctx, base, candidate1, loadgraph.Config{}))

	buurt1IRI := mintPlaceIRI("buurt-1")
	require.Equal(t, 1, c.countOpenActive(t, buurt1IRI), "exactly one open version after the first seed")

	deletedAt := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	setBuurtDeleted(t, ctx, pool, schema, "buurt-1", deletedAt)

	candidate2, err := BuildCandidate(ctx, pool, schema)
	require.NoError(t, err)
	require.NoError(t, loadgraph.Load(ctx, base, candidate2, loadgraph.Config{}))

	deletedAtLiteral := deletedAt.Format("2006-01-02")

	assert.True(t, c.ask(t, `PREFIX gs: <`+gsNS+`>
PREFIX xsd: <http://www.w3.org/2001/XMLSchema#>
ASK { GRAPH ?g { <`+buurt1IRI+`> gs:active false .
                 << <`+buurt1IRI+`> gs:active false >> gs:validFrom "`+deletedAtLiteral+`"^^xsd:date } }`),
		"new gs:active false version carries the soft-delete's gs:validFrom")

	assert.True(t, c.ask(t, `PREFIX gs: <`+gsNS+`>
PREFIX xsd: <http://www.w3.org/2001/XMLSchema#>
ASK { GRAPH ?g { << <`+buurt1IRI+`> gs:active true >> gs:validTo "`+deletedAtLiteral+`"^^xsd:date } }`),
		"prior gs:active true version is closed with gs:validTo equal to the new version's gs:validFrom")

	// history retained: the prior version's own triple is not deleted.
	assert.True(t, c.ask(t, `PREFIX gs: <`+gsNS+`>
ASK { GRAPH ?g { <`+buurt1IRI+`> gs:active true } }`),
		"prior gs:active true version's triple remains in the store")

	assert.Equal(t, 1, c.countOpenActive(t, buurt1IRI), "exactly one open version after the supersede")
}

// 5.4: re-seeding with Config{Reset: true} from the current fixture clears prior run graphs and
// rebuilds the skeleton from scratch — exactly one fresh run:load-... graph and prov:Activity
// afterward, with the Places present again (spec.md scenario "Reset rebuilds the skeleton").
func TestSeedSkeletonReset(t *testing.T) {
	dsn := pgTestDSN(t)
	ctx := context.Background()
	pool, schema := newSchemaPool(t, ctx, dsn)
	seedGebiedenFixture(t, ctx, pool, schema)

	c, base := fusekiTestClient(t)
	require.NoError(t, loadgraph.Load(ctx, base, nil, loadgraph.Config{Reset: true}))

	candidate1, err := BuildCandidate(ctx, pool, schema)
	require.NoError(t, err)
	require.NoError(t, loadgraph.Load(ctx, base, candidate1, loadgraph.Config{}))
	require.Len(t, c.graphsWithPrefix(t, runGraphPrefix), 1, "one run graph after the first seed")

	// Reset from the CURRENT fixture (unchanged here, but Reset must rebuild regardless of
	// whether anything actually changed).
	candidate2, err := BuildCandidate(ctx, pool, schema)
	require.NoError(t, err)
	require.NoError(t, loadgraph.Load(ctx, base, candidate2, loadgraph.Config{Reset: true}))

	after := c.graphsWithPrefix(t, runGraphPrefix)
	assert.Len(t, after, 1, "prior run graphs are cleared and the skeleton is rebuilt into exactly one fresh run graph")
	assert.Equal(t, 1, c.countProvActivities(t), "reset leaves exactly one fresh prov:Activity")

	wijkIRI := mintPlaceIRI("wijk-1")
	buurt1IRI := mintPlaceIRI("buurt-1")
	assert.True(t, c.ask(t, `PREFIX gs: <`+gsNS+`>
ASK { GRAPH ?g { <`+wijkIRI+`> a gs:Place } }`), "wijk Place present after reset")
	assert.True(t, c.ask(t, `PREFIX gs: <`+gsNS+`>
ASK { GRAPH ?g { <`+buurt1IRI+`> a gs:Place } }`), "buurt Place present after reset")
}
