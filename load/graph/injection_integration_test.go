//go:build integration

package graph

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Confirmed injection payload: a subject IRI whose Turtle UCHAR escapes decode to a raw '>' plus a
// chained ';DROP ALL;'. It PARSES (Jena accepts UCHAR escapes) and reaches the assertSafeIRI gate,
// which must reject it with zero store mutation.
const injCandidate = "@prefix gs: <http://gemetenstad.nl/ns#> .\n" +
	`<http://gemetenstad.nl/id/tree1\u003E\u0020\u007D\u0020\u007D;DROP\u0020ALL;#> a gs:Place .`

func TestLoadRejectsSPARQLInjectionIRI(t *testing.T) {
	c, base := testClient(t)
	ctx := context.Background()

	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	beforeGraphs, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)

	err = Load(ctx, base, []byte(injCandidate), Config{})
	require.Error(t, err, "injection candidate must be rejected")
	assert.Contains(t, strings.ToLower(err.Error()), "unsafe", "error names the unsafe IRI")

	afterGraphs, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)
	assert.ElementsMatch(t, beforeGraphs, afterGraphs, "no run graph written on rejection")
	assert.True(t, c.ask(t, `ASK { GRAPH <`+referenceModelGraph+`> { ?s ?p ?o } }`),
		"reference model intact — no chained DROP ALL executed")
}
