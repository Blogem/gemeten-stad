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
