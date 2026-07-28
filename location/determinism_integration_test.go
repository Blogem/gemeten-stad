//go:build integration

package location

// Regression tests for the LIMIT-1 tie-break determinism bug: location's
// resolver queries (sql.go) picked among equally-ranked candidates via a bare
// `LIMIT 1`, with no unique final ORDER BY column to break a tie. Under a
// genuine tie -- two BAG objects at the identical (postcode, huisnummer), or
// two overlapping buurt polygons both containing a query point -- that meant
// Resolve could non-deterministically return a different point/buurt across
// otherwise-identical runs (the real-world symptom: re-running `load koop`
// could resolve the same permit to a different location on unchanged data).
//
// These tests construct such a tie explicitly (testdata/tie_seed.sql, loaded
// on top of testdata/target_seed.sql) and assert Resolve always returns the
// same, smallest-identificatie candidate -- the deterministic tie-break the
// fix is expected to add. Both fixtures seed the LARGER identificatie first,
// so a plan that merely follows physical/insertion order (the shape of the
// original bug) would surface the wrong candidate; only an explicit `ORDER BY
// ... identificatie` (or equivalent) reliably picks tie-num-a / tie-buurt-a
// regardless of insertion order.

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedTie loads testdata/tie_seed.sql -- the additional tie rows -- into the
// already-seeded target tables (call after seedTargets).
func seedTie(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	sql, err := os.ReadFile("testdata/tie_seed.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(sql))
	require.NoError(t, err)
}

// TestResolve_AddressTierTieIsDeterministic exercises addressByPostcodeHuisnummerSQL's tie-break:
// tie-num-a and tie-num-b are two distinct, best-known bag_nummeraanduiding objects sharing
// postcode 1099TT / huisnummer 50, both valid at testDate. Only tie-num-a (the smaller
// identificatie) may win, on every call.
func TestResolve_AddressTierTieIsDeterministic(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedTargets(t, ctx, pool)
	seedTie(t, ctx, pool)

	for i := 0; i < 5; i++ {
		result, err := Resolve(ctx, pool, Query{Postcode: "1099TT", Huisnummer: 50, Date: testDate})
		require.NoError(t, err)
		assert.Equal(t, PlaceAddress, result.PlaceLevel)
		assert.Contains(t, result.Geom, "POINT(129100 495100)",
			"tie-num-a (smallest identificatie) must always win the tie, call #%d", i)
		assert.NotContains(t, result.Geom, "129000",
			"tie-num-b (larger identificatie) must never win, call #%d", i)
	}
}

// TestResolve_BuurtTierTieIsDeterministic exercises buurtPIPSQL's tie-break: tie-buurt-a and
// tie-buurt-b are two congruent gebieden_buurten polygons that both contain (140000, 500000).
// Only tie-buurt-a (the smaller identificatie) may win, on every call.
func TestResolve_BuurtTierTieIsDeterministic(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedTargets(t, ctx, pool)
	seedTie(t, ctx, pool)

	for i := 0; i < 5; i++ {
		result, err := Resolve(ctx, pool, Query{Point: &RDPoint{X: 140000, Y: 500000}, Date: testDate})
		require.NoError(t, err)
		assert.Equal(t, PlaceBuurt, result.PlaceLevel)
		assert.Equal(t, "tie-buurt-a", result.BuurtID,
			"tie-buurt-a (smallest identificatie) must always win the tie, call #%d", i)
	}
}
