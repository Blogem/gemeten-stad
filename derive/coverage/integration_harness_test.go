//go:build integration

// Shared harness for the derive/coverage end-to-end integration suite (OpenSpec change
// derive-coverage-audit, task 5.4, plus the DB-backed halves of tasks 2.4 and 4.6): an isolated
// internal/testdb Postgres schema holding hand-created koop_publications/kapenherplant tables
// (this package never runs load/koop's or load/bomen's own migrations — it only needs the exact
// columns coverage.GenerateCandidates/UpsertAuditMetrics read/write), plus the shared "gs-test"
// SHACL-enabled Fuseki dataset load/graph's own integration tests use. Mirrors
// load/koop/integration_harness_test.go's newSchemaPool/fusekiDatasetURL/resetFusekiRunGraphs
// pattern; written white-box (package coverage) so it can reuse the unexported selectBindings
// SPARQL helper (sparql.go) and mintPeriodIRI/ContentKey/Outcome (contentkey.go, types.go) directly.
package coverage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/internal/testdb"
	"github.com/Blogem/gemeten-stad/load/graph"
)

// requireEnv fails the test loudly (never skips) if name is unset, matching the P5 convention in
// internal/testdb/postgres_test.go and every other integration suite in this repo.
func requireEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	require.NotEmptyf(t, v, "%s must be set to run integration tests (see README.md)", name)
	return v
}

// newSchemaPool creates a fresh internal/testdb schema (dropped on cleanup) and returns a pool
// whose every connection has search_path set to <schema>,public — exactly what coverage.Run's own
// `SELECT current_schema()` resolves, and what an unqualified `CREATE TABLE koop_publications`
// below lands in. Mirrors load/koop/integration_harness_test.go's newSchemaPool.
func newSchemaPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
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

	_, err = pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS postgis;")
	require.NoError(t, err)

	return pool
}

// createCoverageTables hand-creates the two tables coverage.GenerateCandidates/EnsureAuditMetrics
// read/write, with exactly the columns the derive SQL queries (candidates.go's publicationQuery /
// candidateFellingsQuery): this package never runs load/koop's or load/bomen's own schema
// migrations, so the harness owns these definitions directly (task contract's Setup step 1).
func createCoverageTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	const stmt = `
CREATE TABLE koop_publications (
    zaaknummer          text PRIMARY KEY,
    available           date,
    resolved_buurt_code text,
    resolved_geom       geometry(Point,28992),
    resolved_tier       text,
    unresolved          boolean
);

CREATE TABLE kapenherplant (
    id                            text PRIMARY KEY,
    "boomId"                      text,
    "gbdBuurtId"                  text,
    "kapmaatregelDatumUitgevoerd" timestamptz,
    "resolvedGeom"                geometry(Point,4326),
    source_deleted_at             timestamptz
);
`
	_, err := pool.Exec(ctx, stmt)
	require.NoError(t, err, "create coverage source tables")
}

// -- Fuseki: the shared gs-test dataset --------------------------------------------------------

// fusekiTestDataset is the dedicated, in-memory, SHACL-enabled Fuseki dataset every integration
// suite in this repo that needs the /shacl endpoint targets (load/graph/load_integration_test.go's
// own testDataset) — NOT the runtime "ds", so this suite never touches working data (guarded via
// testdb.AssertNotProduction).
const fusekiTestDataset = "gs-test"

// gsRunGraphPrefix mirrors load/graph/write.go's own (private) runGraphPrefix constant
// ("http://gemetenstad.nl/run/load-") — this package can only observe graph.Load's run graphs via
// SPARQL, not by importing load/graph's unexported internals, so the prefix is duplicated here
// deliberately (same string, same purpose), exactly as load/koop/integration_harness_test.go does.
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

// resetFusekiRunGraphs clears every prior run:load-... graph plus run:_provenance and reloads the
// reference model, via a direct graph.Load(Config{Reset:true}) call — the same primitive
// load/koop/integration_harness_test.go's resetFusekiRunGraphs uses. Call this once at the start of
// the suite (gs-test is a single shared dataset, no per-test isolation is possible) and again in
// t.Cleanup so a failed run doesn't poison later integration suites sharing gs-test.
func resetFusekiRunGraphs(t *testing.T, ctx context.Context, dsURL string) {
	t.Helper()
	require.NoError(t, graph.Load(ctx, dsURL, nil, graph.Config{Reset: true}))
}

// graphsWithPrefix returns every named graph in dsURL whose IRI starts with prefix — used to assert
// an unchanged re-run mints no new run:load-... graph, mirroring
// load/koop/integration_harness_test.go's sparqlGraphsWithPrefix, but built on this package's own
// selectBindings (sparql.go) rather than a hand-rolled HTTP call.
func graphsWithPrefix(t *testing.T, ctx context.Context, dsURL, prefix string) []string {
	t.Helper()
	rows, err := selectBindings(ctx, dsURL, `SELECT DISTINCT ?g WHERE { GRAPH ?g { ?s ?p ?o } }`)
	require.NoError(t, err)

	var graphs []string
	for _, row := range rows {
		if g := row["g"]; strings.HasPrefix(g, prefix) {
			graphs = append(graphs, g)
		}
	}
	sort.Strings(graphs)
	return graphs
}

// mustSelect runs a SPARQL SELECT via selectBindings and fails the test loudly on error — a thin
// wrapper so call sites below read as a single expression rather than an err-check block each time.
func mustSelect(t *testing.T, ctx context.Context, dsURL, query string) []map[string]string {
	t.Helper()
	rows, err := selectBindings(ctx, dsURL, query)
	require.NoError(t, err, "SPARQL query failed:\n%s", query)
	return rows
}

// allPeriodIRIs returns every gs:CoveragePeriod subject IRI currently in the store (open or
// closed), sorted — used to assert an unchanged re-run mints no NEW period node anywhere (task
// contract's "Idempotent no-op" scenario): capture the set before/after and diff.
func allPeriodIRIs(t *testing.T, ctx context.Context, dsURL string) []string {
	t.Helper()
	rows := mustSelect(t, ctx, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
SELECT DISTINCT ?p WHERE { GRAPH ?g { ?p a gs:CoveragePeriod } }`)
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r["p"])
	}
	sort.Strings(out)
	return out
}

// openPeriod is one currently-open (no gs:validTo) gs:CoveragePeriod node's queried-back fields —
// the matched-branch fields (ObservationIRI/Confidence/Granularity) are "" when the period is a
// no-source period. IsNoSource is derived in Go from an OPTIONAL ?nsf binding read back INSIDE the
// same GRAPH ?g block as the rest of the row (not a projected EXISTS/BIND against the query's
// default graph): this dataset's default graph is not the union of its named graphs, so a pattern
// evaluated outside GRAPH ?g never sees data that lives only in a run:load-... graph and would
// silently and always bind false, regardless of what is actually stored.
type openPeriod struct {
	IRI            string
	ObservationIRI string
	Confidence     string
	Granularity    string
	IsNoSource     bool
	Evidence       string
}

// openPeriodsFor returns every currently-open gs:CoveragePeriod for the given gs:versionOf anchor
// IRI (the node-form analogue of load/graph/load_integration_test.go's countOpenNodePeriods, but
// returning the full row so callers can assert its branch/content, not just the count). The
// state-node-versioning writer's invariant is exactly one; tests below assert len == 1 explicitly
// rather than baking that into this helper, so a genuine invariant violation surfaces as a visible
// assertion failure with the offending rows, not a silently-truncated helper return.
func openPeriodsFor(t *testing.T, ctx context.Context, dsURL, anchorIRI string) []openPeriod {
	t.Helper()
	query := fmt.Sprintf(`PREFIX gs: <http://gemetenstad.nl/ns#>
SELECT ?p ?obs ?conf ?gran ?evidence ?nsf WHERE {
  GRAPH ?g {
    ?p gs:versionOf <%s> ; gs:evidence ?evidence .
    FILTER NOT EXISTS { ?p gs:validTo ?vt }
    OPTIONAL { ?p gs:linksObservation ?obs }
    OPTIONAL { ?p gs:confidence ?conf }
    OPTIONAL { ?p gs:granularity ?gran }
    OPTIONAL { ?p gs:noSourceFound ?nsf }
  }
}`, anchorIRI)
	rows := mustSelect(t, ctx, dsURL, query)

	out := make([]openPeriod, 0, len(rows))
	for _, r := range rows {
		out = append(out, openPeriod{
			IRI:            r["p"],
			ObservationIRI: r["obs"],
			Confidence:     r["conf"],
			Granularity:    r["gran"],
			IsNoSource:     r["nsf"] == "true",
			Evidence:       r["evidence"],
		})
	}
	return out
}

// parseConfidence parses a gs:confidence binding's lexical value as a float64, failing the test
// loudly if it is not a valid decimal — used instead of a direct string-equality assertion so the
// test does not depend on the store's exact xsd:decimal lexical-form canonicalization (e.g. "1.00"
// vs "1" vs "1.0") to express "the confidence is X".
func parseConfidence(t *testing.T, s string) float64 {
	t.Helper()
	require.NotEmpty(t, s, "expected a non-empty gs:confidence binding")
	v, err := strconv.ParseFloat(s, 64)
	require.NoError(t, err, "gs:confidence binding %q must parse as a float", s)
	return v
}

// periodCaveats returns every gs:caveat object IRI asserted on periodIRI.
func periodCaveats(t *testing.T, ctx context.Context, dsURL, periodIRI string) []string {
	t.Helper()
	query := fmt.Sprintf(`PREFIX gs: <http://gemetenstad.nl/ns#>
SELECT ?caveat WHERE { GRAPH ?g { <%s> gs:caveat ?caveat } }`, periodIRI)
	rows := mustSelect(t, ctx, dsURL, query)
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r["caveat"])
	}
	sort.Strings(out)
	return out
}

// anchorExists reports whether a gs:AuditLink anchor exists asserting gs:coversIntervention the
// given Intervention IRI — scenario "A strong match is a matched period on the permit's anchor"'s
// anchor-existence half, and shared by every per-permit assertion below.
func anchorExists(t *testing.T, ctx context.Context, dsURL, anchorIRI, interventionIRI string) bool {
	t.Helper()
	query := fmt.Sprintf(`PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <%s> a gs:AuditLink ; gs:coversIntervention <%s> } }`, anchorIRI, interventionIRI)
	return sparqlAsk(t, ctx, dsURL, query)
}

// doSPARQLGet runs a plain unauthenticated GET against dsURL's /sparql endpoint — mirroring
// sparql.go's selectBindings request-building exactly (this dataset's /sparql query endpoint does
// not require the admin credentials load/graph's write-side client.go uses; only ASK's differing
// {"boolean": ...} response shape needs a distinct decode from selectBindings' bindings shape) —
// and returns the raw response body.
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

// sparqlAsk runs a SPARQL ASK query against dsURL's /sparql endpoint (selectBindings only handles
// the SELECT results shape, so ASK needs its own tiny decode here) and returns the boolean result.
func sparqlAsk(t *testing.T, ctx context.Context, dsURL, query string) bool {
	t.Helper()
	body := doSPARQLGet(t, ctx, dsURL, query)
	var res struct {
		Boolean bool `json:"boolean"`
	}
	require.NoError(t, json.Unmarshal(body, &res))
	return res.Boolean
}

// -- audit_metrics assertions --------------------------------------------------------------------

// metricRow is one queried-back audit_metrics row's columns (design.md D8) relevant to the
// assertions below.
type metricRow struct {
	Matched              bool
	AssignedFellingIDs   []string
	AssignedFellingCount int
	CandidateCount       int
	NearestDistM         *float64
	RunID                string
}

// queryMetric reads back audit_metrics' row for zaaknummer. found is false when no such row exists.
func queryMetric(t *testing.T, ctx context.Context, pool *pgxpool.Pool, zaaknummer string) (row metricRow, found bool) {
	t.Helper()
	const q = `SELECT matched, assigned_felling_ids, assigned_felling_count, candidate_count, nearest_dist_m, run_id
FROM audit_metrics WHERE zaaknummer = $1`
	err := pool.QueryRow(ctx, q, zaaknummer).Scan(
		&row.Matched, &row.AssignedFellingIDs, &row.AssignedFellingCount, &row.CandidateCount, &row.NearestDistM, &row.RunID,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return metricRow{}, false
		}
		require.NoError(t, err, "query audit_metrics row %s", zaaknummer)
	}
	return row, true
}

// allMetrics returns every audit_metrics row keyed by zaaknummer — used to snapshot the whole table
// before/after a re-run for the idempotency assertions (task contract's "Idempotent no-op" and
// "Metrics upsert idempotency" scenarios).
func allMetrics(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]metricRow {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT zaaknummer, matched, assigned_felling_ids, assigned_felling_count, candidate_count, nearest_dist_m, run_id FROM audit_metrics`)
	require.NoError(t, err)
	defer rows.Close()

	out := make(map[string]metricRow)
	for rows.Next() {
		var z string
		var m metricRow
		require.NoError(t, rows.Scan(&z, &m.Matched, &m.AssignedFellingIDs, &m.AssignedFellingCount, &m.CandidateCount, &m.NearestDistM, &m.RunID))
		out[z] = m
	}
	require.NoError(t, rows.Err())
	return out
}
