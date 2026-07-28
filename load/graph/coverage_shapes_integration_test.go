//go:build integration

package graph

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coverage-audit tasks 1.4 + 1b.4: SHACL conformance integration tests for the coverage
// anchor/period shapes (gs:AuditLinkShape, gs:CoveragePeriodShape) and the Intervention
// dct:available gate (gs:InterventionShape's (1a')), run through the real Load gate against the
// live gs-test dataset. Unlike load_integration_test.go's upsert-focused fixtures, these exist
// purely to exercise ontology/shapes.ttl's structural/range/xone constraints — conformance or
// rejection is the entire assertion; no post-load SPARQL inspection is needed.
//
// CRITICAL (doc.go, shacl.go's validate): the gate validates the candidate merged with
// ontology+vocab ONLY, never the live/stored graph — so every candidate below is fully
// self-contained. An anchor's gs:coversIntervention target, and a period's gs:versionOf anchor
// target, must both be present IN THE CANDIDATE, and any Intervention referenced must itself
// satisfy gs:InterventionShape (a confidence-1.0 annotated gs:locatedAt edge to a gs:Place, plus a
// dct:available xsd:date) — otherwise a candidate meant to isolate one violation would incidentally
// trip an unrelated one (e.g. a period pointing at a non-existent/mistyped anchor would also fail
// gs:CoveragePeriodShape's gs:versionOf sh:class gs:AuditLink check).

// coverageBaseTurtle is the shared, well-formed "base": one Intervention (with a valid locatedAt
// edge + dct:available) plus one gs:AuditLink anchor covering it. Every case below that needs a
// VALID anchor/intervention pair to isolate a period-only or anchor-only violation builds on this,
// per the task contract's "small helper that emits a valid Intervention+Place+anchor base, then
// vary the period" guidance.
//
// gs:AuditLinkShape's gs:coversIntervention property is gated sh:nodeKind sh:IRI + sh:pattern
// "^http://gemetenstad.nl/id/intervention/" (replacing an earlier, cross-load-unsatisfiable
// sh:class gs:Intervention check) — so the covered Intervention's IRI must live under
// http://gemetenstad.nl/id/intervention/, written here as a full IRI (a prefixed name would need
// the "/" escaped in Turtle's PN_LOCAL grammar, so a bare <...> IRI is clearer).
const coverageBaseTurtle = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix dct: <http://purl.org/dc/terms/> .
@prefix data: <http://gemetenstad.nl/id/> .
<http://gemetenstad.nl/id/intervention/COV-1> a gs:Intervention ;
    dct:available "2022-06-01"^^xsd:date ;
    gs:locatedAt data:place-cov {| gs:confidence 1.0 ; gs:evidence "exact BAG match" |} .
data:place-cov a gs:Place .
data:auditlink-cov a gs:AuditLink ;
    gs:coversIntervention <http://gemetenstad.nl/id/intervention/COV-1> .
`

// withCoverageBase appends a candidate-specific fragment (also carrying its own @prefix headers,
// harmless to redeclare identically in Turtle) to coverageBaseTurtle.
func withCoverageBase(fragment string) string {
	return coverageBaseTurtle + fragment
}

const coverageFragmentPrefixes = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix data: <http://gemetenstad.nl/id/> .
`

// --- Task 1.4: well-formed anchor / period cases (must conform) ---

// coverageAnchorOnly: the base with no period at all — the anchor + its covered Intervention
// alone must satisfy gs:AuditLinkShape and gs:InterventionShape.
var coverageAnchorOnly = coverageBaseTurtle

// coverageMatchedPeriod: a well-formed MATCHED gs:CoveragePeriod (linksObservation + confidence +
// granularity + evidence, no noSourceFound) versioning the base anchor. The Observation carries a
// gs:includesFelling member (model-felled-trees gs:ObservationShape, task 1.2, already merged: a
// gs:Observation now requires >=1 gs:includesFelling to a felling-namespace IRI) so this fixture,
// predating that shape, still conforms.
var coverageMatchedPeriod = withCoverageBase(coverageFragmentPrefixes + `data:obs-cov a gs:Observation ;
    gs:includesFelling data:felling/F-cov .
data:period-cov-matched a gs:CoveragePeriod ;
    gs:versionOf data:auditlink-cov ;
    gs:validFrom "2024-01-01"^^xsd:date ;
    gs:linksObservation data:obs-cov ;
    gs:confidence 0.72 ;
    gs:granularity gs:address ;
    gs:evidence "matched via address-level registry lookup" .
`)

// coverageNoSourcePeriod: a well-formed NO-SOURCE gs:CoveragePeriod (noSourceFound true, no
// linksObservation/confidence) versioning the base anchor.
var coverageNoSourcePeriod = withCoverageBase(coverageFragmentPrefixes + `data:period-cov-nosource a gs:CoveragePeriod ;
    gs:versionOf data:auditlink-cov ;
    gs:validFrom "2024-01-01"^^xsd:date ;
    gs:noSourceFound true ;
    gs:evidence "no source found after exhaustive search" .
`)

func TestLoadCoverageAnchorWellFormedConforms(t *testing.T) {
	_, base := testClient(t)
	ctx := context.Background()
	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	assert.NoError(t, Load(ctx, base, []byte(coverageAnchorOnly), Config{}),
		"a well-formed anchor covering a well-formed Intervention must conform")
}

func TestLoadCoveragePeriodMatchedConforms(t *testing.T) {
	_, base := testClient(t)
	ctx := context.Background()
	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	assert.NoError(t, Load(ctx, base, []byte(coverageMatchedPeriod), Config{}),
		"a well-formed matched CoveragePeriod (linksObservation+confidence+granularity+evidence) must conform")
}

func TestLoadCoveragePeriodNoSourceConforms(t *testing.T) {
	_, base := testClient(t)
	ctx := context.Background()
	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	assert.NoError(t, Load(ctx, base, []byte(coverageNoSourcePeriod), Config{}),
		"a well-formed no-source CoveragePeriod (noSourceFound true + evidence) must conform")
}

// --- Task 1.4: rejected anchor / period cases (must be rejected with "conform" in the error) ---

// coverageAnchorNoTarget: an AuditLink with NO gs:coversIntervention at all.
const coverageAnchorNoTarget = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:auditlink-bad a gs:AuditLink .
`

// coverageAnchorOutOfNamespaceTarget: an AuditLink whose gs:coversIntervention points at an IRI
// OUTSIDE the http://gemetenstad.nl/id/intervention/ namespace (here, the place/ namespace) — must
// be rejected by gs:AuditLinkShape's sh:pattern gate regardless of what, if anything, that IRI is
// typed as.
const coverageAnchorOutOfNamespaceTarget = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:auditlink-outns a gs:AuditLink ;
    gs:coversIntervention <http://gemetenstad.nl/id/place/OUT-1> .
`

// coveragePeriodNeitherBranch: a period with gs:linksObservation but NEITHER gs:confidence NOR
// gs:noSourceFound — satisfies neither xone branch (matched needs confidence too; no-source needs
// noSourceFound), so sh:xone must reject it even though versionOf/validFrom/evidence are present.
var coveragePeriodNeitherBranch = withCoverageBase(coverageFragmentPrefixes + `data:obs-neither a gs:Observation ;
    gs:includesFelling data:felling/F-neither .
data:period-cov-neither a gs:CoveragePeriod ;
    gs:versionOf data:auditlink-cov ;
    gs:validFrom "2024-01-01"^^xsd:date ;
    gs:linksObservation data:obs-neither ;
    gs:evidence "ambiguous outcome, neither branch" .
`)

// coveragePeriodConfidenceOutOfRange: a matched period with gs:confidence outside [0,1].
var coveragePeriodConfidenceOutOfRange = withCoverageBase(coverageFragmentPrefixes + `data:obs-oor a gs:Observation ;
    gs:includesFelling data:felling/F-oor .
data:period-cov-oor a gs:CoveragePeriod ;
    gs:versionOf data:auditlink-cov ;
    gs:validFrom "2024-01-01"^^xsd:date ;
    gs:linksObservation data:obs-oor ;
    gs:confidence 1.5 ;
    gs:granularity gs:address ;
    gs:evidence "confidence out of range" .
`)

// coveragePeriodMissingVersionOf: an otherwise well-formed no-source period with NO gs:versionOf
// anchor pointer at all.
const coveragePeriodMissingVersionOf = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:period-cov-noanchor a gs:CoveragePeriod ;
    gs:validFrom "2024-01-01"^^xsd:date ;
    gs:noSourceFound true ;
    gs:evidence "missing anchor pointer" .
`

// coveragePeriodMissingEvidence: an otherwise well-formed no-source period versioning the base
// anchor, but with NO gs:evidence.
var coveragePeriodMissingEvidence = withCoverageBase(coverageFragmentPrefixes + `data:period-cov-noevidence a gs:CoveragePeriod ;
    gs:versionOf data:auditlink-cov ;
    gs:validFrom "2024-01-01"^^xsd:date ;
    gs:noSourceFound true .
`)

func TestLoadCoverageAnchorAndPeriodRejectsMalformed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		candidate string
	}{
		{"anchor missing coversIntervention", coverageAnchorNoTarget},
		{"period matches neither xone branch", coveragePeriodNeitherBranch},
		{"period confidence out of range", coveragePeriodConfidenceOutOfRange},
		{"period missing versionOf", coveragePeriodMissingVersionOf},
		{"period missing evidence", coveragePeriodMissingEvidence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, base := testClient(t)
			ctx := context.Background()

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

// TestLoadCoverageAnchorRejectsOutOfNamespaceCoversIntervention: a dedicated test (kept separate
// from the table above for -run targetability) for the sh:pattern namespace gate on
// gs:coversIntervention — an anchor pointing outside http://gemetenstad.nl/id/intervention/ must
// be rejected, not merely one pointing at nothing (coverageAnchorNoTarget above already covers the
// minCount 0 case).
func TestLoadCoverageAnchorRejectsOutOfNamespaceCoversIntervention(t *testing.T) {
	c, base := testClient(t)
	ctx := context.Background()

	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	before, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err)

	err = Load(ctx, base, []byte(coverageAnchorOutOfNamespaceTarget), Config{})
	require.Error(t, err, "an out-of-namespace coversIntervention target must be rejected")
	assert.Contains(t, strings.ToLower(err.Error()), "conform")

	after, err2 := c.graphsWithPrefix(ctx, runGraphPrefix)
	require.NoError(t, err2)
	assert.Len(t, after, len(before), "nothing written on non-conformance")
}

// --- Task 1b.4: Intervention dct:available gate ---

// interventionAvailableWellFormed: a well-formed Intervention with a proper dct:available
// xsd:date, distinct IRIs from coverageBaseTurtle's so this test never depends on it.
const interventionAvailableWellFormed = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix dct: <http://purl.org/dc/terms/> .
@prefix data: <http://gemetenstad.nl/id/> .
data:intv-avail-ok a gs:Intervention ;
    dct:available "2023-03-15"^^xsd:date ;
    gs:locatedAt data:place-avail-ok {| gs:confidence 1.0 ; gs:evidence "exact BAG match" |} .
data:place-avail-ok a gs:Place .
`

// interventionAvailableMissing: no dct:available at all.
const interventionAvailableMissing = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
data:intv-avail-missing a gs:Intervention ;
    gs:locatedAt data:place-avail-missing {| gs:confidence 1.0 ; gs:evidence "exact BAG match" |} .
data:place-avail-missing a gs:Place .
`

// interventionAvailableNotDate: dct:available is a plain string literal (no xsd:date datatype).
const interventionAvailableNotDate = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix dct: <http://purl.org/dc/terms/> .
@prefix data: <http://gemetenstad.nl/id/> .
data:intv-avail-string a gs:Intervention ;
    dct:available "soon" ;
    gs:locatedAt data:place-avail-string {| gs:confidence 1.0 ; gs:evidence "exact BAG match" |} .
data:place-avail-string a gs:Place .
`

// interventionAvailableWrongDatatype: dct:available carries the wrong datatype (xsd:gYear, not
// xsd:date).
const interventionAvailableWrongDatatype = `@prefix gs: <http://gemetenstad.nl/ns#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix dct: <http://purl.org/dc/terms/> .
@prefix data: <http://gemetenstad.nl/id/> .
data:intv-avail-gyear a gs:Intervention ;
    dct:available "2022"^^xsd:gYear ;
    gs:locatedAt data:place-avail-gyear {| gs:confidence 1.0 ; gs:evidence "exact BAG match" |} .
data:place-avail-gyear a gs:Place .
`

func TestLoadInterventionAvailableWellFormedConforms(t *testing.T) {
	_, base := testClient(t)
	ctx := context.Background()
	require.NoError(t, Load(ctx, base, nil, Config{Reset: true}))
	assert.NoError(t, Load(ctx, base, []byte(interventionAvailableWellFormed), Config{}),
		"an Intervention with a well-formed dct:available xsd:date must conform")
}

func TestLoadInterventionAvailableRejectsMalformed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		candidate string
	}{
		{"missing dct:available", interventionAvailableMissing},
		{"dct:available not xsd:date (plain string)", interventionAvailableNotDate},
		{"dct:available wrong datatype (xsd:gYear)", interventionAvailableWrongDatatype},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, base := testClient(t)
			ctx := context.Background()

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
