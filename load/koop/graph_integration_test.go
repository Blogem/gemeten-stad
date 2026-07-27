//go:build integration

// Integration tests for task 5.2: the P12 graph gate, exercised through koop.Load (the conforming
// write, the immutableConflict skip) and directly through load/graph.Load (the malformed-candidate
// rejection, which needs a candidate koop.Load itself can never build — see the task contract).
// All three target the shared gs-test Fuseki dataset (see integration_harness_test.go's package
// doc comment for why a per-test dataset isn't possible here).
package koop

import (
	"bytes"
	"context"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
	"github.com/Blogem/gemeten-stad/load/graph"
)

// missingConfidenceCandidate is a hand-built, deliberately malformed candidate: a gs:locatedAt
// edge with no gs:confidence annotation (InterventionShape clause 1b, ontology/shapes.ttl) --
// koop.Load can never build this itself (graph.go's renderLocatedAtAnnotations always emits
// gs:confidence unconditionally), so the rejection is exercised directly against load/graph.Load,
// per the task contract.
const missingConfidenceCandidate = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:koop-intv-malformed a gs:Intervention ; gs:locatedAt data:koop-place-malformed .
data:koop-place-malformed a gs:Place .`

// p12bPlaceCandidate mirrors load/places' P12b skeleton shape for the Noord buurt
// (koop_geo_seed.sql's N01BUURT): label + gs:within, both un-annotated (no gs:validFrom on either
// triple) -- an immutable Place, per design.md D4. place: is bound directly to placeNS (a "/" is
// not legal inside a Turtle prefixed-name local part -- load/places/render.go's own reason for a
// dedicated place: prefix, mirrored here).
const p12bPlaceCandidate = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
@prefix place: <http://gemetenstad.nl/id/place/> .
place:N01BUURT a gs:Place ;
    rdfs:label "Testbuurt Noord"@nl ;
    gs:within place:N01WIJK .`

const (
	noordPlaceIRI        = "http://gemetenstad.nl/id/place/N01BUURT"
	noordWijkIRI         = "http://gemetenstad.nl/id/place/N01WIJK"
	noordInterventionIRI = "http://gemetenstad.nl/id/intervention/" + zaaknummerNoord
)

// TestKoopGraphGate_MalformedLocatedAtRejectedNoPartialWrite: 5.2's malformed-candidate scenario.
// A locatedAt edge missing gs:confidence is rejected by the SHACL gate, and the rejection leaves
// no partial write: no new run:load-... graph.
func TestKoopGraphGate_MalformedLocatedAtRejectedNoPartialWrite(t *testing.T) {
	dsURL := fusekiDatasetURL(t)
	ctx := context.Background()

	resetFusekiRunGraphs(t, ctx, dsURL)
	before := sparqlGraphsWithPrefix(t, dsURL, gsRunGraphPrefix)

	err := graph.Load(ctx, dsURL, []byte(missingConfidenceCandidate), graph.Config{})
	require.Error(t, err, "a locatedAt edge with no gs:confidence must be rejected")

	after := sparqlGraphsWithPrefix(t, dsURL, gsRunGraphPrefix)
	assert.ElementsMatch(t, before, after, "no run graph written on non-conformance")
}

// TestKoopLoad_ConformingCorpusWritesNoordIntervention: 5.2's happy path. A conforming Noord
// besluit is written through koop.Load's own graph.Load call: the Intervention exists, typed, with
// a locatedAt edge whose target is exactly the Noord buurt Place IRI (matches P12b's IRI scheme,
// D6).
func TestKoopLoad_ConformingCorpusWritesNoordIntervention(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	dsURL := fusekiDatasetURL(t)
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedGeo(t, ctx, pool)
	store := shared.NewRawStore(t.TempDir())
	landNoordZaak(t, store)

	resetFusekiRunGraphs(t, ctx, dsURL)

	require.NoError(t, Load(ctx, pool, store, dsURL, Config{Reset: true}))

	assert.True(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <`+noordInterventionIRI+`> a gs:Intervention ;
                 gs:locatedAt <`+noordPlaceIRI+`> } }`),
		"the Noord besluit's Intervention must be written with a locatedAt edge to the Noord Place")

	assert.True(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { << <`+noordInterventionIRI+`> gs:locatedAt <`+noordPlaceIRI+`> >> gs:confidence 0.9 } }`),
		"the locatedAt edge must carry the address-tier confidence (0.90)")
}

// TestKoopGraphGate_ImmutableConflictSkipsBarePlaceReassertion: 5.2's immutableConflict scenario.
// Pre-seed a P12b-style Place (label + gs:within, no gs:validFrom -- immutable), then run
// koop.Load: its own bare `<place> a gs:Place` re-assertion (graph.go's renderAuditedBesluit, D5)
// must classify as immutableConflict against the richer live Place -- skipped, not overwritten,
// logged -- while the Intervention itself (genuinely new) is still written.
func TestKoopGraphGate_ImmutableConflictSkipsBarePlaceReassertion(t *testing.T) {
	dsn := requireEnv(t, "GS_TEST_DATABASE_URL")
	dsURL := fusekiDatasetURL(t)
	ctx := context.Background()

	pool := newSchemaPool(t, ctx, dsn)
	seedGeo(t, ctx, pool)
	store := shared.NewRawStore(t.TempDir())
	landNoordZaak(t, store)

	resetFusekiRunGraphs(t, ctx, dsURL)
	require.NoError(t, graph.Load(ctx, dsURL, []byte(p12bPlaceCandidate), graph.Config{}))

	// Capture log.Printf warnings the same way load/graph/load_integration_test.go's own
	// immutableConflict test does, restoring the standard logger on cleanup.
	prevOut := log.Writer()
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	// cfg.Reset is deliberately false here: Reset drops prior run:load-... graphs, which would
	// remove the P12b Place just seeded above -- the opposite of what this test needs to prove.
	err := Load(ctx, pool, store, dsURL, Config{Reset: false})
	require.NoError(t, err, "an immutableConflict-only Place must not fail the whole load")

	assert.Contains(t, logBuf.String(), noordPlaceIRI, "warning names the conflicting Place IRI")

	assert.True(t, sparqlAsk(t, dsURL, `PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
ASK { GRAPH ?g { <`+noordPlaceIRI+`> rdfs:label "Testbuurt Noord"@nl } }`),
		"the pre-seeded Place's label must be retained, not overwritten")
	assert.True(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <`+noordPlaceIRI+`> gs:within <`+noordWijkIRI+`> } }`),
		"the pre-seeded Place's gs:within must be retained, not overwritten")

	assert.True(t, sparqlAsk(t, dsURL, `PREFIX gs: <http://gemetenstad.nl/ns#>
ASK { GRAPH ?g { <`+noordInterventionIRI+`> a gs:Intervention ; gs:locatedAt <`+noordPlaceIRI+`> } }`),
		"the Intervention itself, being genuinely new, must still be written despite the Place conflict")
}
