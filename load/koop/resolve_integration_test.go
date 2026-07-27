//go:build integration

// Integration tests for task 3.5: resolveBesluit exercised end to end against the real P6
// resolver (location.Resolve) and the real gebieden_buurten Noord-scoping lookups
// (buurtByPointSQL/buurtByRDPointSQL/buurtCodeByIdentificatie), seeded with
// testdata/koop_geo_seed.sql in an isolated internal/testdb schema — mirroring
// location/location_integration_test.go's own harness shape (this package's resolveBesluit is the
// thing under test, not location.Resolve itself, which already has its own suite).
package koop

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/location"
)

func TestResolveBesluit_TitleAddressResolvesNoordBuurtAt090(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedGeo(t, ctx, pool)

	pub := Publication{
		ID:         gmbNoordBesluit,
		Zaaknummer: zaaknummerNoord,
		Kind:       KindBesluit,
		Postcode:   "1024BB",
		Huisnummer: 8,
		Street:     "Örehof",
		Point:      &location.RDPoint{X: 122000, Y: 490000}, // coarse Gebiedsmarkering, outside every buurt
		Available:  "2023-03-01",
	}

	res, err := resolveBesluit(ctx, pool, pub)
	require.NoError(t, err)

	assert.False(t, res.Unresolved, "a title address inside a seeded BAG address must resolve")
	assert.Equal(t, noordPlaceIdentificatie, res.Identificatie)
	assert.Equal(t, "N01", res.BuurtCode)
	assert.Equal(t, 0.90, res.Confidence, "address tier confidence")
	assert.True(t, res.InNoord, "N01 code must scope to Noord")
	assert.Empty(t, res.Caveats, "an exact, valid-at-date address match carries no resolver caveat")
	assert.Equal(t, "address", res.Tier, "address-tier resolution")
	assert.Contains(t, res.Geom, "121000", "the address tier's precise BAG point is vbo-orehof-8 (121000, 487000)")
	assert.Contains(t, res.Geom, "487000")
}

func TestResolveBesluit_PointOnlyResolvesBuurtFloorAt050WithUnresolvedLocationCaveat(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedGeo(t, ctx, pool)

	// No street/postcode/huisnummer at all: the address and postcode tiers never run
	// (location.Resolve), so this can only resolve via the buurt point-in-polygon floor.
	pub := Publication{
		ID:         gmbPointFloorBesluit,
		Zaaknummer: zaaknummerPointFloor,
		Kind:       KindBesluit,
		Point:      &location.RDPoint{X: 120850, Y: 486850}, // inside N01BUURT, no seeded address there
		Available:  "2023-06-01",
	}

	res, err := resolveBesluit(ctx, pool, pub)
	require.NoError(t, err)

	assert.False(t, res.Unresolved)
	assert.Equal(t, noordPlaceIdentificatie, res.Identificatie)
	assert.Equal(t, "N01", res.BuurtCode)
	assert.Equal(t, 0.50, res.Confidence, "buurt-floor confidence")
	assert.True(t, res.InNoord)
	assert.Contains(t, res.Caveats, "unresolvedLocation")
	assert.Equal(t, "buurt", res.Tier, "buurt-floor resolution")
	assert.Empty(t, res.Geom, "the buurt floor carries no precise resolved point")
}

func TestResolveBesluit_TitleAddressResolvesOutsideNoordAt090(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedGeo(t, ctx, pool)

	pub := Publication{
		ID:         gmbOutsideNoordBesluit,
		Zaaknummer: zaaknummerOutsideNoord,
		Kind:       KindBesluit,
		Postcode:   "1077ZZ",
		Huisnummer: 3,
		Street:     "Zuidstraat",
		Point:      &location.RDPoint{X: 130050, Y: 490050},
		Available:  "2023-04-01",
	}

	res, err := resolveBesluit(ctx, pool, pub)
	require.NoError(t, err)

	assert.False(t, res.Unresolved)
	assert.Equal(t, "A01BUURT", res.Identificatie)
	assert.Equal(t, "A01", res.BuurtCode)
	assert.Equal(t, 0.90, res.Confidence)
	assert.False(t, res.InNoord, "A01 code must NOT scope to Noord")
}

func TestResolveBesluit_NoAddressAndPointOutsideEveryBuurtIsUnresolved(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedGeo(t, ctx, pool)

	pub := Publication{
		ID:         gmbUnresolvableBesluit,
		Zaaknummer: zaaknummerUnresolvable,
		Kind:       KindBesluit,
		Point:      &location.RDPoint{X: 999000, Y: 999000}, // far outside every seeded buurt polygon
		Available:  "2023-05-01",
	}

	res, err := resolveBesluit(ctx, pool, pub)
	require.NoError(t, err, "an unresolvable besluit is a normal outcome, not an error")

	assert.True(t, res.Unresolved)
	assert.Empty(t, res.Identificatie)
	assert.Empty(t, res.BuurtCode)
	assert.False(t, res.InNoord)
}

func TestResolveBesluit_PostcodeTierWithNoPointIsUnresolved(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedGeo(t, ctx, pool)

	// A postcode that exists in BAG but with no matching huisnummer resolves at the postcode
	// tier (location.Resolve), which carries no single point (Result.Geom == ""); buurtFor's
	// postcode-tier branch then falls back to the publication's own point, which is absent here
	// — so this must land in the unresolved bucket, not error out or fabricate a placement.
	pub := Publication{
		ID:         "gmb-2023-999999",
		Zaaknummer: "Z2023-N099999",
		Kind:       KindBesluit,
		Postcode:   "1024BB",
		Huisnummer: 999, // no such huisnummer seeded
		Available:  time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
	}

	res, err := resolveBesluit(ctx, pool, pub)
	require.NoError(t, err)
	assert.True(t, res.Unresolved, "postcode tier with no fallback point must be unresolved, never fabricated")
}
