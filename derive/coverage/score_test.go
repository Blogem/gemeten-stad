package coverage

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func f64(v float64) *float64 { return &v }
func iptr(v int) *int        { return &v }

// -- Buurt-floor golden (Spike B parity) -------------------------------------
//
// A point-less permit (no resolved_geom -> buurt tier, no distance), unknown
// registry count, felling within 2yr, sole candidate. This must reproduce the
// Spike B buurt-level calibration exactly: 0.50 + 0 + 0.15 + 0.05 = 0.70.

func TestScore_BuurtFloorGolden(t *testing.T) {
	in := ScoreInput{
		Tier:          TierBuurt,
		NearestDistM:  nil,
		LagDays:       400,
		RegistryCount: 1,
		PermitCount:   nil,
		Contenders:    1,
	}

	got := Score(in)

	assert.InDelta(t, 0.70, got.Confidence, 1e-9)
	assert.Equal(t, TierBuurt, got.Granularity)
	assert.Equal(t, []string{CaveatCountUnknown}, got.Caveats)
}

// -- Proximity lifts place above the buurt floor -----------------------------
//
// D5: the place term is distance-graduated up from the 0.50 floor. We don't
// pin the exact graduation curve (calibrated at implementation time against
// the spike's labeled cases), only the direction: an address-tier permit
// essentially at the felling's point must score strictly above the buurt
// floor equivalent, and report granularity gs:address.

func TestScore_ProximityLiftsPlaceAboveFloor(t *testing.T) {
	floorInput := ScoreInput{
		Tier:          TierBuurt,
		NearestDistM:  nil,
		LagDays:       400,
		RegistryCount: 1,
		PermitCount:   nil,
		Contenders:    1,
	}
	floor := Score(floorInput)

	near := floorInput
	near.Tier = TierAddress
	near.NearestDistM = f64(5.0) // essentially at the permit's resolved point

	got := Score(near)

	assert.Greater(t, got.Confidence, floor.Confidence,
		"an address-tier proximate match must score strictly above the buurt floor")
	assert.Equal(t, TierAddress, got.Granularity)
}

// -- Time buckets: <=730d +0.15 vs 731..1095d +0.05 --------------------------

func TestScore_TimeBuckets(t *testing.T) {
	withLag := func(lag int) ScoreInput {
		return ScoreInput{
			Tier:          TierBuurt,
			NearestDistM:  nil,
			LagDays:       lag,
			RegistryCount: 1,
			PermitCount:   nil,
			Contenders:    1,
		}
	}

	at730 := Score(withLag(730))
	at731 := Score(withLag(731))
	at1095 := Score(withLag(1095))

	assert.InDelta(t, 0.70, at730.Confidence, 1e-9, "LagDays=730 is within the <=2yr +0.15 bucket")
	assert.InDelta(t, 0.60, at731.Confidence, 1e-9, "LagDays=731 crosses into the 2-3yr +0.05 bucket")
	assert.InDelta(t, 0.60, at1095.Confidence, 1e-9, "LagDays=1095 is still the +0.05 bucket")
	assert.InDelta(t, 0.10, at730.Confidence-at731.Confidence, 1e-9,
		"the step between buckets is 0.15-0.05=0.10")
}

// -- Ambiguity: sole +0.05, contested -0.03*(n-1), floored -0.15 -------------

func TestScore_Ambiguity(t *testing.T) {
	withContenders := func(n int) ScoreInput {
		return ScoreInput{
			Tier:          TierBuurt,
			NearestDistM:  nil,
			LagDays:       400,
			RegistryCount: 1,
			PermitCount:   nil,
			Contenders:    n,
		}
	}

	tests := []struct {
		name        string
		contenders  int
		wantConf    float64
		wantCaveats []string
	}{
		{"sole contender: +0.05", 1, 0.70, []string{CaveatCountUnknown}},
		{"two contenders: -0.03*(2-1)", 2, 0.62, []string{CaveatCountUnknown}},
		{"six contenders hits the -0.15 floor", 6, 0.50, []string{CaveatCountUnknown, CaveatWeakLink}},
		{"ten contenders stays at the -0.15 floor", 10, 0.50, []string{CaveatCountUnknown, CaveatWeakLink}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Score(withContenders(tt.contenders))
			assert.InDelta(t, tt.wantConf, got.Confidence, 1e-9)
			assert.Equal(t, tt.wantCaveats, got.Caveats)
		})
	}

	// The floor must hold: six and ten contenders are indistinguishable in the
	// ambiguity term once the -0.15 floor is reached.
	six := Score(withContenders(6))
	ten := Score(withContenders(10))
	assert.InDelta(t, six.Confidence, ten.Confidence, 1e-9)
}

// -- weakLink caveat below tau ------------------------------------------------

func TestScore_WeakLinkCaveatBelowTau(t *testing.T) {
	in := ScoreInput{
		Tier:          TierBuurt,
		NearestDistM:  nil,
		LagDays:       1095, // +0.05
		RegistryCount: 1,
		PermitCount:   nil,
		Contenders:    5, // -0.03*(5-1) = -0.12
	}

	got := Score(in)

	// 0.50 (buurt floor) + 0 (count unknown) + 0.05 (time) - 0.12 (ambiguity) = 0.43
	assert.InDelta(t, 0.43, got.Confidence, 1e-9)
	assert.Less(t, got.Confidence, Tau)
	require.Contains(t, got.Caveats, CaveatWeakLink)
	assert.True(t, sort.StringsAreSorted(got.Caveats), "caveats must be sorted")
	assert.Equal(t, []string{CaveatCountUnknown, CaveatWeakLink}, got.Caveats)
}

// -- Clamp: exceeding 1.0 clamps to 1.0 --------------------------------------
//
// Address-tier proximate (pinned anchor 0.90) + exact registry-count match
// (+0.30) + <=2yr lag (+0.15) + sole contender (+0.05) sums to 1.40 unclamped.

func TestScore_ClampsAboveOneToOne(t *testing.T) {
	permitCount := iptr(3)
	in := ScoreInput{
		Tier:          TierAddress,
		NearestDistM:  f64(1.0),
		LagDays:       400,
		RegistryCount: 3,
		PermitCount:   permitCount,
		Contenders:    1,
	}

	got := Score(in)

	assert.InDelta(t, 1.0, got.Confidence, 1e-9)
	assert.Equal(t, TierAddress, got.Granularity)
	assert.NotContains(t, got.Caveats, CaveatWeakLink)
}

// -- Clamp: the documented axis floor never goes below 0 --------------------
//
// NOTE (tester, pass 1): the documented bucket arithmetic floors at
// place=0.50 (buurt, never lower) + count=-0.10 (incompatible) + time=+0.05
// (2-3yr, never negative) + ambiguity=-0.15 (floored) = 0.30. There is no
// combination of the *documented* axes that drives the unclamped sum below
// zero, so a genuine "clamps to 0" case cannot be constructed without
// inventing undocumented input (e.g. a negative LagDays outside the
// [0,1095] candidate window). This test pins that floor instead and
// confirms Score never underflows for the worst realistic combination; see
// the handoff report for this as a flagged spec gap rather than a silent
// assumption.

func TestScore_MinimumAxisCombinationDoesNotUnderflow(t *testing.T) {
	permitCount := iptr(100)
	in := ScoreInput{
		Tier:          TierBuurt,
		NearestDistM:  nil,
		LagDays:       1095,
		RegistryCount: 1, // far from PermitCount -> incompatible
		PermitCount:   permitCount,
		Contenders:    50, // floors the ambiguity term
	}

	got := Score(in)

	assert.InDelta(t, 0.30, got.Confidence, 1e-9)
	assert.GreaterOrEqual(t, got.Confidence, 0.0)
}
