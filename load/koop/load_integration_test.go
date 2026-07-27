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
//
// TODO(re-resolution half, task 8.1): a re-resolution scenario (e.g. a BAG update that moves the
// Noord besluit's address into a different buurt, opening a new graph version and refreshing the
// PostGIS row) is NOT exercised here -- it needs either a second geo seed variant or a mid-test
// mutation of the seeded BAG rows, which is heavier than this suite's fixture-corpus shape
// supports cleanly. Deferred; see the tester's handoff report for the reasoning.
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

	// Re-run against the exact same landed corpus, unchanged.
	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: false}))

	assert.Equal(t, rowCountBefore, countPublications(t, ctx, pool), "no row count change on an unchanged re-run")
	assert.Equal(t, rowsBefore, snapshotRows(t, ctx, pool, ids), "no row content change on an unchanged re-run")

	graphsAfter := sparqlGraphsWithPrefix(t, dsURL, gsRunGraphPrefix)
	assert.ElementsMatch(t, graphsBefore, graphsAfter, "no new run:load-... graph on an unchanged re-run")
}
