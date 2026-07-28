//go:build integration

// End-to-end integration test for the P14 coverage-audit derive step (OpenSpec change
// derive-coverage-audit, task 5.4 — the change's headline gate — plus the DB-backed halves of
// tasks 2.4 (SRID-aligned distance) and 4.6 (audit_metrics upsert idempotency)).
//
// The seeded corpus below is engineered so every acceptance scenario in
// openspec/changes/derive-coverage-audit/specs/coverage-audit/spec.md is genuinely reached by
// Run's actual scoring/assignment arithmetic (design.md D5/D6), not asserted by construction:
//
//   - Z-STRONG-001 (buurt BUURT-STRONG): an address-tier permit with a resolved point, one
//     uncontested felling 30m away, within [pub,+3yr] and <=2yr lag -> place 0.90 + count
//     unknown +0 + time +0.15 + ambiguity(sole) +0.05 = 1.10 clamped to 1.00 >= tau. Also the
//     known-distance SRID-alignment pin (task 2.4): the felling is stored in SRID 4326 at a point
//     exactly 30.0m (a 18/24/30 triangle) from the permit's SRID 28992 point.
//   - Z-CONTEND-1-WEAK / Z-CONTEND-2-OTHER (buurt BUURT-CONTEND): two point-less buurt-tier
//     permits with the IDENTICAL publication date, both candidates for the same felling F-WEAK-A
//     at a 2-3yr lag (881 days). Their per-pair scores tie exactly (0.60 each); the deterministic
//     tie-break (nil distance, then zaaknummer) hands the felling to Z-CONTEND-1-WEAK. Its FINAL
//     confidence then reflects the true contention (2 candidate-permits) via the ambiguity term:
//     0.50 + 0 + 0.05 - 0.03 = 0.52 < tau -> matched period with gs:caveat gs:weakLink.
//   - Z-CONTEST-A / Z-CONTEST-B (buurt BUURT-CONTEST): F-CONTESTED-01 is a candidate for both, but
//     Z-CONTEST-A's address-tier proximity (1.00 clamped) beats Z-CONTEST-B's buurt floor (0.70)
//     outright -> exclusive assignment to A only, B ends up no-source.
//   - Z-MULTI-001 (buurt BUURT-MULTI): three uncontested fellings all close to its point -> all
//     three assigned, assigned_felling_count == 3.
//   - Z-NOSOURCE-001 (buurt BUURT-NOSOURCE): zero kapenherplant rows in its buurt at Run #1 ->
//     no-source period; a qualifying felling is inserted afterward and Run #3 (below) asserts it
//     opens a new matched period and closes the prior no-source one.
//   - Z-CROSSBOUNDARY-001 (buurt BUURT-CROSSB) / F-CROSSBOUNDARY-01 (buurt BUURT-CROSSB-FAR, a
//     DIFFERENT gbdBuurtId): design.md D6's additive spatial clause -- the felling sits 150m from
//     the permit's point (a 90/120/150 triangle), inside crossBoundaryRadiusM=200 but in a buurt the
//     buurt-equals clause would never match. Uncontested, so it is assigned outright: place
//     (postcode-tier proximity, 0.70) + count unknown (+0) + time (151-day lag, +0.15) + ambiguity
//     (sole, +0.05) = 0.90 >= tau, granularity postcode.
//   - F-MULTI-04-FAR: a fourth felling added to BUURT-MULTI/Z-MULTI-001, 250m from its point --
//     beyond crossBoundaryRadiusM, so only the buurt clause (not the spatial one) keeps it a
//     candidate, pinning the union's "far same-buurt fellings are never dropped" fragmentation-safety
//     property (D6).
package coverage

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/load/graph"
)

// -- Seed identifiers -----------------------------------------------------------------------------
//
// Every zaaknummer/felling-id/buurt-code/buurt-identificatie below is a plain, caller-chosen string
// local to this test (no join against real BAG/gebieden data, unlike load/koop's own harness) —
// candidates.go joins koop_publications.resolved_identificatie against kapenherplant."gbdBuurtId" by
// string equality (the GBD buurt identificatie system), so any distinct strings suffice to scope each
// scenario from the others. The buurt* constants below are the short, human-readable buurtcode
// (koop_publications.resolved_buurt_code — evidence text only, NOT the join key); the gbd* constants
// are the 14-digit-style GBD buurt identificatie (koop_publications.resolved_identificatie ==
// kapenherplant."gbdBuurtId" — the actual join key). The two sets are deliberately DISTINCT strings
// per scenario: if candidate generation ever regresses to joining on resolved_buurt_code again, every
// scenario below would see zero candidate fellings and the suite would fail loudly.

const (
	buurtStrong    = "BUURT-STRONG"
	buurtContend   = "BUURT-CONTEND"
	buurtContest   = "BUURT-CONTEST"
	buurtMulti     = "BUURT-MULTI"
	buurtNoSource  = "BUURT-NOSOURCE"
	buurtCrossB    = "BUURT-CROSSB"     // the cross-boundary-catch permit's OWN buurt
	buurtCrossBFar = "BUURT-CROSSB-FAR" // the felling's DIFFERENT buurt, deliberately never a permit's buurt

	gbdStrong    = "03630000000001"
	gbdContend   = "03630000000002"
	gbdContest   = "03630000000003"
	gbdMulti     = "03630000000004"
	gbdNoSource  = "03630000000005"
	gbdCrossB    = "03630000000006"
	gbdCrossBFar = "03630000000007"

	zStrong   = "Z-STRONG-001"
	zWeak1    = "Z-CONTEND-1-WEAK"
	zWeak2    = "Z-CONTEND-2-OTHER"
	zContestA = "Z-CONTEST-A"
	zContestB = "Z-CONTEST-B"
	zMulti    = "Z-MULTI-001"
	zNoSource = "Z-NOSOURCE-001"
	zCrossB   = "Z-CROSSBOUNDARY-001"

	fStrong    = "F-STRONG-01"
	fWeak      = "F-WEAK-A"
	fContested = "F-CONTESTED-01"
	fMulti1    = "F-MULTI-01"
	fMulti2    = "F-MULTI-02"
	fMulti3    = "F-MULTI-03"
	fMulti4Far = "F-MULTI-04-FAR" // same buurt as F-MULTI-01..03, but beyond crossBoundaryRadiusM
	fNewNoSrc  = "F-NOSOURCE-NEW-01"
	fCrossB    = "F-CROSSBOUNDARY-01" // different gbdBuurtId than Z-CROSSBOUNDARY-001, within 200m of its point
)

// interventionIRIFor / anchorIRIFor mirror candidates.go's interventionPrefix and
// contentkey.go's mintAnchorIRI, spelled out again here (deliberately, matching the repeated-string
// convention load/koop/integration_harness_test.go itself uses for gsRunGraphPrefix): a test
// asserting the writer's own IRI-minting contract should not import the very same unexported
// helper it means to be checking end to end, so subject IRIs below are built from the settled
// namespace strings directly.
func interventionIRIFor(zaaknummer string) string {
	return "http://gemetenstad.nl/id/intervention/" + zaaknummer
}
func anchorIRIFor(zaaknummer string) string {
	return "http://gemetenstad.nl/id/auditlink/" + zaaknummer
}
func placeIRIFor(buurt string) string {
	return "http://gemetenstad.nl/id/place/" + buurt
}

func TestCoverageEndToEnd(t *testing.T) {
	ctx := context.Background()
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	fusekiURL := fusekiDatasetURL(t)

	pool := newSchemaPool(t, ctx, dsn)
	createCoverageTables(t, ctx, pool)

	resetFusekiRunGraphs(t, ctx, fusekiURL)
	t.Cleanup(func() { resetFusekiRunGraphs(t, ctx, fusekiURL) })

	seedPublications(t, ctx, pool)
	seedFellings(t, ctx, pool)
	seedInterventions(t, ctx, fusekiURL)

	// -- Malformed period rejected (Requirement: Write derived coverage through the SHACL gate) --
	//
	// Run this BEFORE the first real coverage.Run so a failed write can never be confused with (or
	// contaminate) the real run's own graph state: Load must reject the whole candidate and write
	// nothing.
	t.Run("malformed period is rejected and nothing is written", func(t *testing.T) {
		const malformedZaaknummer = "Z-MALFORMED-001"
		anchor := anchorIRIFor(malformedZaaknummer)
		intervention := interventionIRIFor(malformedZaaknummer)
		observation := "http://gemetenstad.nl/id/observation/" + malformedZaaknummer

		malformed := fmt.Sprintf(`@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix dct: <http://purl.org/dc/terms/> .
<%s> a gs:Intervention ;
    dct:available "2022-01-01"^^xsd:date ;
    gs:locatedAt <http://gemetenstad.nl/id/place/malformed> {| gs:confidence 1.0 ; gs:evidence "seed" |} .
<http://gemetenstad.nl/id/place/malformed> a gs:Place .
<%s> a gs:AuditLink ;
    gs:coversIntervention <%s> .
<%s> a gs:Observation .
<%s/bad> a gs:CoveragePeriod ;
    gs:versionOf <%s> ;
    gs:validFrom "2026-01-01"^^xsd:date ;
    gs:linksObservation <%s> ;
    gs:evidence "malformed: linksObservation with no gs:confidence, no gs:noSourceFound" .
`, intervention, anchor, intervention, observation, anchor, anchor, observation)

		err := graph.Load(ctx, fusekiURL, []byte(malformed), graph.Config{})
		require.Error(t, err, "a gs:CoveragePeriod matching neither the matched nor no-source xone branch must be rejected")
		assert.Contains(t, err.Error(), "conform", "the SHACL non-conformance error must name the gate that rejected it")

		exists := sparqlAsk(t, ctx, fusekiURL, fmt.Sprintf(
			`PREFIX gs: <http://gemetenstad.nl/ns#> ASK { GRAPH ?g { <%s> a gs:AuditLink } }`, anchor))
		assert.False(t, exists, "no partial write: the malformed candidate's anchor must not have been written either")
	})

	// -- Run #1: the real derive run over the whole engineered corpus --------------------------
	require.NoError(t, Run(ctx, pool, fusekiURL, Config{}))

	t.Run("strong match: matched period >= tau, address granularity, known SRID distance", func(t *testing.T) {
		anchor := anchorIRIFor(zStrong)
		require.True(t, anchorExists(t, ctx, fusekiURL, anchor, interventionIRIFor(zStrong)))

		periods := openPeriodsFor(t, ctx, fusekiURL, anchor)
		require.Len(t, periods, 1, "exactly one open period for the strong-match permit's anchor")
		p := periods[0]

		assert.NotEmpty(t, p.ObservationIRI, "a matched period must link an Observation")
		assert.Equal(t, "http://gemetenstad.nl/id/observation/"+zStrong, p.ObservationIRI)
		assert.InDelta(t, 1.00, parseConfidence(t, p.Confidence), 1e-9, "clamped: 0.90 place + 0.15 time + 0.05 ambiguity = 1.10 -> 1.00")
		assert.Equal(t, "http://gemetenstad.nl/ns#address", p.Granularity)
		assert.False(t, p.IsNoSource, "a matched period must not carry gs:noSourceFound")

		m, found := queryMetric(t, ctx, pool, zStrong)
		require.True(t, found)
		assert.True(t, m.Matched)
		assert.Equal(t, []string{fStrong}, m.AssignedFellingIDs)
		assert.Equal(t, 1, m.AssignedFellingCount)
		assert.Equal(t, 1, m.CandidateCount)
		require.NotNil(t, m.NearestDistM, "the permit carries a resolved point, so nearest_dist_m must be populated")
		assert.InDelta(t, 30.0, *m.NearestDistM, 1.0,
			"task 2.4: the felling is stored at a KNOWN 18/24/30 metre offset in SRID 4326; "+
				"nearest_dist_m must reflect ST_Transform(...,28992) alignment against the permit's 28992 point")
	})

	t.Run("below-tau weak link: contested felling, buurt floor, weakLink caveat", func(t *testing.T) {
		winnerAnchor := anchorIRIFor(zWeak1)
		require.True(t, anchorExists(t, ctx, fusekiURL, winnerAnchor, interventionIRIFor(zWeak1)))

		periods := openPeriodsFor(t, ctx, fusekiURL, winnerAnchor)
		require.Len(t, periods, 1)
		p := periods[0]

		assert.Equal(t, "http://gemetenstad.nl/id/observation/"+zWeak1, p.ObservationIRI,
			"below tau is still a MATCHED period (>=1 assigned felling), not no-source")
		assert.InDelta(t, 0.52, parseConfidence(t, p.Confidence), 1e-9,
			"0.50 buurt floor + 0 count-unknown + 0.05 (2-3yr lag) - 0.03 (contested by 1 other permit) = 0.52")
		assert.Equal(t, "http://gemetenstad.nl/ns#buurt", p.Granularity)

		caveats := periodCaveats(t, ctx, fusekiURL, p.IRI)
		assert.Contains(t, caveats, "http://gemetenstad.nl/ns#weakLink", "score 0.52 < tau=0.60 must carry the weakLink caveat")
		assert.Contains(t, caveats, "http://gemetenstad.nl/ns#countUnknown")

		m, found := queryMetric(t, ctx, pool, zWeak1)
		require.True(t, found)
		assert.True(t, m.Matched)
		assert.Equal(t, []string{fWeak}, m.AssignedFellingIDs)

		// The losing tie-break side must show the felling was NOT assigned to it.
		loserMetric, found := queryMetric(t, ctx, pool, zWeak2)
		require.True(t, found)
		assert.NotContains(t, loserMetric.AssignedFellingIDs, fWeak,
			"the deterministic tie-break gives F-WEAK-A to Z-CONTEND-1-WEAK only")
		assert.False(t, loserMetric.Matched, "having lost its only candidate felling, the loser ends up no-source")
	})

	t.Run("contested felling is assigned exclusively to the higher-scoring permit", func(t *testing.T) {
		winner, found := queryMetric(t, ctx, pool, zContestA)
		require.True(t, found)
		assert.Contains(t, winner.AssignedFellingIDs, fContested)

		loser, found := queryMetric(t, ctx, pool, zContestB)
		require.True(t, found)
		assert.NotContains(t, loser.AssignedFellingIDs, fContested,
			"a felling that is a candidate for two permits must never appear in more than one Observation")
		assert.False(t, loser.Matched, "the losing permit ends up no-source: its only candidate felling went to A")

		anchorB := anchorIRIFor(zContestB)
		periodsB := openPeriodsFor(t, ctx, fusekiURL, anchorB)
		require.Len(t, periodsB, 1)
		assert.True(t, periodsB[0].IsNoSource)
		assert.Empty(t, periodsB[0].ObservationIRI)
	})

	t.Run("multi-tree permit is assigned its whole matched set, including a far same-buurt felling", func(t *testing.T) {
		m, found := queryMetric(t, ctx, pool, zMulti)
		require.True(t, found)
		assert.True(t, m.Matched)
		// F-MULTI-04-FAR sits 250m from the permit's point (beyond crossBoundaryRadiusM=200) but in
		// the SAME buurt as F-MULTI-01..03: the union's buurt clause must still catch it (D6
		// fragmentation-safety) — the spatial radius does not shrink the buurt net.
		assert.ElementsMatch(t, []string{fMulti1, fMulti2, fMulti3, fMulti4Far}, m.AssignedFellingIDs)
		assert.Equal(t, 4, m.AssignedFellingCount)
		assert.Equal(t, 4, m.CandidateCount)

		anchor := anchorIRIFor(zMulti)
		periods := openPeriodsFor(t, ctx, fusekiURL, anchor)
		require.Len(t, periods, 1)
		assert.NotEmpty(t, periods[0].ObservationIRI)
	})

	t.Run("cross-boundary felling: caught only by the 200m spatial clause, not the buurt clause", func(t *testing.T) {
		anchor := anchorIRIFor(zCrossB)
		require.True(t, anchorExists(t, ctx, fusekiURL, anchor, interventionIRIFor(zCrossB)))

		periods := openPeriodsFor(t, ctx, fusekiURL, anchor)
		require.Len(t, periods, 1, "exactly one open period for the cross-boundary permit's anchor")
		p := periods[0]

		// F-CROSSBOUNDARY-01 carries gbdBuurtId=gbdCrossBFar, DIFFERENT from this permit's own
		// gbdCrossB — the buurt-equals clause alone would never select it. It is 150m away, inside
		// crossBoundaryRadiusM=200, so only the additive spatial clause makes it a candidate.
		assert.NotEmpty(t, p.ObservationIRI, "a matched period must link an Observation")
		assert.Equal(t, "http://gemetenstad.nl/id/observation/"+zCrossB, p.ObservationIRI)
		assert.Equal(t, "http://gemetenstad.nl/ns#postcode", p.Granularity,
			"150m is beyond addressRadiusM=50 but within postcodeRadiusM=200 -> postcode-tier place score")
		assert.InDelta(t, 0.90, parseConfidence(t, p.Confidence), 1e-9,
			"0.70 place (postcode) + 0 count-unknown + 0.15 (<=2yr lag) + 0.05 (sole, uncontested) = 0.90")
		assert.False(t, p.IsNoSource)

		m, found := queryMetric(t, ctx, pool, zCrossB)
		require.True(t, found)
		assert.True(t, m.Matched)
		assert.Equal(t, []string{fCrossB}, m.AssignedFellingIDs,
			"the cross-boundary felling is assigned to this permit even though it lies in a different buurt")
		assert.Equal(t, 1, m.CandidateCount,
			"F-CROSSBOUNDARY-01 is the only candidate: it would NOT be found by the buurt clause alone")
		require.NotNil(t, m.NearestDistM)
		assert.InDelta(t, 150.0, *m.NearestDistM, 1.0,
			"the felling is stored at a KNOWN 90/120/150 metre offset; nearest_dist_m must reflect the "+
				"SRID-aligned metric distance even though the felling crosses a buurt boundary")
	})

	t.Run("no-source permit: noSourceFound period, no Observation, zero-count metrics", func(t *testing.T) {
		anchor := anchorIRIFor(zNoSource)
		require.True(t, anchorExists(t, ctx, fusekiURL, anchor, interventionIRIFor(zNoSource)))

		periods := openPeriodsFor(t, ctx, fusekiURL, anchor)
		require.Len(t, periods, 1)
		p := periods[0]
		assert.True(t, p.IsNoSource)
		assert.Empty(t, p.ObservationIRI)
		assert.Empty(t, p.Confidence)
		assert.Empty(t, p.Granularity)
		assert.Contains(t, p.Evidence, buurtNoSource)

		m, found := queryMetric(t, ctx, pool, zNoSource)
		require.True(t, found)
		assert.False(t, m.Matched)
		assert.Equal(t, 0, m.AssignedFellingCount)
		assert.Equal(t, 0, m.CandidateCount)
		assert.Empty(t, m.AssignedFellingIDs)
	})

	// -- Idempotent no-op re-run (Requirement: Idempotent re-derivation) + Metrics upsert
	// idempotency (task 4.6) ---------------------------------------------------------------
	t.Run("unchanged re-run is a true no-op", func(t *testing.T) {
		graphsBefore := graphsWithPrefix(t, ctx, fusekiURL, gsRunGraphPrefix)
		periodsBefore := allPeriodIRIs(t, ctx, fusekiURL)
		metricsBefore := allMetrics(t, ctx, pool)
		require.NotEmpty(t, graphsBefore, "sanity: run #1 must have minted at least one run graph")
		require.NotEmpty(t, periodsBefore)
		require.NotEmpty(t, metricsBefore)

		require.NoError(t, Run(ctx, pool, fusekiURL, Config{}))

		graphsAfter := graphsWithPrefix(t, ctx, fusekiURL, gsRunGraphPrefix)
		periodsAfter := allPeriodIRIs(t, ctx, fusekiURL)
		metricsAfter := allMetrics(t, ctx, pool)

		assert.Equal(t, graphsBefore, graphsAfter,
			"an unchanged re-run must mint no new run:load-... graph: same content-key -> same period IRI -> the writer sees it unchanged")
		assert.Equal(t, periodsBefore, periodsAfter,
			"the same content-keyed gs:CoveragePeriod IRIs must be produced again, with no new period added")

		require.Equal(t, len(metricsBefore), len(metricsAfter), "no row count change")
		for zaaknummer, before := range metricsBefore {
			after, ok := metricsAfter[zaaknummer]
			require.True(t, ok, "zaaknummer %s must still have an audit_metrics row", zaaknummer)
			assert.Equal(t, before.Matched, after.Matched, "matched flag unchanged for %s", zaaknummer)
			assert.Equal(t, before.AssignedFellingIDs, after.AssignedFellingIDs, "assigned felling ids unchanged for %s", zaaknummer)
			assert.Equal(t, before.AssignedFellingCount, after.AssignedFellingCount, "assigned felling count unchanged for %s", zaaknummer)
			assert.Equal(t, before.CandidateCount, after.CandidateCount, "candidate count unchanged for %s", zaaknummer)
			assert.Equal(t, before.NearestDistM, after.NearestDistM, "nearest_dist_m unchanged for %s", zaaknummer)
			// run_id is excluded from the MERGE's change-detection predicate (metrics.go), so an
			// unchanged row keeps the run_id of whichever run last actually changed its numbers.
			assert.Equal(t, before.RunID, after.RunID, "run_id unchanged (retained) on a no-op re-run for %s", zaaknummer)
		}
	})

	// -- New felling opens matched + closes prior no-source ------------------------------------
	t.Run("a newly appearing felling opens a matched period and closes the prior no-source one", func(t *testing.T) {
		anchor := anchorIRIFor(zNoSource)

		priorPeriods := openPeriodsFor(t, ctx, fusekiURL, anchor)
		require.Len(t, priorPeriods, 1, "exactly one open (no-source) period before the new felling appears")
		priorPeriodIRI := priorPeriods[0].IRI
		require.True(t, priorPeriods[0].IsNoSource)

		insertNoSourceFelling(t, ctx, pool)

		require.NoError(t, Run(ctx, pool, fusekiURL, Config{}))

		openAfter := openPeriodsFor(t, ctx, fusekiURL, anchor)
		require.Len(t, openAfter, 1, "exactly one open period must remain for the anchor after the transition")
		newPeriod := openAfter[0]
		assert.NotEqual(t, priorPeriodIRI, newPeriod.IRI, "the transition must open a NEW content-keyed period, not reuse the no-source one")
		assert.NotEmpty(t, newPeriod.ObservationIRI, "the new open period must be the matched branch")
		assert.False(t, newPeriod.IsNoSource)

		closedValidTo := sparqlAsk(t, ctx, fusekiURL, fmt.Sprintf(
			`PREFIX gs: <http://gemetenstad.nl/ns#> ASK { GRAPH ?g { <%s> gs:validTo ?vt } }`, priorPeriodIRI))
		assert.True(t, closedValidTo, "the prior no-source period must be closed (gs:validTo stamped), not deleted")

		m, found := queryMetric(t, ctx, pool, zNoSource)
		require.True(t, found)
		assert.True(t, m.Matched)
		assert.Equal(t, []string{fNewNoSrc}, m.AssignedFellingIDs)
	})
}

// -- Seed data construction -------------------------------------------------------------------
//
// RD (SRID 28992) points below are arbitrary but valid within the Netherlands' RD bounding box
// (roughly x in [0,280000], y in [300000,625000]) — their absolute location is irrelevant, only
// the per-felling OFFSET from each permit's point matters, since that offset is what the
// known-distance SRID assertion (task 2.4, "strong match" subtest above) pins.

// rdPoint renders a SPARQL/SQL PostGIS point literal in SRID 28992.
func rdPoint(x, y int) string {
	return fmt.Sprintf("ST_SetSRID(ST_MakePoint(%d,%d),28992)", x, y)
}

// fellingPointFromRDOffset renders the PostGIS expression for a kapenherplant."resolvedGeom" value
// (SRID 4326): the permit's RD point (px,py) offset by (dx,dy) metres, transformed to 4326 — i.e.
// exactly what a real registry row derived from the SAME real-world spot as the permit, but stored
// in the registry's own SRID, would look like. GenerateCandidates' candidateFellingsQuery then
// ST_Transforms this back to 28992 and ST_Distances it against the permit's own 28992 point, so the
// round trip must reproduce sqrt(dx^2+dy^2) metres (task 2.4's SRID-alignment pin).
func fellingPointFromRDOffset(px, py, dx, dy int) string {
	return fmt.Sprintf("ST_Transform(%s,4326)", rdPoint(px+dx, py+dy))
}

// seedPublications inserts one koop_publications row per permit in the engineered corpus (see the
// package doc comment above for the scoring arithmetic each seed value is chosen to reach).
// resolved_identificatie carries the gbd* join key (matching the corresponding felling's
// "gbdBuurtId"); resolved_buurt_code carries the DISTINCT, human-readable buurt* label — the two
// never coincide, guarding against the join regressing to resolved_buurt_code (see the seed
// identifiers doc comment above).
func seedPublications(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	rows := []string{
		// Z-STRONG-001: address-tier, resolved point at (121500,487400).
		fmt.Sprintf(`('%s','2022-01-01','%s','%s',%s,'address',false)`,
			zStrong, buurtStrong, gbdStrong, rdPoint(121500, 487400)),
		// Z-CONTEND-1-WEAK / Z-CONTEND-2-OTHER: buurt-tier, no resolved point, IDENTICAL
		// publication date (2021-01-01) so their pair-scores tie exactly on the time bucket too.
		fmt.Sprintf(`('%s','2021-01-01','%s','%s',NULL,'buurt',false)`, zWeak1, buurtContend, gbdContend),
		fmt.Sprintf(`('%s','2021-01-01','%s','%s',NULL,'buurt',false)`, zWeak2, buurtContend, gbdContend),
		// Z-CONTEST-A: address-tier, resolved point at (135000,480500).
		fmt.Sprintf(`('%s','2022-01-01','%s','%s',%s,'address',false)`,
			zContestA, buurtContest, gbdContest, rdPoint(135000, 480500)),
		// Z-CONTEST-B: buurt-tier, no resolved point, same buurt+window as A.
		fmt.Sprintf(`('%s','2022-01-01','%s','%s',NULL,'buurt',false)`, zContestB, buurtContest, gbdContest),
		// Z-MULTI-001: address-tier, resolved point at (150000,490000).
		fmt.Sprintf(`('%s','2022-01-01','%s','%s',%s,'address',false)`,
			zMulti, buurtMulti, gbdMulti, rdPoint(150000, 490000)),
		// Z-NOSOURCE-001: address-tier, resolved point at (108000,500000); buurt starts with zero
		// kapenherplant rows (seedFellings inserts none for gbdNoSource up front).
		fmt.Sprintf(`('%s','2022-01-01','%s','%s',%s,'address',false)`,
			zNoSource, buurtNoSource, gbdNoSource, rdPoint(108000, 500000)),
		// Z-CROSSBOUNDARY-001: address-tier, resolved point at (160000,495000). Its only candidate
		// felling (F-CROSSBOUNDARY-01, seeded below) carries a DIFFERENT gbdBuurtId — this permit is
		// matched ONLY via the spatial clause (D6's cross-boundary catch), never the buurt clause.
		fmt.Sprintf(`('%s','2022-01-01','%s','%s',%s,'address',false)`,
			zCrossB, buurtCrossB, gbdCrossB, rdPoint(160000, 495000)),
	}

	stmt := "INSERT INTO koop_publications (zaaknummer, available, resolved_buurt_code, resolved_identificatie, resolved_geom, resolved_tier, unresolved) VALUES\n" +
		joinRows(rows) + ";"
	_, err := pool.Exec(ctx, stmt)
	require.NoError(t, err, "seed koop_publications")
}

// seedFellings inserts every kapenherplant row the engineered corpus needs (see the package doc
// comment for how each date/offset is chosen). buurtNoSource deliberately gets NO row here — the
// permit-with-no-candidate-felling case (Requirement: Record the coverage outcome..., scenario "An
// unmatched permit is a no-source period"); insertNoSourceFelling adds one later, after run #1.
func seedFellings(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	rows := []string{
		// F-STRONG-01: felled 2022-06-01 (151-day lag from Z-STRONG-001's 2022-01-01 publication,
		// well within the <=2yr bucket), offset (18,24) from the permit's point = exactly 30.0m in
		// SRID 28992 (a scaled 3-4-5 triangle) once round-tripped through SRID 4326 storage.
		fmt.Sprintf(`('%s','BOOM-STRONG-01','%s','2022-06-01T00:00:00Z',%s,NULL)`,
			fStrong, gbdStrong, fellingPointFromRDOffset(121500, 487400, 18, 24)),

		// F-WEAK-A: felled 2023-06-01 -> an 881-day lag from BOTH Z-CONTEND-1-WEAK's and
		// Z-CONTEND-2-OTHER's identical 2021-01-01 publication date (2-3yr bucket, +0.05, not the
		// <=2yr +0.15 bucket) -- a candidate for both permits (buurtContend), giving contention=2.
		// Placement is irrelevant (both permits are point-less, so distance is never computed for
		// this pair per candidates.go's CASE WHEN $2 IS NULL branch); still given a real point for
		// realism.
		fmt.Sprintf(`('%s','BOOM-WEAK-A','%s','2023-06-01T00:00:00Z',%s,NULL)`,
			fWeak, gbdContend, fellingPointFromRDOffset(125000, 485000, 0, 0)),

		// F-CONTESTED-01: felled 2022-06-01 (151-day lag from both Z-CONTEST-A/B's 2022-01-01
		// publication), offset (10,10) ~= 14.14m from Z-CONTEST-A's point (address-tier proximate,
		// far below the addressRadiusM=50 ceiling) -- a candidate for both A (wins outright on
		// place) and B (buurt floor only).
		fmt.Sprintf(`('%s','BOOM-CONTESTED-01','%s','2022-06-01T00:00:00Z',%s,NULL)`,
			fContested, gbdContest, fellingPointFromRDOffset(135000, 480500, 10, 10)),

		// F-MULTI-01/02/03: three fellings all close to Z-MULTI-001's point and uncontested
		// (buurtMulti has no other permit), each within the <=2yr time bucket.
		fmt.Sprintf(`('%s','BOOM-MULTI-01','%s','2022-06-01T00:00:00Z',%s,NULL)`,
			fMulti1, gbdMulti, fellingPointFromRDOffset(150000, 490000, 5, 5)),
		fmt.Sprintf(`('%s','BOOM-MULTI-02','%s','2022-07-01T00:00:00Z',%s,NULL)`,
			fMulti2, gbdMulti, fellingPointFromRDOffset(150000, 490000, 10, -5)),
		fmt.Sprintf(`('%s','BOOM-MULTI-03','%s','2022-08-01T00:00:00Z',%s,NULL)`,
			fMulti3, gbdMulti, fellingPointFromRDOffset(150000, 490000, -8, 12)),

		// F-MULTI-04-FAR: SAME buurt (gbdMulti) as F-MULTI-01..03, but offset (200,150) = exactly
		// 250.0m from Z-MULTI-001's point (a scaled 4-3-5 triangle) -- beyond crossBoundaryRadiusM=200,
		// so only the buurt clause keeps it a candidate. Pins D6's fragmentation-safety property: the
		// spatial radius must never shrink the buurt net for a spread-out same-buurt project.
		fmt.Sprintf(`('%s','BOOM-MULTI-04-FAR','%s','2022-09-01T00:00:00Z',%s,NULL)`,
			fMulti4Far, gbdMulti, fellingPointFromRDOffset(150000, 490000, 200, 150)),

		// F-CROSSBOUNDARY-01: a DIFFERENT gbdBuurtId (gbdCrossBFar) than Z-CROSSBOUNDARY-001's own
		// buurt (gbdCrossB), so the buurt clause alone would exclude it. Offset (90,120) = exactly
		// 150.0m from the permit's point (a scaled 3-4-5 triangle) -- within crossBoundaryRadiusM=200,
		// so the spatial clause alone makes it a candidate (D6's additive cross-boundary catch).
		// Felled 2022-06-01: a 151-day lag from the permit's 2022-01-01 publication (<=2yr bucket).
		fmt.Sprintf(`('%s','BOOM-CROSSBOUNDARY-01','%s','2022-06-01T00:00:00Z',%s,NULL)`,
			fCrossB, gbdCrossBFar, fellingPointFromRDOffset(160000, 495000, 90, 120)),
	}

	stmt := `INSERT INTO kapenherplant (id, "boomId", "gbdBuurtId", "kapmaatregelDatumUitgevoerd", "resolvedGeom", source_deleted_at) VALUES
` + joinRows(rows) + ";"
	_, err := pool.Exec(ctx, stmt)
	require.NoError(t, err, "seed kapenherplant")
}

// insertNoSourceFelling adds the qualifying felling for Z-NOSOURCE-001's buurt AFTER run #1 has
// already recorded a no-source period for it — the "a newly appearing felling opens a matched
// period and closes the prior no-source one" scenario (Requirement: Idempotent re-derivation via
// content-keyed period nodes).
func insertNoSourceFelling(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	stmt := fmt.Sprintf(
		`INSERT INTO kapenherplant (id, "boomId", "gbdBuurtId", "kapmaatregelDatumUitgevoerd", "resolvedGeom", source_deleted_at)
VALUES ('%s','BOOM-NOSOURCE-NEW-01','%s','2022-06-01T00:00:00Z',%s,NULL)`,
		fNewNoSrc, gbdNoSource, fellingPointFromRDOffset(108000, 500000, 5, 5))
	_, err := pool.Exec(ctx, stmt)
	require.NoError(t, err, "insert the newly-appearing felling for the no-source permit")
}

// joinRows joins pre-rendered "(...)" SQL VALUES row literals with ",\n".
func joinRows(rows []string) string {
	out := rows[0]
	for _, r := range rows[1:] {
		out += ",\n" + r
	}
	return out
}

// seedInterventions loads the reference model (already done by resetFusekiRunGraphs) plus one
// gs:Intervention per permit into the graph — the source of truth GenerateCandidates enumerates
// from (design.md D10). Every Intervention satisfies gs:InterventionShape: gs:locatedAt an
// annotated (confidence 1.0) edge to a gs:Place, plus exactly one dct:available xsd:date.
func seedInterventions(t *testing.T, ctx context.Context, fusekiURL string) {
	t.Helper()

	permits := []struct {
		zaaknummer string
		buurt      string
		available  string
	}{
		{zStrong, buurtStrong, "2022-01-01"},
		{zWeak1, buurtContend, "2021-01-01"},
		{zWeak2, buurtContend, "2021-01-01"},
		{zContestA, buurtContest, "2022-01-01"},
		{zContestB, buurtContest, "2022-01-01"},
		{zMulti, buurtMulti, "2022-01-01"},
		{zNoSource, buurtNoSource, "2022-01-01"},
		{zCrossB, buurtCrossB, "2022-01-01"},
	}

	var turtle string
	turtle += "@prefix gs: <http://gemetenstad.nl/ns#> .\n"
	turtle += "@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .\n"
	turtle += "@prefix dct: <http://purl.org/dc/terms/> .\n\n"

	seenPlace := make(map[string]bool)
	for _, p := range permits {
		turtle += fmt.Sprintf("<%s> a gs:Intervention ;\n", interventionIRIFor(p.zaaknummer))
		turtle += fmt.Sprintf("    dct:available \"%s\"^^xsd:date ;\n", p.available)
		turtle += fmt.Sprintf("    gs:locatedAt <%s> {| gs:confidence 1.0 ; gs:evidence \"seed\" |} .\n\n",
			placeIRIFor(p.buurt))
		if !seenPlace[p.buurt] {
			seenPlace[p.buurt] = true
			turtle += fmt.Sprintf("<%s> a gs:Place .\n\n", placeIRIFor(p.buurt))
		}
	}

	require.NoError(t, graph.Load(ctx, fusekiURL, []byte(turtle), graph.Config{}))
}
