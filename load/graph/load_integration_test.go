//go:build integration

package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/internal/testdb"
)

// These integration tests exercise the SHACL load gate AND the idempotent-SCD2-upsert write path
// (graph-writer-upsert) against the dedicated, in-memory, SHACL-enabled `gs-test` dataset
// (deploy/compose/fuseki/gs-test.ttl) — NOT the runtime `ds`, so they never touch working data
// (enforced by testdb.AssertNotProduction). GS_TEST_FUSEKI_URL is the bare Fuseki server root
// (matching GS_TEST_DATABASE_URL's convention); the test appends the dataset segment. A per-test
// isolated dataset is not possible here — the image can't add a /shacl endpoint to admin-API-created
// datasets (both mem and tdb2 return 405), and config-upload creation is disabled — so isolation is
// by the run:* graphs Load writes, dropped in t.Cleanup; tests run serially against gs-test (whose
// in-memory store is also wiped on container restart).
const testDataset = "gs-test"

const (
	// A well-formed intervention: an exact (confidence 1.0) locatedAt edge needs no caveat. Note:
	// deliberately carries NO gs:validFrom — it is an immutable-shaped fixture used for the
	// structural/provenance/SPARQL-star assertions that predate the upsert layer.
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

	// intvEvolvingIRI: the Intervention subject IRI the evolvingV1/evolvingV2 fixtures below
	// assert facts about — used by the countOpenLocatedAt helper so query strings and fixture
	// content can't drift apart silently.
	intvEvolvingIRI    = "http://gemetenstad.nl/id/intv-eo"
	validFromV1Literal = "2024-01-01"
	validFromV2Literal = "2024-06-01"

	// evolvingV1: an Intervention's locatedAt edge carrying a gs:validFrom annotation — the
	// evolving/upsert-tracked form (design.md D4: gs:validFrom presence marks an entity evolving).
	evolvingV1 = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:intv-eo a gs:Intervention ;
    gs:locatedAt data:place-eo-a {| gs:confidence 1.0 ; gs:evidence "exact BAG match" ;
                                    gs:validFrom "` + validFromV1Literal + `"^^xsd:date |} .
data:place-eo-a a gs:Place .
data:place-eo-b a gs:Place .`

	// evolvingV2: the same Intervention, re-located to a different Place with a new gs:validFrom —
	// a changed tracked field on an evolving entity (must open a new version and close the prior).
	evolvingV2 = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:intv-eo a gs:Intervention ;
    gs:locatedAt data:place-eo-b {| gs:confidence 1.0 ; gs:evidence "corrected match" ;
                                    gs:validFrom "` + validFromV2Literal + `"^^xsd:date |} .
data:place-eo-a a gs:Place .
data:place-eo-b a gs:Place .`

	// placeConflictIRI: the subject the immutablePlaceV1/immutablePlaceV2Conflict fixtures assert
	// facts about.
	placeConflictIRI = "http://gemetenstad.nl/id/place-ic"

	// immutablePlaceV1: an immutable Place skeleton (no gs:validFrom — write-once, D4).
	immutablePlaceV1 = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:place-ic a gs:Place ;
    rdfs:label "Noorderpark"@nl .`

	// immutablePlaceV2Conflict: the SAME immutable Place IRI re-asserted with DIFFERING
	// non-temporal content (still no gs:validFrom) — a data anomaly the writer must skip and
	// surface, never silently overwrite.
	immutablePlaceV2Conflict = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:place-ic a gs:Place ;
    rdfs:label "Nieuw Label"@nl .`
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

// count runs a SPARQL SELECT whose sole projection is a single `(... AS ?n)` binding and returns
// it as an int — the shared engine behind countOpenLocatedAt/countProvActivities/countRunTriples.
func (c *client) count(t *testing.T, query string) int {
	t.Helper()
	req, err := c.newRequest(context.Background(), http.MethodGet, "/sparql?query="+url.QueryEscape(query), nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/sparql-results+json")
	body, err := c.do(req, "COUNT")
	require.NoError(t, err)
	var res struct {
		Results struct {
			Bindings []struct {
				N struct {
					Value string `json:"value"`
				} `json:"n"`
			} `json:"bindings"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(body, &res))
	require.Len(t, res.Results.Bindings, 1, "count query must return exactly one row")
	n, err := strconv.Atoi(res.Results.Bindings[0].N.Value)
	require.NoError(t, err)
	return n
}

// countOpenLocatedAt returns how many currently-open (no gs:validTo) gs:locatedAt annotated
// versions exist for the Intervention IRI intv, across every named graph — used to assert the
// single-open-version invariant (design.md "Open-version ambiguity" risk) after a supersede, and
// that a rejected/no-op run left the open version count undisturbed.
func (c *client) countOpenLocatedAt(t *testing.T, intv string) int {
	t.Helper()
	query := `PREFIX gs: <http://gemetenstad.nl/ns#>
SELECT (COUNT(?place) AS ?n) WHERE {
  GRAPH ?g {
    <` + intv + `> gs:locatedAt ?place .
    << <` + intv + `> gs:locatedAt ?place >> gs:validFrom ?vf .
    FILTER NOT EXISTS { << <` + intv + `> gs:locatedAt ?place >> gs:validTo ?vt }
  }
}`
	return c.count(t, query)
}

// countProvActivities returns how many prov:Activity individuals exist in run:_provenance — used
// to assert a no-op/rejected/conflict-only run records no new provenance (design.md D5).
func (c *client) countProvActivities(t *testing.T) int {
	t.Helper()
	return c.count(t, `PREFIX prov: <http://www.w3.org/ns/prov#>
SELECT (COUNT(?a) AS ?n) WHERE { GRAPH <`+provenanceGraph+`> { ?a a prov:Activity } }`)
}

// countRunTriples returns the total triple count across every run:load-... named graph — used to
// assert a true no-op re-run adds literally nothing to the store.
func (c *client) countRunTriples(t *testing.T) int {
	t.Helper()
	return c.count(t, `SELECT (COUNT(*) AS ?n) WHERE {
  GRAPH ?g { ?s ?p ?o }
  FILTER(STRSTARTS(STR(?g), "`+runGraphPrefix+`"))
}`)
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

// 4.1: the upsert layer's first write. A first non-empty Load on a fresh graph is still the
// upsert's "new" path (every candidate entity is absent from live) — it writes exactly one
// run:load-... graph, one prov:Activity, and the annotation is SPARQL-star readable. This is the
// same observable outcome the shipped P8 primitive had for its first write; what has changed is
// what happens on a SECOND load of the same input (see TestLoadNoOpRerunLeavesNoTrace) rather than
// this first-write behaviour.
func TestLoadAcceptsWellFormedAndWritesProvenance(t *testing.T) {
	c, base := testClient(t)
	ctx := context.Background()

	before, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)

	require.NoError(t, Load(ctx, base, []byte(wellFormed), Config{Reset: true}))

	// upsert first-write: a new run:load-… graph holds the intervention.
	after, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)
	assert.Len(t, after, len(before)+1, "one new run graph on the upsert's first write")
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

// 4.2: re-running Load on the exact same candidate must be a true no-op end to end — no second
// run:load-... graph, no new prov:Activity, and no triples added anywhere under run:load-...
// (design.md D5, the "Idempotent SCD2 upsert on re-run" requirement's first scenario).
func TestLoadNoOpRerunLeavesNoTrace(t *testing.T) {
	c, base := testClient(t)
	ctx := context.Background()

	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	require.NoError(t, Load(ctx, base, []byte(evolvingV1), Config{}))

	beforeGraphs, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)
	beforeProv := c.countProvActivities(t)
	beforeTriples := c.countRunTriples(t)

	require.NoError(t, Load(ctx, base, []byte(evolvingV1), Config{}))

	afterGraphs, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)
	assert.ElementsMatch(t, beforeGraphs, afterGraphs, "no second run:load-... graph on a true no-op re-run")
	assert.Equal(t, beforeProv, c.countProvActivities(t), "no new prov:Activity on a true no-op re-run")
	assert.Equal(t, beforeTriples, c.countRunTriples(t), "no triples added on a true no-op re-run")
}

// 4.3: a changed tracked field on an evolving entity (the locatedAt edge's Place, with a new
// gs:validFrom) opens a new version and closes the prior by stamping its gs:validTo equal to the
// new version's gs:validFrom (contiguous intervals), leaving exactly one open version and never
// deleting the prior version's triples (design.md D3, the "changed tracked field" scenario).
func TestLoadChangedEvolvingEdgeOpensNewVersionAndClosesPrior(t *testing.T) {
	c, base := testClient(t)
	ctx := context.Background()

	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	require.NoError(t, Load(ctx, base, []byte(evolvingV1), Config{}))

	// 4.1: the caller-supplied gs:validFrom is present on the first-written evolving edge.
	assert.True(t, c.ask(t, `PREFIX gs: <http://gemetenstad.nl/ns#>
PREFIX xsd: <http://www.w3.org/2001/XMLSchema#>
PREFIX data: <http://gemetenstad.nl/id/>
ASK { GRAPH ?g { << data:intv-eo gs:locatedAt data:place-eo-a >> gs:validFrom "`+validFromV1Literal+`"^^xsd:date } }`),
		"first write carries the caller-supplied gs:validFrom")
	require.Equal(t, 1, c.countOpenLocatedAt(t, intvEvolvingIRI), "exactly one open version after the first load")

	require.NoError(t, Load(ctx, base, []byte(evolvingV2), Config{}))

	// the new version, carrying the new gs:validFrom, is present.
	assert.True(t, c.ask(t, `PREFIX gs: <http://gemetenstad.nl/ns#>
PREFIX xsd: <http://www.w3.org/2001/XMLSchema#>
PREFIX data: <http://gemetenstad.nl/id/>
ASK { GRAPH ?g { data:intv-eo gs:locatedAt data:place-eo-b .
                 << data:intv-eo gs:locatedAt data:place-eo-b >> gs:validFrom "`+validFromV2Literal+`"^^xsd:date } }`),
		"new version carries the candidate's new gs:validFrom")

	// the prior version is closed: gs:validTo == the new version's gs:validFrom (contiguous).
	assert.True(t, c.ask(t, `PREFIX gs: <http://gemetenstad.nl/ns#>
PREFIX xsd: <http://www.w3.org/2001/XMLSchema#>
PREFIX data: <http://gemetenstad.nl/id/>
ASK { GRAPH ?g { << data:intv-eo gs:locatedAt data:place-eo-a >> gs:validTo "`+validFromV2Literal+`"^^xsd:date } }`),
		"prior version is closed with gs:validTo equal to the new version's gs:validFrom")

	// the prior version's triples remain — history is retained, not overwritten.
	assert.True(t, c.ask(t, `PREFIX gs: <http://gemetenstad.nl/ns#>
PREFIX data: <http://gemetenstad.nl/id/>
ASK { GRAPH ?g { data:intv-eo gs:locatedAt data:place-eo-a .
                 << data:intv-eo gs:locatedAt data:place-eo-a >> gs:confidence 1.0 } }`),
		"prior version's triples/annotation remain in the store")

	// exactly one open version remains.
	assert.Equal(t, 1, c.countOpenLocatedAt(t, intvEvolvingIRI), "exactly one open version after the change")
}

// 4.5: an immutable entity (no gs:validFrom) re-asserted with differing non-temporal content is a
// data anomaly: the writer must not overwrite the stored version, must emit a warning diagnostic
// naming the subject IRI, and — since a conflict alone yields no new/changed entity — the run
// mints no run:load-... graph and records no prov:Activity (design.md D4, "immutable conflict").
func TestLoadImmutableConflictSkippedAndSurfaced(t *testing.T) {
	c, base := testClient(t)
	ctx := context.Background()

	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	require.NoError(t, Load(ctx, base, []byte(immutablePlaceV1), Config{}))

	beforeGraphs, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)
	beforeProv := c.countProvActivities(t)

	// Capture log.Printf warnings: redirect the standard logger's output for the duration of this
	// test only, restoring it via t.Cleanup so other tests are unaffected.
	prevOut := log.Writer()
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	err = Load(ctx, base, []byte(immutablePlaceV2Conflict), Config{})
	require.NoError(t, err, "a conflict-only run is a no-op write, not an error")

	assert.Contains(t, logBuf.String(), placeConflictIRI, "warning names the conflicting subject IRI")

	// the stored version is unchanged: original content retained, not overwritten.
	assert.True(t, c.ask(t, `PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX data: <http://gemetenstad.nl/id/>
ASK { GRAPH ?g { data:place-ic rdfs:label "Noorderpark"@nl } }`),
		"original immutable content is retained")
	assert.False(t, c.ask(t, `PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX data: <http://gemetenstad.nl/id/>
ASK { GRAPH ?g { data:place-ic rdfs:label "Nieuw Label"@nl } }`),
		"conflicting content is not written")

	// a conflict-only run (no new/changed entity) mints no run graph / no prov.
	afterGraphs, err2 := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err2)
	assert.ElementsMatch(t, beforeGraphs, afterGraphs, "a conflict-only run mints no run graph")
	assert.Equal(t, beforeProv, c.countProvActivities(t), "a conflict-only run records no prov:Activity")
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

// 4.4 regression: the SHACL gate must preserve the upsert's no-partial-writes guarantee. Seed an
// evolving edge, then submit a malformed candidate: the rejection must not close the seeded prior
// version, must not mint a run graph, and must not record provenance (design.md D7, "The upsert
// path preserves the SHACL gate").
func TestLoadRejectsMalformedWithNoPartialWrites(t *testing.T) {
	c, base := testClient(t)
	ctx := context.Background()

	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	require.NoError(t, Load(ctx, base, []byte(evolvingV1), Config{}))

	beforeGraphs, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)
	beforeProv := c.countProvActivities(t)

	err = Load(ctx, base, []byte(missingConfidence), Config{})
	require.Error(t, err, "malformed candidate must be rejected")
	assert.Contains(t, strings.ToLower(err.Error()), "conform")

	afterGraphs, err2 := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err2)
	assert.ElementsMatch(t, beforeGraphs, afterGraphs, "no run graph written on non-conformance")
	assert.Equal(t, beforeProv, c.countProvActivities(t), "no prov:Activity written on non-conformance")

	// the seeded evolving edge is untouched: still present, still the sole open version — no
	// gs:validTo stamped by the rejected candidate.
	assert.Equal(t, 1, c.countOpenLocatedAt(t, intvEvolvingIRI),
		"seeded evolving edge remains the sole open version — no prior closed by the rejected candidate")
	assert.False(t, c.ask(t, `PREFIX gs: <http://gemetenstad.nl/ns#>
PREFIX data: <http://gemetenstad.nl/id/>
ASK { GRAPH ?g { << data:intv-eo gs:locatedAt data:place-eo-a >> gs:validTo ?vt } }`),
		"the rejected candidate must not have closed the seeded prior version")
}
