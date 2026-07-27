//go:build integration

// Integration tests for tasks 6.3 (PostGIS persistence), 8.1 (idempotency), and 8.2 (end-to-end
// over the fixture corpus): koop.Load exercised against the real P6 resolver, the real P12 graph
// writer, and a real PostGIS target, in an isolated internal/testdb schema + the shared gs-test
// Fuseki dataset (see integration_harness_test.go).
package koop

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
	"github.com/Blogem/gemeten-stad/load/graph"
)

// TestKoopLoad_EndToEndCorpus is tasks 6.3 + 8.2 combined: one koop.Load run over the full fixture
// corpus (Noord aanvraag+besluit pair, pending, out-of-Noord, unresolvable, point-floor),
// asserting the aggregate PostGIS + graph outcome the scoping policy promises:
//
//   - Noord zaak: two PostGIS rows (aanvraag + besluit), keyed by gmb_id; the besluit row carries
//     its resolution; its Intervention is graphed with locatedAt -> the P12b-seeded Noord Place.
//   - pending zaak: one PostGIS row (aanvraag only), never graphed (no besluit yet).
//   - out-of-Noord zaak: no PostGIS rows at all, no graph entities -- excluded entirely.
//   - unresolvable zaak: one PostGIS row marked unresolved, never graphed.
//   - point-floor zaak: one PostGIS row (buurt-tier resolution), graphed at 0.50 confidence with
//     the unresolvedLocation caveat, locatedAt the SAME Noord Place as the address-tier besluit.
func TestKoopLoad_EndToEndCorpus(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	dsURL := fusekiDatasetURL(t)
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedGeo(t, ctx, pool)
	store := shared.NewRawStore(t.TempDir())
	landFullCorpus(t, store)

	resetFusekiRunGraphs(t, ctx, dsURL)
	// Pre-seed the P12b Place skeleton for the Noord buurt, so the graph assertions below can
	// confirm both Noord Interventions' locatedAt target is exactly this seeded node (task 8.2's
	// own wording), not merely koop's own bare `a gs:Place` re-assertion.
	require.NoError(t, graph.Load(ctx, dsURL, []byte(p12bPlaceCandidate), graph.Config{}))

	// cfg.Reset is deliberately false, for the same reason as the immutableConflict test: Reset
	// must never disturb the P12b Place just seeded above.
	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: false}))

	t.Run("PostGIS: Noord zaak persists both aanvraag and besluit rows", func(t *testing.T) {
		aanvraag, found := queryPublication(t, ctx, pool, gmbNoordAanvraag)
		require.True(t, found, "the Noord zaak's aanvraag row must be persisted")
		assert.Equal(t, zaaknummerNoord, *aanvraag.Zaaknummer)
		assert.Equal(t, "aanvraag", *aanvraag.Kind)
		assert.Nil(t, aanvraag.ResolvedIdentificatie, "a non-besluit trail row carries no resolution")
		assert.False(t, aanvraag.Unresolved, "a non-besluit row was never subject to resolution")

		besluit, found := queryPublication(t, ctx, pool, gmbNoordBesluit)
		require.True(t, found, "the Noord zaak's besluit row must be persisted")
		assert.Equal(t, zaaknummerNoord, *besluit.Zaaknummer)
		assert.Equal(t, "besluit", *besluit.Kind)
		assert.Equal(t, "1024BB", *besluit.Postcode)
		require.NotNil(t, besluit.Huisnummer)
		assert.Equal(t, 8, *besluit.Huisnummer)
		require.NotNil(t, besluit.ResolvedIdentificatie)
		assert.Equal(t, noordPlaceIdentificatie, *besluit.ResolvedIdentificatie)
		require.NotNil(t, besluit.ResolvedBuurtCode)
		assert.Equal(t, "N01", *besluit.ResolvedBuurtCode)
		require.NotNil(t, besluit.ResolvedConfidence)
		assert.InDelta(t, 0.90, *besluit.ResolvedConfidence, 1e-9)
		require.NotNil(t, besluit.InNoord)
		assert.True(t, *besluit.InNoord)
		assert.False(t, besluit.Unresolved)
		require.NotNil(t, besluit.GeomWKT, "geom is the record's own landed point")
		assert.Contains(t, *besluit.GeomWKT, "122000")
		assert.Contains(t, *besluit.GeomWKT, "490000")
		// The address tier resolves to vbo-orehof-8's own precise BAG point (121000, 487000, per
		// koop_geo_seed.sql), kept as silver in resolved_geom — distinct from the record's own
		// coarse landed point (122000, 490000) asserted just above.
		require.NotNil(t, besluit.ResolvedTier)
		assert.Equal(t, "address", *besluit.ResolvedTier)
		require.NotNil(t, besluit.ResolvedGeomWKT, "address tier carries the precise resolved point")
		assert.Contains(t, *besluit.ResolvedGeomWKT, "121000")
		assert.Contains(t, *besluit.ResolvedGeomWKT, "487000")
	})

	t.Run("PostGIS: pending zaak persists its aanvraag only", func(t *testing.T) {
		row, found := queryPublication(t, ctx, pool, gmbPendingAanvraag)
		require.True(t, found, "a pending zaak (no besluit yet) must still be persisted")
		assert.Equal(t, zaaknummerPending, *row.Zaaknummer)
		assert.Equal(t, "aanvraag", *row.Kind)
		assert.Nil(t, row.ResolvedIdentificatie)
		assert.False(t, row.Unresolved)
	})

	t.Run("PostGIS: out-of-Noord zaak has no rows at all", func(t *testing.T) {
		_, found := queryPublication(t, ctx, pool, gmbOutsideNoordBesluit)
		assert.False(t, found, "a zaak whose besluit resolves outside Noord must be excluded entirely")
	})

	t.Run("PostGIS: unresolvable zaak is persisted and marked, never fabricated", func(t *testing.T) {
		row, found := queryPublication(t, ctx, pool, gmbUnresolvableBesluit)
		require.True(t, found, "an unresolvable besluit must still be persisted, marked, not dropped")
		assert.True(t, row.Unresolved)
		assert.Nil(t, row.ResolvedIdentificatie)
		assert.Nil(t, row.ResolvedConfidence)
		// The record's own landed point is real data (not a fabricated resolution) and is kept.
		require.NotNil(t, row.GeomWKT)
		assert.Contains(t, *row.GeomWKT, "999000")
		// Unresolvable: both resolution columns stay NULL, never fabricated.
		assert.Nil(t, row.ResolvedGeomWKT)
		assert.Nil(t, row.ResolvedTier)
	})

	t.Run("PostGIS: point-floor zaak resolves at the buurt floor with unresolvedLocation", func(t *testing.T) {
		row, found := queryPublication(t, ctx, pool, gmbPointFloorBesluit)
		require.True(t, found)
		require.NotNil(t, row.ResolvedIdentificatie)
		assert.Equal(t, noordPlaceIdentificatie, *row.ResolvedIdentificatie)
		require.NotNil(t, row.ResolvedConfidence)
		assert.InDelta(t, 0.50, *row.ResolvedConfidence, 1e-9)
		assert.Contains(t, row.Caveats, "unresolvedLocation")
		require.NotNil(t, row.InNoord)
		assert.True(t, *row.InNoord)
		assert.False(t, row.Unresolved)
		// Buurt tier: the buurt code records the place, but there is no precise BAG point to keep
		// as silver, so resolved_geom stays NULL while resolved_tier records "buurt".
		require.NotNil(t, row.ResolvedTier)
		assert.Equal(t, "buurt", *row.ResolvedTier)
		assert.Nil(t, row.ResolvedGeomWKT, "buurt tier carries no precise resolved point")
	})

	t.Run("PostGIS: total row count matches the scoping policy (out-of-Noord excluded)", func(t *testing.T) {
		// Noord aanvraag + Noord besluit + pending + unresolvable + point-floor = 5;
		// out-of-Noord contributes zero.
		assert.Equal(t, 5, countPublications(t, ctx, pool))
	})

	t.Run("graph: Noord besluit's Intervention targets the P12b-seeded Place", func(t *testing.T) {
		assert.True(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <`+noordInterventionIRI+`> a gs:Intervention ; gs:locatedAt <`+noordPlaceIRI+`> } }`))
		assert.True(t, sparqlAsk(t, dsURL, `PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
ASK { GRAPH ?g { <`+noordPlaceIRI+`> rdfs:label "Testbuurt Noord"@nl } }`),
			"the locatedAt target is the SAME Place P12b seeded (label survives koop's bare re-assertion)")
	})

	t.Run("graph: point-floor besluit is graphed at 0.50 confidence into the same Noord Place", func(t *testing.T) {
		pointFloorInterventionIRI := "http://gemetenstad.nl/id/intervention/" + zaaknummerPointFloor
		assert.True(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <`+pointFloorInterventionIRI+`> a gs:Intervention ; gs:locatedAt <`+noordPlaceIRI+`> } }`))
		assert.True(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { << <`+pointFloorInterventionIRI+`> gs:locatedAt <`+noordPlaceIRI+`> >> gs:confidence 0.5 ;
                 gs:caveat gs:unresolvedLocation } }`))
	})

	t.Run("graph: out-of-Noord and unresolvable zaken have no Intervention", func(t *testing.T) {
		outsideIRI := "http://gemetenstad.nl/id/intervention/" + zaaknummerOutsideNoord
		unresolvableIRI := "http://gemetenstad.nl/id/intervention/" + zaaknummerUnresolvable
		pendingIRI := "http://gemetenstad.nl/id/intervention/" + zaaknummerPending

		assert.False(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <`+outsideIRI+`> a gs:Intervention } }`))
		assert.False(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <`+unresolvableIRI+`> a gs:Intervention } }`))
		assert.False(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <`+pendingIRI+`> a gs:Intervention } }`), "a pending zaak (no besluit) mints no Intervention IRI at all")
	})
}

// snapshotRows reads back every gmb id in ids as a pubRow, in the given order, for the
// idempotency test's before/after comparison.
func snapshotRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ids []string) []pubRow {
	t.Helper()
	rows := make([]pubRow, 0, len(ids))
	for _, id := range ids {
		row, found := queryPublication(t, ctx, pool, id)
		require.True(t, found, "expected row %s to exist for the idempotency snapshot", id)
		rows = append(rows, row)
	}
	return rows
}

// TestKoopLoad_UnchangedRerunIsANoOp is task 8.1's core scenario: re-running koop.Load against the
// exact same landed corpus must leave PostGIS and the graph completely undisturbed -- no row
// content change, no new run:load-... graph.
func TestKoopLoad_UnchangedRerunIsANoOp(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	dsURL := fusekiDatasetURL(t)
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedGeo(t, ctx, pool)
	store := shared.NewRawStore(t.TempDir())
	landNoordZaak(t, store)

	resetFusekiRunGraphs(t, ctx, dsURL)

	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: true}))

	ids := []string{gmbNoordAanvraag, gmbNoordBesluit}
	rowCountBefore := countPublications(t, ctx, pool)
	rowsBefore := snapshotRows(t, ctx, pool, ids)
	graphsBefore := sparqlGraphsWithPrefix(t, dsURL, gsRunGraphPrefix)

	besluitBefore, found := queryPublication(t, ctx, pool, gmbNoordBesluit)
	require.True(t, found)
	require.NotNil(t, besluitBefore.LoadedAt, "loaded_at must be populated (NOT NULL) after the initial load")

	// Re-run against the exact same landed corpus, unchanged.
	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: false}))

	assert.Equal(t, rowCountBefore, countPublications(t, ctx, pool), "no row count change on an unchanged re-run")
	assert.Equal(t, rowsBefore, snapshotRows(t, ctx, pool, ids), "no row content change on an unchanged re-run")

	graphsAfter := sparqlGraphsWithPrefix(t, dsURL, gsRunGraphPrefix)
	assert.ElementsMatch(t, graphsBefore, graphsAfter, "no new run:load-... graph on an unchanged re-run")

	besluitAfter, found := queryPublication(t, ctx, pool, gmbNoordBesluit)
	require.True(t, found)
	require.NotNil(t, besluitAfter.LoadedAt)
	assert.Equal(t, *besluitBefore.LoadedAt, *besluitAfter.LoadedAt,
		"loaded_at must be excluded from MERGE change-detection — an unchanged re-run must not touch it")
}

// TestKoopLoad_ResolveInfraErrorRetainsTrail covers the resolve-error trail retention fix
// (load-koop-assembly): when resolveBesluit returns a genuine infra error (not the normal
// "unresolvable" outcome), koop.Load must retain the whole zaak's trail — each publication
// persisted as a row with NULL resolution and unresolved == false, like a pending zaak — log +
// count it, never graph it, and still return nil (the run itself must not abort).
//
// The infra error is forced the reachable way: seedGeo loads the FULL geo fixture so the Noord
// besluit's title address (Örehof 8, 1024BB) still resolves via bag_* at the address tier
// (location.Resolve never touches gebieden_buurten — see location/resolve.go, the buurt-PIP tier
// only runs when address AND postcode both miss). We then break the schema-local
// gebieden_buurten table itself (ALTER ... DROP COLUMN geom) rather than dropping it outright:
// with search_path=<schema>,public, dropping the table entirely would make the unqualified
// reference in buurtFor's SQL fall through to public.gebieden_buurten (the dev DB's real
// Amsterdam buurten), which would resolve the Örehof point into a real, non-Noord buurt and hit
// the (working) exclusion path instead of an infra error. Keeping the table but dropping its geom
// column means the unqualified reference still resolves to the schema-local table (no fallback),
// and buurtFor's subsequent point-in-polygon lookup (buurtByPointSQL's `ST_Contains(geom, ...)`)
// fails outright with "column geom does not exist" — a genuine SQL/infra failure, not an
// unresolvable-location outcome.
func TestKoopLoad_ResolveInfraErrorRetainsTrail(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	dsURL := fusekiDatasetURL(t)
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedGeo(t, ctx, pool)
	store := shared.NewRawStore(t.TempDir())
	landFixture(t, store, "it_corpus_noord_besluit", gmbNoordBesluit)

	resetFusekiRunGraphs(t, ctx, dsURL)

	_, err := pool.Exec(ctx, "ALTER TABLE gebieden_buurten DROP COLUMN geom CASCADE")
	require.NoError(t, err, "break the address-tier buurt lookup's own geom column, in this test's isolated schema only")

	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: true}),
		"a resolve infra error must not abort the whole run — Load still returns nil")

	row, found := queryPublication(t, ctx, pool, gmbNoordBesluit)
	require.True(t, found, "the besluit's own row must be retained as a pending, retry-able trail row, not dropped")
	assert.Nil(t, row.ResolvedIdentificatie, "no resolution was reached — the column must stay NULL, never fabricated")
	assert.Nil(t, row.ResolvedBuurtCode)
	assert.Nil(t, row.ResolvedConfidence)
	assert.Nil(t, row.ResolvedGeomWKT)
	assert.Nil(t, row.ResolvedTier)
	assert.False(t, row.Unresolved,
		"a resolve infra error is not the same outcome as an unresolvable besluit — it must read as a pending row (unresolved=false), eligible for retry on the next run")

	assert.False(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <`+noordInterventionIRI+`> a gs:Intervention } }`),
		"a zaak whose resolve hit an infra error must never be graphed")
}

// reResolveNoordAddressSQL is the mid-test mutation that forces a re-resolution (task 8.1's other
// scenario): it adds a brand-new, disjoint Noord buurt (N03BUURT, code "N03", still in scope) to
// gebieden_buurten, then moves the Noord besluit's address-tier BAG point (vbo-orehof-8 -- the
// point postcode 1024BB/huisnummer 8 resolves to, per koop_geo_seed.sql) out of N01BUURT and into
// N03BUURT. The address itself (postcode/huisnummer/openbareruimteref) is untouched, so the
// address tier still fires at the same 0.90 confidence on the second run -- only the
// point-in-polygon buurt result changes. This never touches koop_geo_seed.sql itself; it only ADDS
// a disjoint buurt and UPDATEs the one BAG point this test needs moved, exactly as the task
// contract asks.
const reResolveNoordAddressSQL = `
INSERT INTO gebieden_buurten (identificatie, naam, code, ligtinwijkid, geom) VALUES
    ('N03BUURT', 'Testbuurt Noord Reresolved', 'N03', 'N01WIJK',
     ST_GeomFromText('POLYGON((125000 486800, 125200 486800, 125200 487150, 125000 487150, 125000 486800))', 28992));

UPDATE bag_verblijfsobject
SET geom = ST_GeomFromText('POINT(125100 487000)', 28992)
WHERE identificatie = 'vbo-orehof-8';
`

const (
	// n03PlaceIdentificatie/n03PlaceIRI are the re-resolution target: a brand-new Noord buurt,
	// disjoint from N01BUURT, minted inline by reResolveNoordAddressSQL above (not part of
	// koop_geo_seed.sql).
	n03PlaceIdentificatie = "N03BUURT"
	n03PlaceIRI           = "http://gemetenstad.nl/id/place/N03BUURT"
)

// TestKoopLoad_ReResolutionOpensNewVersion is task 8.1's re-resolution scenario -- the other half
// of TestKoopLoad_UnchangedRerunIsANoOp above: WHEN a besluit's resolved location changes between
// runs (specs/koop-load/spec.md's "A re-resolution opens a new version"), THEN:
//
//   - PostGIS: the besluit's koop_publications row is upserted IN PLACE (same gmb_id, no second
//     row), its resolved_identificatie/resolved_buurt_code/resolved_geom refreshed to the newly
//     resolved buurt.
//   - Graph: a new gs:locatedAt version opens, targeting the new Place; the prior version (to the
//     original Place) is retained -- never deleted -- but stamped with gs:validTo, i.e. closed, so
//     exactly one open version remains.
func TestKoopLoad_ReResolutionOpensNewVersion(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	dsURL := fusekiDatasetURL(t)
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedGeo(t, ctx, pool)
	store := shared.NewRawStore(t.TempDir())
	landNoordZaak(t, store)

	resetFusekiRunGraphs(t, ctx, dsURL)

	// Run 1: the besluit resolves address-tier (0.90) into the original Noord buurt, N01BUURT.
	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: true}))

	rowCountBeforeReResolution := countPublications(t, ctx, pool)

	before, found := queryPublication(t, ctx, pool, gmbNoordBesluit)
	require.True(t, found)
	require.NotNil(t, before.ResolvedIdentificatie)
	assert.Equal(t, noordPlaceIdentificatie, *before.ResolvedIdentificatie, "run 1 resolves into the original Noord buurt")
	require.NotNil(t, before.LoadedAt, "loaded_at must be populated (NOT NULL) after the initial load")

	assert.True(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <`+noordInterventionIRI+`> gs:locatedAt <`+noordPlaceIRI+`> } }`),
		"run 1's Intervention locates at the original Noord Place")

	// Force a re-resolution between the two runs.
	_, err := pool.Exec(ctx, reResolveNoordAddressSQL)
	require.NoError(t, err)

	// Run 2: same landed corpus, re-run against the moved BAG address.
	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: false}))

	t.Run("PostGIS: the besluit's row is upserted in place, not duplicated", func(t *testing.T) {
		assert.Equal(t, rowCountBeforeReResolution, countPublications(t, ctx, pool),
			"a re-resolution upserts the existing row, it never inserts a second one")

		after, found := queryPublication(t, ctx, pool, gmbNoordBesluit)
		require.True(t, found)
		require.NotNil(t, after.ResolvedIdentificatie)
		assert.Equal(t, n03PlaceIdentificatie, *after.ResolvedIdentificatie, "the resolved buurt is refreshed to the new Noord buurt")
		require.NotNil(t, after.ResolvedBuurtCode)
		assert.Equal(t, "N03", *after.ResolvedBuurtCode)
		require.NotNil(t, after.InNoord)
		assert.True(t, *after.InNoord, "the new buurt is still in scope (code N03 -- Noord)")
		require.NotNil(t, after.ResolvedTier)
		assert.Equal(t, "address", *after.ResolvedTier, "still an address-tier resolution -- only the buurt it lands in changed")
		require.NotNil(t, after.ResolvedGeomWKT)
		assert.Contains(t, *after.ResolvedGeomWKT, "125100", "resolved_geom is refreshed to the moved BAG point")
		assert.Contains(t, *after.ResolvedGeomWKT, "487000")

		require.NotNil(t, after.LoadedAt)
		assert.True(t, after.LoadedAt.After(*before.LoadedAt),
			"loaded_at must be bumped on a genuine change -- a re-resolution is not excluded from change-detection, only loaded_at's own column is")
	})

	t.Run("graph: a new locatedAt version opens at the new Place, the prior version is closed", func(t *testing.T) {
		// The new version is open (no gs:validTo) and targets the new Place, carrying the same
		// address-tier confidence (0.90) as before.
		assert.True(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <`+noordInterventionIRI+`> gs:locatedAt <`+n03PlaceIRI+`> .
                 << <`+noordInterventionIRI+`> gs:locatedAt <`+n03PlaceIRI+`> >> gs:confidence 0.9 .
                 FILTER NOT EXISTS { << <`+noordInterventionIRI+`> gs:locatedAt <`+n03PlaceIRI+`> >> gs:validTo ?vt } } }`),
			"a new, open gs:locatedAt version targets the newly resolved Place")

		// The prior version's triple is retained -- history is never deleted -- but is now closed:
		// stamped with gs:validTo rather than left open.
		assert.True(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <`+noordInterventionIRI+`> gs:locatedAt <`+noordPlaceIRI+`> } }`),
			"the prior version's triple is retained, not deleted")
		assert.True(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { << <`+noordInterventionIRI+`> gs:locatedAt <`+noordPlaceIRI+`> >> gs:validTo ?vt } }`),
			"the prior version is stamped with gs:validTo -- closed, not left open")

		// The prior version is no longer the OPEN one: re-running the same "open" pattern against
		// it (locatedAt with no validTo) must now fail, since only the new Place's version is open.
		assert.False(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <`+noordInterventionIRI+`> gs:locatedAt <`+noordPlaceIRI+`> .
                 FILTER NOT EXISTS { << <`+noordInterventionIRI+`> gs:locatedAt <`+noordPlaceIRI+`> >> gs:validTo ?vt } } }`),
			"the original version is no longer the open one")
	})
}
