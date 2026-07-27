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

// coverage-audit: the belt-and-suspenders anchor gate. TestLoadRejectsSPARQLInjectionIRI above
// covers a malicious SUBJECT IRI, caught by upsert's early stagingSigs/liveSigs loop. A node-form
// period's gs:versionOf ANCHOR is a *different* IRI, read back by a separate query
// (stagingPeriodAnchors) and validated by its own, later assertSafeIRI call in upsert (write.go:
// "it gets its own assertSafeIRI check right where it is read, further down") — precisely because
// it is not a key of either signature map and so is NOT covered by the earlier subject-IRI loop.
// injAnchorCandidate's SUBJECT (data:period-inj-anchor-victim) is an ordinary, safe IRI; only the
// gs:versionOf OBJECT carries the same confirmed Turtle UCHAR injection payload as injCandidate
// (decoding to a raw '>' plus a chained ';DROP ALL;'). This proves the anchor-specific gate holds
// end to end, independently of the subject gate.
const injAnchorCandidate = "@prefix gs: <http://gemetenstad.nl/ns#> .\n" +
	"@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .\n" +
	"@prefix data: <http://gemetenstad.nl/id/> .\n" +
	"data:period-inj-anchor-victim gs:versionOf " +
	"<http://gemetenstad.nl/id/anchor-inj" + "\\u003E\\u0020\\u007D\\u0020\\u007D;DROP\\u0020ALL;#>" +
	" ;\n    gs:validFrom \"2024-01-01\"^^xsd:date .\n"

func TestLoadRejectsSPARQLInjectionAnchorIRI(t *testing.T) {
	c, base := testClient(t)
	ctx := context.Background()

	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	beforeGraphs, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)
	beforeProv := c.countProvActivities(t)

	err = Load(ctx, base, []byte(injAnchorCandidate), Config{})
	require.Error(t, err, "a period candidate whose gs:versionOf anchor carries an injection payload must be rejected")
	assert.Contains(t, strings.ToLower(err.Error()), "unsafe", "error names the unsafe anchor IRI")

	afterGraphs, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)
	assert.ElementsMatch(t, beforeGraphs, afterGraphs, "no new run:load-... graph written on rejection")
	assert.Equal(t, beforeProv, c.countProvActivities(t), "no prov:Activity recorded on rejection")
	assert.True(t, c.ask(t, `ASK { GRAPH <`+referenceModelGraph+`> { ?s ?p ?o } }`),
		"reference model intact — no chained DROP ALL executed via the anchor")
	assert.False(t, c.ask(t, `PREFIX gs: <http://gemetenstad.nl/ns#>
PREFIX data: <http://gemetenstad.nl/id/>
ASK { GRAPH ?g { data:period-inj-anchor-victim gs:versionOf ?a } }`),
		"the victim period was not written to any graph — nothing written")
}
