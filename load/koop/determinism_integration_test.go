//go:build integration

package koop

// Regression test for the LIMIT-1 tie-break determinism bug in load/koop's OWN buurt lookups
// (buurtByPointSQL / buurtByRDPointSQL in resolve.go) — the load/koop-side counterpart to
// location/determinism_integration_test.go's tests of location/sql.go's buurtPIPSQL. Two congruent
// gebieden_buurten polygons both contain the same point; only the smaller-identificatie buurt may
// deterministically win, across every call, via lookupBuurtByEWKT (the address-tier path) and
// lookupBuurtByRDPoint (the postcode-tier fallback path) respectively.
//
// This seeds ONLY a minimal gebieden_buurten table (not the full BAG schema
// testdata/koop_geo_seed.sql provides), since lookupBuurtByEWKT/lookupBuurtByRDPoint never touch
// the BAG tables — keeping the tie fixture self-contained and independent of the shared corpus
// fixture other tests in this package rely on.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/location"
)

// buurtTieDDL creates gebieden_buurten with two congruent polygons (tie-buurt-a, tie-buurt-b) both
// containing (140000, 500000), inserted in DESCENDING identificatie order (tie-buurt-b before
// tie-buurt-a) — the same insertion-order-bias reasoning as location/testdata/tie_seed.sql: a plan
// that merely follows physical/insertion order (the shape of the original bug) would surface the
// wrong (larger-identificatie) candidate.
const buurtTieDDL = `
CREATE TABLE gebieden_buurten (
    identificatie      text PRIMARY KEY,
    naam               text,
    code               text,
    ligtinwijkid       text,
    geom               geometry(Geometry, 28992),
    source_deleted_at  timestamptz
);

INSERT INTO gebieden_buurten (identificatie, naam, code, ligtinwijkid, geom)
VALUES
    ('tie-buurt-b', 'Tie Buurt B', 'T02', NULL,
     ST_GeomFromText('POLYGON((139900 499900, 140100 499900, 140100 500100, 139900 500100, 139900 499900))', 28992)),
    ('tie-buurt-a', 'Tie Buurt A', 'T01', NULL,
     ST_GeomFromText('POLYGON((139900 499900, 140100 499900, 140100 500100, 139900 500100, 139900 499900))', 28992));
`

func seedBuurtTie(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, buurtTieDDL)
	require.NoError(t, err)
}

// TestLookupBuurtByEWKT_TieIsDeterministic exercises buurtByPointSQL's tie-break (the address-tier
// buurt lookup path in buurtFor).
func TestLookupBuurtByEWKT_TieIsDeterministic(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedBuurtTie(t, ctx, pool)

	for i := 0; i < 5; i++ {
		identificatie, code, err := lookupBuurtByEWKT(ctx, pool, "SRID=28992;POINT(140000 500000)")
		require.NoError(t, err)
		assert.Equal(t, "tie-buurt-a", identificatie,
			"tie-buurt-a (smallest identificatie) must always win the tie, call #%d", i)
		assert.Equal(t, "T01", code, "call #%d", i)
	}
}

// TestLookupBuurtByRDPoint_TieIsDeterministic exercises buurtByRDPointSQL's tie-break (the
// postcode-tier fallback buurt lookup path in buurtFor).
func TestLookupBuurtByRDPoint_TieIsDeterministic(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedBuurtTie(t, ctx, pool)

	for i := 0; i < 5; i++ {
		identificatie, code, err := lookupBuurtByRDPoint(ctx, pool, location.RDPoint{X: 140000, Y: 500000})
		require.NoError(t, err)
		assert.Equal(t, "tie-buurt-a", identificatie,
			"tie-buurt-a (smallest identificatie) must always win the tie, call #%d", i)
		assert.Equal(t, "T01", code, "call #%d", i)
	}
}
