//go:build integration

package graph

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// model-felled-trees task 1.5: SHACL conformance integration tests for the felled-tree/felling
// model (gs:FellingShape, gs:TreeShape) and the matched gs:Observation's felling-membership gate
// (gs:ObservationShape), run through the real Load gate against the live gs-test dataset — mirrors
// coverage_shapes_integration_test.go's shape (a shared well-formed "base" fragment, well-formed
// cases that must conform, and a table of malformed cases that must be rejected with no partial
// write), applied to openspec/changes/model-felled-trees/specs/graph-shapes/spec.md's scenarios.
//
// CRITICAL (doc.go, shacl.go's validate): the gate validates the candidate merged with
// ontology+vocab ONLY, never the live/stored graph, so every candidate below is fully
// self-contained — a felling's gs:felledTree target, and an Observation's gs:includesFelling
// target, need not correspond to a real gs:Tree/gs:Felling node written by another load (design.md
// D5's cross-load, sh:class-free gating), but a well-formed case still declares one for realism.

// -- Task 1.5, scenario "A well-formed felling conforms" ----------------------------------------

// fellingWellFormed: a gs:Felling with gs:felledTree pointing at a tree-namespace IRI and
// gs:felledOn a proper xsd:date — must conform.
const fellingWellFormed = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:felling/F-well a gs:Felling ;
    gs:felledTree data:tree/T-well ;
    gs:felledOn "2023-02-01"^^xsd:date .
data:tree/T-well a gs:Tree .
`

func TestLoadFellingWellFormedConforms(t *testing.T) {
	_, base := testClient(t)
	ctx := context.Background()
	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	assert.NoError(t, Load(ctx, base, []byte(fellingWellFormed), Config{}),
		"a gs:Felling with gs:felledTree a tree-namespace IRI and gs:felledOn an xsd:date must conform")
}

// -- Task 1.5, scenario "A felling missing its tree or date is rejected" ------------------------

// fellingMissingFelledTree: no gs:felledTree at all.
const fellingMissingFelledTree = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:felling/F-notree a gs:Felling ;
    gs:felledOn "2023-02-01"^^xsd:date .
`

// fellingFelledTreeOutOfNamespace: gs:felledTree points outside the tree/ namespace (here, place/) —
// must be rejected by the sh:pattern gate regardless of what that IRI is typed as (design.md D5).
const fellingFelledTreeOutOfNamespace = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:felling/F-outns a gs:Felling ;
    gs:felledTree data:place/OUT-1 ;
    gs:felledOn "2023-02-01"^^xsd:date .
`

// fellingMissingFelledOn: gs:felledTree present, but no gs:felledOn at all.
const fellingMissingFelledOn = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:felling/F-nodate a gs:Felling ;
    gs:felledTree data:tree/T-nodate .
data:tree/T-nodate a gs:Tree .
`

// fellingFelledOnNotDate: gs:felledOn is a plain string literal (no xsd:date datatype).
const fellingFelledOnNotDate = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:felling/F-strdate a gs:Felling ;
    gs:felledTree data:tree/T-strdate ;
    gs:felledOn "2023-02-01" .
data:tree/T-strdate a gs:Tree .
`

// fellingFelledOnWrongDatatype: gs:felledOn carries the wrong datatype (xsd:gYear, not xsd:date).
const fellingFelledOnWrongDatatype = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:felling/F-gyear a gs:Felling ;
    gs:felledTree data:tree/T-gyear ;
    gs:felledOn "2023"^^xsd:gYear .
data:tree/T-gyear a gs:Tree .
`

func TestLoadFellingRejectsMalformed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		candidate string
	}{
		{"missing gs:felledTree", fellingMissingFelledTree},
		{"gs:felledTree out of tree/ namespace", fellingFelledTreeOutOfNamespace},
		{"missing gs:felledOn", fellingMissingFelledOn},
		{"gs:felledOn not an xsd:date (plain string)", fellingFelledOnNotDate},
		{"gs:felledOn wrong datatype (xsd:gYear)", fellingFelledOnWrongDatatype},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, base := testClient(t)
			ctx := context.Background()

			require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
			before, err := c.graphsWithPrefix(ctx, runGraphPrefix)
			require.NoError(t, err)

			err = Load(ctx, base, []byte(tc.candidate), Config{})
			require.Error(t, err, "malformed felling candidate must be rejected")
			assert.Contains(t, strings.ToLower(err.Error()), "conform")

			after, err2 := c.graphsWithPrefix(ctx, runGraphPrefix)
			require.NoError(t, err2)
			assert.Len(t, after, len(before), "nothing written on non-conformance")
		})
	}
}

// -- Task 1.5, scenario "A matched Observation lists its fellings" ------------------------------

// observationWithFelling: a gs:Observation carrying one gs:includesFelling pointing at a
// felling-namespace IRI — must conform.
const observationWithFelling = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:observation/obs-well a gs:Observation ;
    gs:includesFelling data:felling/F-member .
`

func TestLoadObservationWithFellingConforms(t *testing.T) {
	_, base := testClient(t)
	ctx := context.Background()
	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	assert.NoError(t, Load(ctx, base, []byte(observationWithFelling), Config{}),
		"a gs:Observation with >=1 gs:includesFelling pointing at a felling-namespace IRI must conform")
}

// -- Task 1.5, scenario "An Observation with no fellings is rejected" ---------------------------

// observationNoFelling: no gs:includesFelling at all.
const observationNoFelling = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:observation/obs-empty a gs:Observation .
`

// observationFellingOutOfNamespace: gs:includesFelling points outside the felling/ namespace (here,
// tree/) — must be rejected by the sh:pattern gate (design.md D5), not merely the minCount-0 case
// coveredWithFelling above already exercises.
const observationFellingOutOfNamespace = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:observation/obs-outns a gs:Observation ;
    gs:includesFelling data:tree/NOT-A-FELLING .
`

func TestLoadObservationRejectsMalformed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		candidate string
	}{
		{"no gs:includesFelling at all", observationNoFelling},
		{"gs:includesFelling out of felling/ namespace", observationFellingOutOfNamespace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, base := testClient(t)
			ctx := context.Background()

			require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
			before, err := c.graphsWithPrefix(ctx, runGraphPrefix)
			require.NoError(t, err)

			err = Load(ctx, base, []byte(tc.candidate), Config{})
			require.Error(t, err, "malformed Observation candidate must be rejected")
			assert.Contains(t, strings.ToLower(err.Error()), "conform")

			after, err2 := c.graphsWithPrefix(ctx, runGraphPrefix)
			require.NoError(t, err2)
			assert.Len(t, after, len(before), "nothing written on non-conformance")
		})
	}
}

// -- Task 1.5: gs:TreeShape is permissive (identity node, no required properties) ---------------

// treeBareConforms: a bare `a gs:Tree` with no other properties — gs:TreeShape imposes none
// (graph-shapes spec.md: "gs:Tree is an identity node ... the shape imposes no required
// properties").
const treeBareConforms = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:tree/T-bare a gs:Tree .
`

func TestLoadBareTreeConforms(t *testing.T) {
	_, base := testClient(t)
	ctx := context.Background()
	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	assert.NoError(t, Load(ctx, base, []byte(treeBareConforms), Config{}),
		"gs:TreeShape is permissive: a bare gs:Tree identity node must conform")
}
