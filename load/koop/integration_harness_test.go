//go:build integration

// Shared harness for the load/koop integration suite (OpenSpec change load-koop-assembly, tasks
// 3.5, 5.2, 6.3, 8.1, 8.2): an isolated internal/testdb Postgres schema seeded with a real-shaped
// BAG + gebieden subset (testdata/koop_geo_seed.sql, adapted from location/testdata/target_seed.sql
// per the task contract), plus the shared "gs-test" SHACL-enabled Fuseki dataset load/graph's own
// integration tests use (deploy/compose/fuseki/gs-test.ttl) — the only dataset in the dev stack
// provisioned with a /shacl endpoint (internal/testdb can't add one to an admin-API-created
// dataset), so koop.Load's own graph.Load call needs it too. Isolation on the Fuseki side is by
// resetting run:load-... graphs (mirrors load/graph/load_integration_test.go's dropRunGraphs), not
// a per-test dataset.
package koop

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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
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
// whose every connection has search_path set to <schema>,public. resolveBesluit's SQL
// (buurtCodeByIdentificatieSQL, buurtByPointSQL, buurtByRDPointSQL) and location.Resolve's own SQL
// all use UNQUALIFIED table names, and the PostGIS/pg_trgm functions they call
// (ST_Contains, ST_MakePoint, similarity, ...) live in public, so both must be on the path.
// pg_trgm is created here (not by koop.Load, which only ensures postgis — schema.go's
// ensureExtensions) because the fuzzy-street address tier (location/sql.go's
// addressByStreetFuzzySQL) needs it, mirroring location_integration_test.go's own harness.
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

	_, err = pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS postgis; CREATE EXTENSION IF NOT EXISTS pg_trgm;")
	require.NoError(t, err)

	return pool
}

// seedGeo loads testdata/koop_geo_seed.sql (the checked-in BAG + gebieden_buurten/_wijken subset:
// one Noord buurt N01BUURT, one non-Noord buurt A01BUURT) directly into the target tables, the
// same tables location.Resolve (called by resolveBesluit) reads.
func seedGeo(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	sql, err := os.ReadFile("testdata/koop_geo_seed.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(sql))
	require.NoError(t, err)
}

// -- Fuseki: the shared gs-test dataset --------------------------------------------------------

// fusekiTestDataset is the dedicated, in-memory, SHACL-enabled Fuseki dataset every integration
// suite in this repo that needs the /shacl endpoint targets (load/graph/load_integration_test.go's
// own testDataset) — NOT the runtime "ds", so these tests never touch working data (guarded below
// via testdb.AssertNotProduction).
const fusekiTestDataset = "gs-test"

// gsRunGraphPrefix mirrors load/graph/write.go's own (private) runGraphPrefix constant
// ("http://gemetenstad.nl/run/load-") — load/koop can only observe graph.Load's run graphs via
// SPARQL, not by importing load/graph's unexported internals, so the prefix is duplicated here
// deliberately (same string, same purpose: list/count a run's own named graphs).
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
// reference model, via a direct graph.Load(Config{Reset:true}) call — the exported primitive that
// does exactly what load/graph/load_integration_test.go's own dropRunGraphs helper does by hand.
// Call this at the start of every test that asserts a specific run-graph outcome, since gs-test is
// a single shared dataset (no per-test isolation is possible — see the package doc comment above).
func resetFusekiRunGraphs(t *testing.T, ctx context.Context, dsURL string) {
	t.Helper()
	require.NoError(t, graph.Load(ctx, dsURL, nil, graph.Config{Reset: true}))
}

// doSPARQLQuery runs a SPARQL query (ASK or SELECT) against dsURL's /sparql endpoint, authenticated
// as the Fuseki admin user, and returns the raw JSON response body.
func doSPARQLQuery(t *testing.T, dsURL, query string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, dsURL+"/sparql?query="+url.QueryEscape(query), nil)
	require.NoError(t, err)
	req.SetBasicAuth("admin", requireEnv(t, "FUSEKI_ADMIN_PASSWORD"))
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
func sparqlAsk(t *testing.T, dsURL, query string) bool {
	t.Helper()
	body := doSPARQLQuery(t, dsURL, query)
	var res struct {
		Boolean bool `json:"boolean"`
	}
	require.NoError(t, json.Unmarshal(body, &res))
	return res.Boolean
}

// sparqlGraphsWithPrefix returns every named graph in dsURL whose IRI starts with prefix — used to
// assert a no-op re-run mints no new run:load-... graph (task 8.1), mirroring
// load/graph/load_integration_test.go's own graphsWithPrefix-based assertions.
func sparqlGraphsWithPrefix(t *testing.T, dsURL, prefix string) []string {
	t.Helper()
	body := doSPARQLQuery(t, dsURL, `SELECT DISTINCT ?g WHERE { GRAPH ?g { ?s ?p ?o } }`)
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

// -- Corpus: fixture ids and landing ------------------------------------------------------------

// The fixture corpus (testdata/it_corpus_*.xml + .metadata.xml) spans five zaken, mirroring the
// task contract's required scenarios:
//
//   - gmbNoordAanvraag + gmbNoordBesluit (zaaknummerNoord, "Z2023-N010001"): an aanvraag+besluit
//     pair whose title address (Örehof 8 1024BB) resolves via the address tier (0.90) into
//     N01BUURT (Noord) — the "title address -> Noord buurt at 0.90" scenario, and task 6.3's
//     "a zaak's aanvraag+besluit are two rows".
//   - gmbPendingAanvraag (zaaknummerPending, "Z2023-N020000"): an aanvraag with no besluit yet —
//     persisted (pending), never graphed.
//   - gmbOutsideNoordBesluit (zaaknummerOutsideNoord, "Z2023-A030000"): a besluit whose title
//     address (Zuidstraat 3 1077ZZ) resolves via the address tier (0.90) into A01BUURT (NOT
//     Noord) — excluded entirely per the scoping policy (no rows, no graph).
//   - gmbUnresolvableBesluit (zaaknummerUnresolvable, "Z2023-X040000"): a besluit with no
//     extractable address and a point far outside every seeded buurt — the unresolvable bucket.
//   - gmbPointFloorBesluit (zaaknummerPointFloor, "Z2023-N050000"): a besluit with no extractable
//     address, only a point inside N01BUURT at no seeded address — the "point-only -> buurt floor
//     0.50 + unresolvedLocation" scenario.
const (
	gmbNoordAanvraag       = "gmb-2023-810001"
	gmbNoordBesluit        = "gmb-2023-810002"
	gmbPendingAanvraag     = "gmb-2023-820001"
	gmbOutsideNoordBesluit = "gmb-2023-830001"
	gmbUnresolvableBesluit = "gmb-2023-840001"
	gmbPointFloorBesluit   = "gmb-2023-850001"

	zaaknummerNoord        = "Z2023-N010001"
	zaaknummerPending      = "Z2023-N020000"
	zaaknummerOutsideNoord = "Z2023-A030000"
	zaaknummerUnresolvable = "Z2023-X040000"
	zaaknummerPointFloor   = "Z2023-N050000"

	// noordPlaceIdentificatie is koop_geo_seed.sql's Noord buurt identificatie — the Place IRI
	// every Noord-resolved besluit in the corpus (address-tier AND point-floor) must locate at.
	noordPlaceIdentificatie = "N01BUURT"
)

// fixedFetchedAt is the fetchedAt timestamp every corpus fixture is landed with — its exact value
// is irrelevant to any assertion (only Provenance.FetchedAt records it), so one fixed value keeps
// the corpus landing deterministic.
var fixedFetchedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// landFixture reads testdata/<fixtureBase>.xml + testdata/<fixtureBase>.metadata.xml and lands them
// into store under koop/<gmbID>.xml + koop/<gmbID>.metadata.xml — the exact layout
// readPublications (read.go) walks. The on-disk testdata filenames are descriptive
// (it_corpus_noord_besluit.xml, ...); the landed name is the gmb id readPublications actually
// requires (gmbIDPattern), so the two are kept as separate parameters rather than forcing the
// testdata filenames themselves into the gmb-id shape.
func landFixture(t *testing.T, store *shared.RawStore, fixtureBase, gmbID string) {
	t.Helper()

	record, err := os.ReadFile("testdata/" + fixtureBase + ".xml")
	require.NoError(t, err)
	_, err = store.Land("koop/"+gmbID+".xml", strings.NewReader(string(record)), "https://example.com/"+fixtureBase, fixedFetchedAt)
	require.NoError(t, err)

	metadata, err := os.ReadFile("testdata/" + fixtureBase + ".metadata.xml")
	require.NoError(t, err)
	_, err = store.Land("koop/"+gmbID+".metadata.xml", strings.NewReader(string(metadata)), "https://example.com/"+fixtureBase+"/metadata", fixedFetchedAt)
	require.NoError(t, err)
}

// landNoordZaak lands only the Noord aanvraag+besluit pair (gmbNoordAanvraag, gmbNoordBesluit) —
// the minimal corpus the focused graph-gate tests (5.2) need, without the rest of the fixture
// corpus's unrelated zaken.
func landNoordZaak(t *testing.T, store *shared.RawStore) {
	t.Helper()
	landFixture(t, store, "it_corpus_noord_aanvraag", gmbNoordAanvraag)
	landFixture(t, store, "it_corpus_noord_besluit", gmbNoordBesluit)
}

// landFullCorpus lands every fixture in the corpus — the full scoping matrix (Noord pair, pending,
// out-of-Noord, unresolvable, point-floor) tasks 6.3 and 8.2 exercise end to end.
func landFullCorpus(t *testing.T, store *shared.RawStore) {
	t.Helper()
	landNoordZaak(t, store)
	landFixture(t, store, "it_corpus_pending_aanvraag", gmbPendingAanvraag)
	landFixture(t, store, "it_corpus_outside_noord_besluit", gmbOutsideNoordBesluit)
	landFixture(t, store, "it_corpus_unresolvable_besluit", gmbUnresolvableBesluit)
	landFixture(t, store, "it_corpus_point_floor_besluit", gmbPointFloorBesluit)
}

// -- koop_publications row assertions -----------------------------------------------------------

// pubRow is one queried-back koop_publications row's columns relevant to the assertions below.
type pubRow struct {
	GmbID                 string
	Zaaknummer            *string
	Kind                  *string
	Available             *time.Time
	GeomWKT               *string
	Postcode              *string
	Huisnummer            *int
	ResolvedIdentificatie *string
	ResolvedBuurtCode     *string
	ResolvedConfidence    *float64
	// ResolvedGeomWKT is the resolver's precise address-tier BAG point (ST_AsText(resolved_geom));
	// NULL at the postcode/buurt tier and for unresolvable/keyless rows (spec's "precise resolved
	// point is kept as silver" scenario).
	ResolvedGeomWKT *string
	// ResolvedTier is resolved_tier ("address"/"postcode"/"buurt"); NULL when unresolvable/keyless.
	ResolvedTier *string
	Caveats      []string
	InNoord      *bool
	Unresolved   bool
	Raw          []byte
	// LoadedAt is koop_publications.loaded_at (load-koop-assembly's loaded_at fix): stamped with
	// the load run's timestamp on every insert and every genuine update, but excluded from
	// upsertPublications' MERGE change-detection — an unchanged re-run must leave it untouched,
	// while a genuine change (e.g. a re-resolution) must bump it. Never nil once a row exists
	// (NOT NULL), but kept as *time.Time here so the harness's zero-value pubRow{} (the
	// not-found case) stays visibly distinct from a real, populated row.
	LoadedAt *time.Time
}

// queryPublication reads back koop_publications' row for gmbID. found is false (with a zero
// pubRow) when no such row exists — used to assert an excluded zaak's publication left no row.
func queryPublication(t *testing.T, ctx context.Context, pool *pgxpool.Pool, gmbID string) (row pubRow, found bool) {
	t.Helper()
	const q = `SELECT gmb_id, zaaknummer, kind, available, ST_AsText(geom), postcode, huisnummer,
		resolved_identificatie, resolved_buurt_code, resolved_confidence, ST_AsText(resolved_geom), resolved_tier,
		caveats, in_noord, unresolved, raw, loaded_at
		FROM koop_publications WHERE gmb_id = $1`
	err := pool.QueryRow(ctx, q, gmbID).Scan(
		&row.GmbID, &row.Zaaknummer, &row.Kind, &row.Available, &row.GeomWKT, &row.Postcode, &row.Huisnummer,
		&row.ResolvedIdentificatie, &row.ResolvedBuurtCode, &row.ResolvedConfidence, &row.ResolvedGeomWKT, &row.ResolvedTier,
		&row.Caveats, &row.InNoord, &row.Unresolved, &row.Raw, &row.LoadedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return pubRow{}, false
		}
		require.NoError(t, err, "query koop_publications row %s", gmbID)
	}
	return row, true
}

// countPublications counts every row in koop_publications (used for the idempotency / total-count
// assertions).
func countPublications(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM koop_publications").Scan(&n))
	return n
}
