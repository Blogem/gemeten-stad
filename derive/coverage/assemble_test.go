package coverage

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -- Matched period -----------------------------------------------------------
//
// "Requirement: Record the coverage outcome as an anchor plus a versioned
// period", scenario "A strong match is a matched period on the permit's
// anchor": the anchor (write-once) plus a gs:CoveragePeriod carrying
// gs:linksObservation, gs:confidence, and gs:granularity.

func TestAssemble_MatchedPeriod(t *testing.T) {
	item := Assembled{
		Zaaknummer:      "Z1",
		InterventionIRI: "http://gemetenstad.nl/id/intervention/Z1",
		Outcome: Outcome{
			Matched:        true,
			ObservationIRI: "http://gemetenstad.nl/id/observation/Z1",
			FellingIRIs:    []string{"http://gemetenstad.nl/id/felling/GMB-2022-1"},
			Confidence:     0.70,
			Granularity:    TierAddress,
			Caveats:        []string{CaveatCountUnknown},
		},
		ValidFrom:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Evidence:    "matched 1 felling in buurt BU0363 within [2022-01-01,2025-01-01]",
		DerivedFrom: []string{"http://gemetenstad.nl/id/intervention/Z1", "http://gemetenstad.nl/id/kapenherplant/GMB-2022-1"},
	}

	got, err := Assemble([]Assembled{item})
	require.NoError(t, err)
	turtle := string(got)

	key := ContentKey(item.Outcome)

	// Anchor: write-once, gs:coversIntervention the Intervention.
	assert.Contains(t, turtle, "<http://gemetenstad.nl/id/auditlink/Z1> a gs:AuditLink")
	assert.Contains(t, turtle, "gs:coversIntervention <http://gemetenstad.nl/id/intervention/Z1>")

	// Observation minted (identity only) because the period is matched.
	assert.Contains(t, turtle, "<http://gemetenstad.nl/id/observation/Z1> a gs:Observation")

	// Period node: content-keyed IRI under the anchor, gs:versionOf it.
	assert.Contains(t, turtle, fmt.Sprintf("<http://gemetenstad.nl/id/auditlink/Z1/%s> a gs:CoveragePeriod", key),
		"the period IRI must be content-keyed under the anchor (auditlink/<zaaknummer>/<content-key>)")
	assert.Contains(t, turtle, "gs:versionOf <http://gemetenstad.nl/id/auditlink/Z1>")

	// gs:validFrom is an xsd:date literal.
	assert.Contains(t, turtle, `gs:validFrom "2026-01-15"^^xsd:date`)

	// Matched branch: linksObservation + confidence + granularity.
	assert.Contains(t, turtle, "gs:linksObservation <http://gemetenstad.nl/id/observation/Z1>")
	assert.Regexp(t, `gs:confidence 0\.70($|[^0-9])`, turtle,
		"confidence must be forced to 2dp: 0.70, not 0.7")
	assert.NotContains(t, turtle, "gs:confidence 0.7 ", "must not emit the un-padded 1dp form")
	assert.Contains(t, turtle, "gs:granularity gs:address")
	assert.Contains(t, turtle, "gs:caveat gs:countUnknown")

	// Evidence + provenance always present.
	assert.Contains(t, turtle, `gs:evidence "matched 1 felling in buurt BU0363 within [2022-01-01,2025-01-01]"`)
	assert.Contains(t, turtle, "prov:wasDerivedFrom")
	assert.Contains(t, turtle, "<http://gemetenstad.nl/id/kapenherplant/GMB-2022-1>")

	// A matched period is never a no-source period, and Assemble (pure
	// turtle assembly) never stamps a close - that is the writer's job.
	assert.NotContains(t, turtle, "gs:noSourceFound")
	assert.NotContains(t, turtle, "gs:validTo")
}

// -- Weak-link matched period --------------------------------------------------
//
// Scenario "A below-τ match is a matched period with a weakLink caveat": still
// the matched branch (Observation + confidence + granularity present), plus
// gs:caveat gs:weakLink.

func TestAssemble_WeakLinkMatchedPeriod(t *testing.T) {
	item := Assembled{
		Zaaknummer:      "Z2",
		InterventionIRI: "http://gemetenstad.nl/id/intervention/Z2",
		Outcome: Outcome{
			Matched:        true,
			ObservationIRI: "http://gemetenstad.nl/id/observation/Z2",
			FellingIRIs:    []string{"http://gemetenstad.nl/id/felling/GMB-2022-2"},
			Confidence:     0.43,
			Granularity:    TierBuurt,
			Caveats:        []string{CaveatCountUnknown, CaveatWeakLink},
		},
		ValidFrom:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Evidence:    "matched 1 felling in buurt BU0363, contested, below tau",
		DerivedFrom: []string{"http://gemetenstad.nl/id/intervention/Z2"},
	}

	got, err := Assemble([]Assembled{item})
	require.NoError(t, err)
	turtle := string(got)

	// Still the matched branch: Observation + confidence + granularity present.
	assert.Contains(t, turtle, "<http://gemetenstad.nl/id/observation/Z2> a gs:Observation")
	assert.Contains(t, turtle, "gs:linksObservation <http://gemetenstad.nl/id/observation/Z2>")
	assert.Regexp(t, `gs:confidence 0\.43($|[^0-9])`, turtle)
	assert.Contains(t, turtle, "gs:granularity gs:buurt")

	// Plus the weakLink caveat for the below-tau score. Both caveats may be
	// rendered as a single idiomatic Turtle predicate-object list
	// (`gs:caveat gs:countUnknown, gs:weakLink`), which expands to the same
	// two triples as two separate `gs:caveat` statements — so assert the
	// predicate appears and that both caveat objects are present, rather
	// than pinning a `gs:caveat <one-value>` substring per caveat.
	assert.Contains(t, turtle, "gs:caveat ")
	assert.Contains(t, turtle, "gs:weakLink")
	assert.Contains(t, turtle, "gs:countUnknown")

	assert.NotContains(t, turtle, "gs:noSourceFound")
}

// -- No-source period -----------------------------------------------------------
//
// Scenario "An unmatched permit is a no-source period": gs:noSourceFound true
// and gs:evidence naming the searched buurt+window; no Observation minted and
// none of the matched-branch predicates appear.

func TestAssemble_NoSourcePeriod(t *testing.T) {
	item := Assembled{
		Zaaknummer:      "Z3",
		InterventionIRI: "http://gemetenstad.nl/id/intervention/Z3",
		Outcome: Outcome{
			Matched: false,
		},
		ValidFrom:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Evidence:    "no felling found in buurt BU0363 within [2022-01-01,2025-01-01]",
		DerivedFrom: []string{"http://gemetenstad.nl/id/intervention/Z3"},
	}

	got, err := Assemble([]Assembled{item})
	require.NoError(t, err)
	turtle := string(got)

	key := ContentKey(item.Outcome)

	// Anchor still present.
	assert.Contains(t, turtle, "<http://gemetenstad.nl/id/auditlink/Z3> a gs:AuditLink")
	assert.Contains(t, turtle, fmt.Sprintf("<http://gemetenstad.nl/id/auditlink/Z3/%s> a gs:CoveragePeriod", key))

	// No-source branch.
	assert.Contains(t, turtle, "gs:noSourceFound true")
	assert.Contains(t, turtle, `gs:evidence "no felling found in buurt BU0363 within [2022-01-01,2025-01-01]"`)

	// None of the matched-branch predicates are emitted for this permit.
	assert.NotContains(t, turtle, "gs:Observation")
	assert.NotContains(t, turtle, "gs:linksObservation")
	assert.NotContains(t, turtle, "gs:confidence")
	assert.NotContains(t, turtle, "gs:granularity")
	assert.NotContains(t, turtle, "gs:validTo")
}

// -- Two items in one call: both anchors + both periods, one prefix header ---

func TestAssemble_TwoItemsBothAppearUnderOnePreamble(t *testing.T) {
	matched := Assembled{
		Zaaknummer:      "Z4",
		InterventionIRI: "http://gemetenstad.nl/id/intervention/Z4",
		Outcome: Outcome{
			Matched:        true,
			ObservationIRI: "http://gemetenstad.nl/id/observation/Z4",
			FellingIRIs:    []string{"http://gemetenstad.nl/id/felling/GMB-2022-4"},
			Confidence:     0.85,
			Granularity:    TierAddress,
		},
		ValidFrom:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Evidence:    "matched felling",
		DerivedFrom: []string{"http://gemetenstad.nl/id/intervention/Z4"},
	}
	noSource := Assembled{
		Zaaknummer:      "Z5",
		InterventionIRI: "http://gemetenstad.nl/id/intervention/Z5",
		Outcome: Outcome{
			Matched: false,
		},
		ValidFrom:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Evidence:    "no felling found",
		DerivedFrom: []string{"http://gemetenstad.nl/id/intervention/Z5"},
	}

	got, err := Assemble([]Assembled{matched, noSource})
	require.NoError(t, err)
	turtle := string(got)

	assert.Contains(t, turtle, "@prefix gs:", "a single valid Turtle prefix header must be present")

	// Both anchors present.
	assert.Contains(t, turtle, "<http://gemetenstad.nl/id/auditlink/Z4> a gs:AuditLink")
	assert.Contains(t, turtle, "<http://gemetenstad.nl/id/auditlink/Z5> a gs:AuditLink")

	// Both periods present (content-keyed under their respective anchors).
	assert.Contains(t, turtle, fmt.Sprintf("auditlink/Z4/%s", ContentKey(matched.Outcome)))
	assert.Contains(t, turtle, fmt.Sprintf("auditlink/Z5/%s", ContentKey(noSource.Outcome)))
}

// -- Content-key stability: timestamps do not affect the period IRI ----------
//
// "Requirement: Idempotent re-derivation via content-keyed period nodes":
// the content-key is a hash of the outcome excluding timestamps, so two
// Assembled built from the identical Outcome but different ValidFrom (as if
// derive ran on two different days against unchanged inputs) must yield the
// exact same period IRI.

func TestAssemble_ContentKeyExcludesTimestamps(t *testing.T) {
	outcome := Outcome{
		Matched:        true,
		ObservationIRI: "http://gemetenstad.nl/id/observation/Z6",
		FellingIRIs:    []string{"http://gemetenstad.nl/id/felling/GMB-2022-6"},
		Confidence:     0.70,
		Granularity:    TierAddress,
		Caveats:        []string{CaveatCountUnknown},
	}

	run1 := Assembled{
		Zaaknummer:      "Z6",
		InterventionIRI: "http://gemetenstad.nl/id/intervention/Z6",
		Outcome:         outcome,
		ValidFrom:       time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Evidence:        "matched felling",
		DerivedFrom:     []string{"http://gemetenstad.nl/id/intervention/Z6"},
	}
	run2 := run1
	run2.ValidFrom = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) // a later run, unchanged outcome

	got1, err := Assemble([]Assembled{run1})
	require.NoError(t, err)
	got2, err := Assemble([]Assembled{run2})
	require.NoError(t, err)

	periodIRIRe := regexp.MustCompile(`auditlink/Z6/([0-9a-f]+)`)

	m1 := periodIRIRe.FindStringSubmatch(string(got1))
	m2 := periodIRIRe.FindStringSubmatch(string(got2))
	require.Len(t, m1, 2, "run1 output must contain a content-keyed Z6 period IRI")
	require.Len(t, m2, 2, "run2 output must contain a content-keyed Z6 period IRI")

	assert.Equal(t, m1[1], m2[1],
		"the same Outcome with a different ValidFrom must produce the same content-key (timestamps excluded)")

	// Cross-check against the already-implemented ContentKey directly.
	assert.Equal(t, ContentKey(outcome), m1[1])
}

// -- model-felled-trees D2 (task 3.1/3.3): a matched Observation carries gs:includesFelling -----
//
// "Requirement: Record the coverage outcome as an anchor plus a versioned period", scenario "A
// matched period links a content-addressed Observation of its fellings": the Observation carries
// gs:includesFelling -> each assigned gs:Felling (data:felling/<kapenherplant-id>).
//
// Reconciled to the real Outcome shape (derive/coverage/types.go): the assigned gs:Felling IRIs
// live in Outcome.FellingIRIs, not the guessed Outcome.Fellings. The turtle-content assertions
// themselves (gs:includesFelling present, one triple per assigned felling IRI, felling-namespace
// IRIs) are spec-derived (graph-shapes/spec.md, coverage-audit/spec.md) and did not need to change.
func TestAssemble_MatchedPeriodIncludesFellingMembership(t *testing.T) {
	item := Assembled{
		Zaaknummer:      "Z10",
		InterventionIRI: "http://gemetenstad.nl/id/intervention/Z10",
		Outcome: Outcome{
			Matched:        true,
			ObservationIRI: "http://gemetenstad.nl/id/observation/Z10/abcdef0123456789",
			Confidence:     0.90,
			Granularity:    TierAddress,
			FellingIRIs: []string{
				"http://gemetenstad.nl/id/felling/F1",
				"http://gemetenstad.nl/id/felling/F2",
			},
		},
		ValidFrom:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Evidence:    "matched 2 fellings in buurt BU0363",
		DerivedFrom: []string{"http://gemetenstad.nl/id/intervention/Z10"},
	}

	got, err := Assemble([]Assembled{item})
	require.NoError(t, err)
	turtle := string(got)

	includesLines := gtLinesContaining(turtle, "gs:includesFelling")
	require.NotEmpty(t, includesLines, "a matched Observation must carry gs:includesFelling")
	block := strings.Join(includesLines, "\n")
	assert.Contains(t, block, "http://gemetenstad.nl/id/felling/F1", "expected the first assigned felling as a gs:includesFelling member")
	assert.Contains(t, block, "http://gemetenstad.nl/id/felling/F2", "expected the second assigned felling as a gs:includesFelling member")
}

// gtLinesContaining returns every line of turtle containing substr — a local helper mirroring
// load/koop/graph_test.go's gtLinesContaining (this package has no existing line-scoped helper of
// its own prior to this file's felling-membership addition).
func gtLinesContaining(turtle, substr string) []string {
	var out []string
	for _, line := range strings.Split(turtle, "\n") {
		if strings.Contains(line, substr) {
			out = append(out, line)
		}
	}
	return out
}
