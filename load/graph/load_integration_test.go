//go:build integration

package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/internal/testdb"
)

// These integration tests exercise the SHACL load gate against the dedicated, in-memory,
// SHACL-enabled `gs-test` dataset (deploy/compose/fuseki/gs-test.ttl) — NOT the runtime `ds`, so
// they never touch working data (enforced by testdb.AssertNotProduction). GS_TEST_FUSEKI_URL is
// the bare Fuseki server root (matching GS_TEST_DATABASE_URL's convention); the test appends the
// dataset segment. A per-test isolated dataset is not possible here — the image can't add a
// /shacl endpoint to admin-API-created datasets (both mem and tdb2 return 405), and config-upload
// creation is disabled — so isolation is by the run:* graphs Load writes, dropped in t.Cleanup;
// tests run serially against gs-test (whose in-memory store is also wiped on container restart).
const testDataset = "gs-test"

const (
	// A well-formed intervention: an exact (confidence 1.0) locatedAt edge needs no caveat.
	wellFormed = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:intv-it a gs:Intervention ;
    gs:locatedAt data:place-it {| gs:confidence 1.0 ; gs:evidence "exact BAG match" |} .
data:place-it a gs:Place .`

	// Missing structure: an Intervention with no locatedAt edge.
	missingStructure = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:intv-it a gs:Intervention .`

	// Missing confidence: a locatedAt edge written as if exact (no annotation).
	missingConfidence = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:intv-it a gs:Intervention ; gs:locatedAt data:place-it .
data:place-it a gs:Place .`

	// Out-of-vocab activity: references a concept not in the tree-audit scheme.
	outOfVocab = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:obs-it gs:activity data:not-a-real-concept .`

	// #3503 mixed: a valid RDF-star edge (data:good) alongside a real structural violation
	// (data:bad has no confidence annotation). Must still report non-conforming.
	mixed3503 = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:good a gs:Intervention ; gs:locatedAt data:pg {| gs:confidence 1.0 ; gs:evidence "ok" |} .
data:pg a gs:Place .
data:bad a gs:Intervention ; gs:locatedAt data:pb .
data:pb a gs:Place .`
)

// testClient returns a client and the dataset URL for the gs-test dataset. GS_TEST_FUSEKI_URL is
// the bare server root; the dataset segment is appended here and guarded against production names.
func testClient(t *testing.T) (*client, string) {
	t.Helper()
	root := os.Getenv("GS_TEST_FUSEKI_URL")
	require.NotEmptyf(t, root, "GS_TEST_FUSEKI_URL must be set to run graph integration tests")
	require.NotEmptyf(t, os.Getenv("FUSEKI_ADMIN_PASSWORD"), "FUSEKI_ADMIN_PASSWORD must be set")
	require.NoError(t, testdb.AssertNotProduction("", testDataset), "must not target a production dataset")
	dsURL := strings.TrimRight(root, "/") + "/" + testDataset
	c, err := newClient(dsURL, os.Getenv)
	require.NoError(t, err)
	// Clean slate + teardown: drop every run:* graph this suite touches.
	dropRunGraphs(t, c)
	t.Cleanup(func() { dropRunGraphs(t, c) })
	return c, dsURL
}

func dropRunGraphs(t *testing.T, c *client) {
	t.Helper()
	ctx := context.Background()
	for _, g := range []string{referenceModelGraph, provenanceGraph} {
		assert.NoError(t, c.dropGraph(ctx, g))
	}
	runs, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)
	for _, g := range runs {
		assert.NoError(t, c.dropGraph(ctx, g))
	}
}

// ask runs a SPARQL ASK against the whole dataset and returns the boolean result.
func (c *client) ask(t *testing.T, query string) bool {
	t.Helper()
	req, err := c.newRequest(context.Background(), http.MethodGet, "/sparql?query="+url.QueryEscape(query), nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/sparql-results+json")
	body, err := c.do(req, "ASK")
	require.NoError(t, err)
	var res struct {
		Boolean bool `json:"boolean"`
	}
	require.NoError(t, json.Unmarshal(body, &res))
	return res.Boolean
}

func TestLoadReferenceModelAndVocab(t *testing.T) {
	c, base := testClient(t)
	ctx := context.Background()

	// 5.1: the reference model (ontology + vocab) loads without error.
	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))

	// 5.2: vocab assertions via SPARQL. Queries name the graph explicitly (the reference model
	// lands in run:_model) rather than relying on a union default graph.
	gs := "PREFIX skos: <http://www.w3.org/2004/02/skos/core#>\nPREFIX act: <http://gemetenstad.nl/id/activity/>\nPREFIX sp: <http://gemetenstad.nl/id/species/>\n"
	assert.True(t, c.ask(t, gs+`ASK { GRAPH ?g { act:vellen skos:altLabel "verplanten"@nl , "kappen"@nl , "rooien"@nl } }`),
		"felling concept carries verplanten/kappen/rooien altLabels")
	assert.False(t, c.ask(t, gs+`ASK { GRAPH ?g { ?c skos:prefLabel "verplanten"@nl } }`),
		"no separate Verplanten concept exists")
	assert.True(t, c.ask(t, gs+`ASK { GRAPH ?g { sp:ulmus skos:prefLabel "iep"@nl ; skos:altLabel "iepen"@nl , "Ulmus" } }`),
		"species iep carries plural + Latin genus")
	// a plural surface form resolves to the same concept as its singular
	assert.True(t, c.ask(t, gs+`ASK { GRAPH ?g { ?c skos:prefLabel "es"@nl ; skos:altLabel "essen"@nl } }`),
		"essen resolves to the same concept as es")
	assert.False(t, c.ask(t, gs+`ASK { GRAPH ?g { ?p a <http://gemetenstad.nl/id/place/> } }`),
		"no place concepts seeded")
}

func TestLoadAcceptsWellFormedAndWritesProvenance(t *testing.T) {
	c, base := testClient(t)
	ctx := context.Background()

	before, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)

	require.NoError(t, Load(ctx, base, []byte(wellFormed), Config{Reset: true}))

	// 5.4 accept: a new run:load-… graph holds the intervention.
	after, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)
	assert.Len(t, after, len(before)+1, "one new run graph")
	assert.True(t, c.ask(t, `ASK { GRAPH ?g { <http://gemetenstad.nl/id/intv-it> a <http://gemetenstad.nl/ns#Intervention> } }`),
		"intervention written to a run graph")

	// provenance: a prov:Activity with generatedAtTime in run:_provenance.
	assert.True(t, c.ask(t, `PREFIX prov: <http://www.w3.org/ns/prov#>
ASK { GRAPH <`+provenanceGraph+`> { ?run a prov:Activity ; prov:generatedAtTime ?t } }`),
		"run provenance recorded")

	// 5.6: SPARQL-star read of the confidence/evidence annotation off the written edge.
	assert.True(t, c.ask(t, `PREFIX gs: <http://gemetenstad.nl/ns#>
PREFIX data: <http://gemetenstad.nl/id/>
ASK { GRAPH ?g { << data:intv-it gs:locatedAt data:place-it >> gs:confidence 1.0 ;
                                                               gs:evidence ?e } }`),
		"confidence + evidence readable via SPARQL-star")
}

func TestLoadRejectsMalformed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		candidate string
	}{
		{"missing structure", missingStructure},
		{"missing confidence", missingConfidence},
		{"out of vocab", outOfVocab},
		{"mixed #3503", mixed3503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, base := testClient(t)
			ctx := context.Background()

			// seed reference model first so controlled-value checks have the vocab
			require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
			before, err := c.graphsWithPrefix(ctx, runGraphPrefix)
			require.NoError(t, err)

			err = Load(ctx, base, []byte(tc.candidate), Config{})
			require.Error(t, err, "malformed candidate must be rejected")
			assert.Contains(t, strings.ToLower(err.Error()), "conform")

			after, err2 := c.graphsWithPrefix(ctx, runGraphPrefix)
			require.NoError(t, err2)
			assert.Len(t, after, len(before), "nothing written on non-conformance")
		})
	}
}
