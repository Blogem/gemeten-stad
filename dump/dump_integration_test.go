//go:build integration

package dump

// Integration tests for P9 tasks 7.4/7.5/7.6: a full export -> restore round-trip against
// internal/testdb-provisioned isolated Fuseki datasets and Postgres databases (never production
// names), asserting named graphs, RDF-star confidence annotations, PostGIS rows + geometry, and
// NER cache files all survive; a --skip-bag round-trip asserting additive restore leaves
// non-bundled BAG-stand-in tables and non-bundled named graphs untouched; and a guard check that
// the test path itself aborts if ever pointed at a reserved production name.
//
// Postgres isolation here is a whole DATABASE (internal/testdb.CreateDatabase/WithDatabase), not
// just a schema: ExportPostGIS/RestorePostGIS shell out to pg_dump/pg_restore, which operate on an
// entire database by design (no --schema knob — design D3's Non-Goals), so a schema-only harness
// would let pg_dump read (if not write) the shared database's `public` schema.

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Blogem/gemeten-stad/internal/testdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireEnv fails the test loudly (never skips) if name is unset, matching the P5 convention in
// internal/testdb/postgres_test.go.
func requireEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	require.NotEmptyf(t, v, "%s must be set to run integration tests (see README.md)", name)
	return v
}

// isolatedFuseki provisions a fresh, empty Fuseki dataset for one test (dropped on cleanup) and
// returns its dataset URL — the graph store's isolation unit, matching ExportGraph/RestoreGraph's
// own dataset-level granularity.
func isolatedFuseki(t *testing.T, ctx context.Context) string {
	t.Helper()
	baseURL := requireEnv(t, "GS_TEST_FUSEKI_URL")
	adminPassword := requireEnv(t, "FUSEKI_ADMIN_PASSWORD")

	datasetName, err := testdb.NewDatasetName()
	require.NoError(t, err)
	require.NoError(t, testdb.CreateDataset(ctx, baseURL, adminPassword, datasetName))
	t.Cleanup(func() {
		assert.NoError(t, testdb.DropDataset(ctx, baseURL, adminPassword, datasetName), "DropDataset cleanup")
	})

	return strings.TrimRight(baseURL, "/") + "/" + datasetName
}

// isolatedPostgres provisions a fresh, empty Postgres database for one test (dropped on cleanup)
// and returns its DSN — the whole-database isolation unit ExportPostGIS/RestorePostGIS need (see
// file doc comment).
func isolatedPostgres(t *testing.T, ctx context.Context) string {
	t.Helper()
	adminDSN := requireEnv(t, "GS_TEST_DATABASE_URL")

	dbName, err := testdb.NewDatabaseName()
	require.NoError(t, err)
	require.NoError(t, testdb.CreateDatabase(ctx, adminDSN, dbName))
	t.Cleanup(func() {
		assert.NoError(t, testdb.DropDatabase(ctx, adminDSN, dbName), "DropDatabase cleanup")
	})

	dsn, err := testdb.WithDatabase(adminDSN, dbName)
	require.NoError(t, err)
	return dsn
}

// postNQuads loads raw N-Quads-star body into fusekiURL's dataset — test-data seeding via the
// same dataset-level POST RestoreGraph itself uses.
func postNQuads(t *testing.T, ctx context.Context, fusekiURL, body string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fusekiURL, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/n-quads")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// sparqlSelectFirstValue runs a SPARQL SELECT against fusekiURL and returns the string value bound
// to varName in the first result row ("" if there are no rows).
func sparqlSelectFirstValue(t *testing.T, ctx context.Context, fusekiURL, query, varName string) string {
	t.Helper()
	var result struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	require.NoError(t, sparqlQuery(ctx, fusekiURL, query, &result))
	if len(result.Results.Bindings) == 0 {
		return ""
	}
	return result.Results.Bindings[0][varName].Value
}

const confidenceQuery = `PREFIX ex: <http://ex/>
SELECT ?c WHERE {
  GRAPH ?g {
    ?ann <http://www.w3.org/1999/02/22-rdf-syntax-ns#reifies> <<( ex:tree1 ex:locatedAt ex:addr1 )>> ;
         ex:confidence ?c
  }
}`

// seedSourceGraph loads a named-graph triple with an RDF-star confidence annotation plus a
// default-graph triple, so the round-trip test exercises both graph kinds.
func seedSourceGraph(t *testing.T, ctx context.Context, fusekiURL string) {
	t.Helper()
	postNQuads(t, ctx, fusekiURL, `<http://ex/muni> <http://ex/name> "Amsterdam" .
`)
	// The RDF-star annotation form ({| ... |}) is Turtle/TriG-star syntax, not N-Quads — load it
	// via TriG so Fuseki expands it, then the export path re-serializes it as N-Quads-star.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fusekiURL, strings.NewReader(`PREFIX ex: <http://ex/>
GRAPH <http://ex/run/1> {
  ex:tree1 ex:locatedAt ex:addr1 {| ex:confidence 0.9 |} .
}
`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/trig")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func connectPostgres(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, pool.Ping(ctx))
	return pool
}

func seedGeometryTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, id int) {
	t.Helper()
	ident := pgx.Identifier{table}.Sanitize()
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS postgis")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "CREATE TABLE "+ident+" (id int PRIMARY KEY, geom geometry(Point, 28992))")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "INSERT INTO "+ident+" (id, geom) VALUES ($1, ST_SetSRID(ST_MakePoint(1, 2), 28992))", id)
	require.NoError(t, err)
}

func seedNoteTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, note string) {
	t.Helper()
	ident := pgx.Identifier{table}.Sanitize()
	_, err := pool.Exec(ctx, "CREATE TABLE "+ident+" (id int PRIMARY KEY, note text)")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "INSERT INTO "+ident+" (id, note) VALUES (1, $1)", note)
	require.NoError(t, err)
}

func geomAsText(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, id int) string {
	t.Helper()
	ident := pgx.Identifier{table}.Sanitize()
	var wkt string
	err := pool.QueryRow(ctx, "SELECT ST_AsText(geom) FROM "+ident+" WHERE id = $1", id).Scan(&wkt)
	require.NoError(t, err)
	return wkt
}

func noteValue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, id int) string {
	t.Helper()
	ident := pgx.Identifier{table}.Sanitize()
	var note string
	err := pool.QueryRow(ctx, "SELECT note FROM "+ident+" WHERE id = $1", id).Scan(&note)
	require.NoError(t, err)
	return note
}

func tableExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)", table).Scan(&exists)
	require.NoError(t, err)
	return exists
}

func writeNERCacheEntry(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func readNERCacheEntry(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	return string(data)
}

// TestDumpRestore_RoundTrip covers task 7.4: export a populated graph + PostGIS + NER cache,
// restore into fresh isolated stores, and assert the round-trip reproduces all three — named
// graphs, the RDF-star confidence annotation, the default graph, PostGIS rows + geometry, and NER
// cache files.
func TestDumpRestore_RoundTrip(t *testing.T) {
	ctx := context.Background()

	srcFuseki := isolatedFuseki(t, ctx)
	srcDSN := isolatedPostgres(t, ctx)
	srcCache := filepath.Join(t.TempDir(), "ner-cache")

	seedSourceGraph(t, ctx, srcFuseki)
	srcPool := connectPostgres(t, ctx, srcDSN)
	seedGeometryTable(t, ctx, srcPool, "audit_probe", 1)
	writeNERCacheEntry(t, srcCache, "doc1__modelA", "cached extraction output")

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	srcCfg := Config{FusekiURL: srcFuseki, DatabaseURL: srcDSN, NERCachePath: srcCache}
	require.NoError(t, Export(ctx, srcCfg, bundleDir, ExportOptions{}, time.Now().UTC()))

	dstFuseki := isolatedFuseki(t, ctx)
	dstDSN := isolatedPostgres(t, ctx)
	dstCache := filepath.Join(t.TempDir(), "ner-cache")

	dstCfg := Config{FusekiURL: dstFuseki, DatabaseURL: dstDSN, NERCachePath: dstCache}
	require.NoError(t, Restore(ctx, dstCfg, bundleDir))

	t.Run("named graph and RDF-star confidence annotation survive", func(t *testing.T) {
		named, err := NamedGraphs(ctx, dstFuseki)
		require.NoError(t, err)
		assert.Contains(t, named, "http://ex/run/1")

		got := sparqlSelectFirstValue(t, ctx, dstFuseki, confidenceQuery, "c")
		assert.Equal(t, "0.9", got)
	})

	t.Run("default graph survives", func(t *testing.T) {
		hasDefault, err := HasDefaultGraphData(ctx, dstFuseki)
		require.NoError(t, err)
		assert.True(t, hasDefault)

		got := sparqlSelectFirstValue(t, ctx, dstFuseki,
			`SELECT ?name WHERE { <http://ex/muni> <http://ex/name> ?name }`, "name")
		assert.Equal(t, "Amsterdam", got)
	})

	t.Run("PostGIS rows and geometry survive", func(t *testing.T) {
		dstPool := connectPostgres(t, ctx, dstDSN)
		assert.Equal(t, "POINT(1 2)", geomAsText(t, ctx, dstPool, "audit_probe", 1))
	})

	t.Run("NER cache files survive", func(t *testing.T) {
		assert.Equal(t, "cached extraction output", readNERCacheEntry(t, dstCache, "doc1__modelA"))
	})
}

// TestDumpRestore_SkipBAG covers task 7.6: export with --skip-bag, then restore into a target
// that already holds a BAG stand-in table plus a named graph absent from the bundle — the bundled
// audit table and graph must land exactly, while the non-bundled BAG table and graph must survive
// unchanged (additive restore, design D6).
func TestDumpRestore_SkipBAG(t *testing.T) {
	ctx := context.Background()

	srcFuseki := isolatedFuseki(t, ctx)
	srcDSN := isolatedPostgres(t, ctx)
	srcCache := t.TempDir()

	postNQuads(t, ctx, srcFuseki, `<http://ex/s> <http://ex/p> <http://ex/o> <http://ex/run/bundled> .
`)
	srcPool := connectPostgres(t, ctx, srcDSN)
	seedGeometryTable(t, ctx, srcPool, "audit_probe", 1)
	seedNoteTable(t, ctx, srcPool, "bag_probe", "source BAG content (must not appear in bundle)")

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	srcCfg := Config{FusekiURL: srcFuseki, DatabaseURL: srcDSN, NERCachePath: srcCache}
	require.NoError(t, Export(ctx, srcCfg, bundleDir, ExportOptions{SkipBAG: true}, time.Now().UTC()))

	manifest, err := ReadManifest(bundleDir)
	require.NoError(t, err)
	assert.True(t, manifest.SkipBAG)

	dstFuseki := isolatedFuseki(t, ctx)
	dstDSN := isolatedPostgres(t, ctx)
	dstCache := t.TempDir()

	// Pre-seed the target with content the bundle does NOT carry: a BAG stand-in table with its
	// own pre-existing content, and a named graph the bundle never mentions.
	postNQuads(t, ctx, dstFuseki, `<http://ex/existing> <http://ex/p> <http://ex/o> <http://ex/run/pre-existing> .
`)
	dstPool := connectPostgres(t, ctx, dstDSN)
	seedNoteTable(t, ctx, dstPool, "bag_probe", "pre-existing target BAG content")

	dstCfg := Config{FusekiURL: dstFuseki, DatabaseURL: dstDSN, NERCachePath: dstCache}
	require.NoError(t, Restore(ctx, dstCfg, bundleDir))

	t.Run("bundled audit table restores exactly", func(t *testing.T) {
		assert.True(t, tableExists(t, ctx, dstPool, "audit_probe"))
		assert.Equal(t, "POINT(1 2)", geomAsText(t, ctx, dstPool, "audit_probe", 1))
	})

	t.Run("non-bundled BAG stand-in table is preserved unchanged", func(t *testing.T) {
		assert.Equal(t, "pre-existing target BAG content", noteValue(t, ctx, dstPool, "bag_probe", 1))
	})

	t.Run("bundled named graph restores exactly", func(t *testing.T) {
		named, err := NamedGraphs(ctx, dstFuseki)
		require.NoError(t, err)
		assert.Contains(t, named, "http://ex/run/bundled")
	})

	t.Run("non-bundled named graph is preserved unchanged", func(t *testing.T) {
		named, err := NamedGraphs(ctx, dstFuseki)
		require.NoError(t, err)
		assert.Contains(t, named, "http://ex/run/pre-existing")

		got := sparqlSelectFirstValue(t, ctx, dstFuseki,
			`SELECT ?o WHERE { GRAPH <http://ex/run/pre-existing> { <http://ex/existing> <http://ex/p> ?o } }`, "o")
		assert.Equal(t, "http://ex/o", got)
	})
}

// TestDumpIntegration_ProductionNameGuardAbortsTestPath covers task 7.5: the dump integration
// test path resolves every target exclusively through internal/testdb (isolatedFuseki/
// isolatedPostgres above) — this asserts that path aborts outright, rather than silently
// operating against production, if it were ever pointed at a reserved production name.
func TestDumpIntegration_ProductionNameGuardAbortsTestPath(t *testing.T) {
	ctx := context.Background()
	adminDSN := requireEnv(t, "GS_TEST_DATABASE_URL")
	baseURL := requireEnv(t, "GS_TEST_FUSEKI_URL")
	adminPassword := requireEnv(t, "FUSEKI_ADMIN_PASSWORD")

	require.Error(t, testdb.CreateDatabase(ctx, adminDSN, "gemeten_stad"),
		"the test path's database provisioning must abort against the production database name")
	require.Error(t, testdb.CreateDataset(ctx, baseURL, adminPassword, "ds"),
		"the test path's dataset provisioning must abort against the production dataset name")
}
