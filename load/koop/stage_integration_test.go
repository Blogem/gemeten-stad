//go:build integration

// Integration test for stagePublications' per-row skip behavior (load-koop-assembly's stage-skip
// fix): a row that fails publicationArgs (or whose insert otherwise fails) must be skipped (log +
// continue), staging every other row in the batch — only the initial TRUNCATE is a batch-level,
// returned error. Mirrors integration_harness_test.go's newSchemaPool/seedGeo-free schema setup
// (this test never resolves a location, so it needs no geo seed).
package koop

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/location"
)

// TestStagePublications_SkipsUnstageableRowStagesTheRest lands one well-formed row and one row
// with a malformed Available date (the documented publicationArgs error trigger — see
// stage_test.go) in the same batch, and asserts stagePublications returns no error, then that the
// well-formed row alone made it through to koop_publications.
func TestStagePublications_SkipsUnstageableRowStagesTheRest(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	require.NoError(t, ensureExtensions(ctx, pool))
	schema, err := loadSchema(ctx, pool)
	require.NoError(t, err)
	require.NoError(t, ensureSchema(ctx, pool, schema))

	good := PublicationRow{
		Pub: Publication{
			ID:        "gmb-2022-good0001",
			Kind:      KindBesluit,
			Available: "2022-05-31",
			Point:     &location.RDPoint{X: 121000, Y: 487000},
		},
		Res: nil,
	}
	bad := PublicationRow{
		Pub: Publication{
			ID:        "gmb-2022-bad00001",
			Kind:      KindBesluit,
			Available: "not-a-date",
		},
		Res: nil,
	}

	err = stagePublications(ctx, pool, schema, []PublicationRow{good, bad})
	require.NoError(t, err, "a single unstageable row must not fail the whole batch — only the TRUNCATE is batch-level")

	require.NoError(t, upsertPublications(ctx, pool, schema, time.Now().UTC()))

	_, found := queryPublication(t, ctx, pool, good.Pub.ID)
	assert.True(t, found, "the well-formed row must be staged and upserted")

	_, found = queryPublication(t, ctx, pool, bad.Pub.ID)
	assert.False(t, found, "the malformed row must be skipped, never staged")
}
