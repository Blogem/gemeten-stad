package coverage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// -- Determinism: the same Outcome yields the same key -----------------------

func TestContentKey_Deterministic(t *testing.T) {
	o := Outcome{
		Matched:        true,
		ObservationIRI: "data:observation/GMB-2022-1",
		Confidence:     0.70,
		Granularity:    TierBuurt,
		Caveats:        []string{CaveatCountUnknown},
	}

	k1 := ContentKey(o)
	k2 := ContentKey(o)

	assert.Equal(t, k1, k2)
	assert.NotEmpty(t, k1)
}

// -- Caveat-order independence ------------------------------------------------

func TestContentKey_CaveatOrderIndependent(t *testing.T) {
	base := Outcome{
		Matched:        true,
		ObservationIRI: "data:observation/GMB-2022-1",
		Confidence:     0.55,
		Granularity:    TierBuurt,
	}

	a := base
	a.Caveats = []string{CaveatWeakLink, CaveatCountUnknown}
	b := base
	b.Caveats = []string{CaveatCountUnknown, CaveatWeakLink}

	assert.Equal(t, ContentKey(a), ContentKey(b),
		"the same caveat SET in a different slice order must hash identically")
}

// -- Distinctness: each axis that changes produces a distinct key -----------

func TestContentKey_DistinctOutcomesYieldDistinctKeys(t *testing.T) {
	baseline := Outcome{
		Matched:        true,
		ObservationIRI: "data:observation/GMB-2022-1",
		Confidence:     0.70,
		Granularity:    TierBuurt,
		Caveats:        []string{CaveatCountUnknown},
	}

	withMatched := baseline
	withMatched.Matched = false

	withObservationIRI := baseline
	withObservationIRI.ObservationIRI = "data:observation/GMB-2022-2"

	withConfidence := baseline
	withConfidence.Confidence = 0.71

	withGranularity := baseline
	withGranularity.Granularity = TierPostcode

	withCaveatSet := baseline
	withCaveatSet.Caveats = []string{CaveatWeakLink}

	noSource := Outcome{
		Matched:        false,
		ObservationIRI: "",
		Confidence:     0,
		Granularity:    "",
		Caveats:        nil,
	}

	variants := map[string]Outcome{
		"baseline":                      baseline,
		"matched flipped":               withMatched,
		"different observation IRI":     withObservationIRI,
		"confidence 0.71 vs 0.70":       withConfidence,
		"granularity postcode vs buurt": withGranularity,
		"different caveat set":          withCaveatSet,
		"no-source":                     noSource,
	}

	seen := make(map[string]string, len(variants))
	for name, o := range variants {
		k := ContentKey(o)
		if other, exists := seen[k]; exists {
			t.Fatalf("outcomes %q and %q produced the same content key %q", name, other, k)
		}
		seen[k] = name
	}
}

// -- No-source vs matched must differ ----------------------------------------

func TestContentKey_NoSourceVsMatchedDiffer(t *testing.T) {
	matched := Outcome{
		Matched:        true,
		ObservationIRI: "data:observation/GMB-2022-1",
		Confidence:     0.70,
		Granularity:    TierBuurt,
		Caveats:        []string{CaveatCountUnknown},
	}
	noSource := Outcome{
		Matched:        false,
		ObservationIRI: "",
		Confidence:     0,
		Granularity:    "",
		Caveats:        nil,
	}

	assert.NotEqual(t, ContentKey(matched), ContentKey(noSource))
}

// -- model-felled-trees D2/D3 (task 3.4): the Observation IRI is content-addressed by its assigned
// felling set --------------------------------------------------------------------------------
//
// D2: "data:observation/<zaaknummer>/<felling-set-key>, a hash of the sorted assigned gs:Felling
// IRIs". D3: the period content-key changes IFF the assigned set changes (via the Observation
// IRI), and stays identical otherwise — this is the fix for the real-corpus "59 anchors with two
// open periods" bug (design.md Context): two outcomes with the SAME rounded confidence but
// DIFFERENT assigned felling sets must never collide on one content-key.
//
// TODO(pass-2): FellingSetKey and mintObservationIRI below are BEST-GUESS names for the functions
// the coder adds (contentkey.go/assemble.go, mirroring ContentKey's own hash-of-sorted-fields
// convention and assemble.go's existing mintAnchorIRI/mintPeriodIRI unexported-helper convention).
// Once the coder's actual felling-set-key / Observation-IRI minting function is visible, fix the
// two call sites in fellingSetObservationIRI below to match its real name/signature. The
// TestContentKey_SameRoundedConfidenceDifferentFellingSetStillDiffers test further below does NOT
// depend on this guess — it is built directly on the existing Outcome/ContentKey and should already
// pass; keep it as the durable regression pin for D3 regardless of how the minting function ends up
// named.
func fellingSetObservationIRI(zaaknummer string, fellingIRIs []string) string {
	return mintObservationIRI(zaaknummer, FellingSetKey(fellingIRIs))
}

func TestFellingSetKey_SameSetSameKeyRegardlessOfOrder(t *testing.T) {
	set1 := []string{"http://gemetenstad.nl/id/felling/F1", "http://gemetenstad.nl/id/felling/F2"}
	set2 := []string{"http://gemetenstad.nl/id/felling/F2", "http://gemetenstad.nl/id/felling/F1"}

	iri1 := fellingSetObservationIRI("Z1", set1)
	iri2 := fellingSetObservationIRI("Z1", set2)

	assert.Equal(t, iri1, iri2, "the same felling SET in a different slice order must mint the same Observation IRI")
	assert.NotEmpty(t, iri1)
}

func TestFellingSetKey_ChangedSetYieldsDifferentIRI_SameCountSwap(t *testing.T) {
	// A same-COUNT swap ({F1,F2} -> {F1,F3}) must still mint a different Observation IRI — the D2
	// scenario "A changed felling set mints a new Observation and a new period".
	before := []string{"http://gemetenstad.nl/id/felling/F1", "http://gemetenstad.nl/id/felling/F2"}
	after := []string{"http://gemetenstad.nl/id/felling/F1", "http://gemetenstad.nl/id/felling/F3"}

	iriBefore := fellingSetObservationIRI("Z2", before)
	iriAfter := fellingSetObservationIRI("Z2", after)

	assert.NotEqual(t, iriBefore, iriAfter, "swapping one member of a same-count felling set must mint a NEW Observation IRI")
}

func TestFellingSetKey_DifferentZaaknummerSameSetYieldsDifferentIRI(t *testing.T) {
	set := []string{"http://gemetenstad.nl/id/felling/F1"}
	assert.NotEqual(t, fellingSetObservationIRI("Z-A", set), fellingSetObservationIRI("Z-B", set),
		"the Observation IRI is scoped per zaaknummer (data:observation/<zaaknummer>/<felling-set-key>)")
}

// TestContentKey_SameRoundedConfidenceDifferentFellingSetStillDiffers is the durable D3 regression
// pin: built directly on Outcome/ContentKey (no dependency on the guessed minting functions above),
// it asserts that two outcomes sharing IDENTICAL confidence/granularity/caveats but DIFFERENT
// Observation IRIs (as two different assigned felling sets would mint, per D2) must never produce
// the same content-key — exactly the collision design.md describes ("two different outcomes with
// the same rounded confidence collide on one period IRI").
func TestContentKey_SameRoundedConfidenceDifferentFellingSetStillDiffers(t *testing.T) {
	o1 := Outcome{
		Matched:        true,
		ObservationIRI: "http://gemetenstad.nl/id/observation/Z9/feeeeeeeeeeeeeee1",
		Confidence:     0.70,
		Granularity:    TierBuurt,
		Caveats:        []string{CaveatCountUnknown},
	}
	o2 := o1
	o2.ObservationIRI = "http://gemetenstad.nl/id/observation/Z9/feeeeeeeeeeeeeee2"

	assert.NotEqual(t, ContentKey(o1), ContentKey(o2),
		"identical confidence/granularity/caveats but a different (felling-set-addressed) Observation IRI must yield a different content-key")
}
