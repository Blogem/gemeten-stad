//go:build integration

package graph

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStagingPeriodAnchorsLargeValuesListDoesNotOverflowURI is a regression test for a production
// 414 hit by the model-felled-trees bomen->graph projection: its SCD2 series-close step builds a
// stagingPeriodAnchors query with a `VALUES ?s { <iri1> <iri2> ... }` list over ~16k subject
// IRIs, and every SPARQL SELECT used to be sent via GET with the entire query embedded in the
// request URI ("graph: read staging period anchors: status 414: URI Too Long"). Now that SELECTs
// go via POST (selectQuery in client.go), a VALUES list far larger than any URI-length limit must
// still succeed.
func TestStagingPeriodAnchorsLargeValuesListDoesNotOverflowURI(t *testing.T) {
	c, _ := testClient(t)
	ctx := context.Background()

	const n = 5000
	ids := make([]iri, n)
	for i := range ids {
		ids[i] = iri(fmt.Sprintf("http://gemetenstad.nl/id/felled-synthetic-%06d", i))
	}

	// Sanity check on the fixture itself: the rendered VALUES list must comfortably exceed
	// Fuseki/Jetty's default request-URI limit (a few KB) — otherwise this test would pass
	// trivially even against the old, buggy GET-in-URI code path.
	const uriLimitMargin = 8 * 1024
	require.Greater(t, len(iriValuesList(ids)), uriLimitMargin,
		"fixture VALUES list must be larger than a typical URI length limit to reproduce the 414")

	// No staging graph is seeded for this runID: the point is only that the request itself
	// succeeds (200, valid SPARQL JSON results) rather than 414ing on the URI. An empty result
	// map is the correct, valid answer for a runID with no staged candidate.
	anchors, err := c.stagingPeriodAnchors(ctx, "regression-large-values", ids)
	require.NoError(t, err)
	assert.Empty(t, anchors)
}
