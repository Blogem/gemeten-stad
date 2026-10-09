package coverage

import (
	"math"
	"sort"
)

// Place-term anchors (D5): the buurt floor and the address/postcode ceilings.
// The buurt floor is pinned so a point-less permit matches Spike B's
// buurt-level calibration exactly (0.50).
const (
	placeBuurt    = 0.50
	placePostcode = 0.70
	placeAddress  = 0.90
)

// Proximity radii (D5 open question — calibrated at implementation, not spec-pinned):
// within addressRadiusM of the felling, an address-tier resolution counts as
// "at point" and scores the address ceiling; within postcodeRadiusM it scores
// the postcode ceiling; beyond that (or with no distance) it graduates down to
// the buurt floor. These are a first calibration against the spike's labeled
// cases — revisit with a hand-labeled Noord sample if too coarse.
const (
	addressRadiusM  = 50.0
	postcodeRadiusM = 200.0
)

// countTolerance is the ±15% band (D5) for a "count-compatible" registry match.
const countTolerance = 0.15

// timeNearDays / timeFarDays are the publication→felling lag bands (D5):
// ≤2yr scores the higher time bonus, 2–3yr the lower one, beyond that (should
// not occur — candidates are excluded upstream past +3yr) scores neither.
const (
	timeNearDays = 730
	timeFarDays  = 1095
)

// Score adapts spike-b/match_rate.py::confidence to the per-felling signal
// (D5): a distance-graduated place term with a buurt floor, a registry-count
// term, a publication→felling time term, and an assignment-ambiguity term.
func Score(in ScoreInput) ScoreResult {
	place, granularity := placeTerm(in.Tier, in.NearestDistM)

	score := place
	// An unknown permit count contributes +0 and is recorded as a caveat below.
	countUnknown := in.PermitCount == nil
	if !countUnknown {
		c := *in.PermitCount
		switch {
		case in.RegistryCount == c:
			score += 0.30
		case abs(in.RegistryCount-c) <= max(1, roundInt(countTolerance*float64(c))):
			score += 0.15
		default:
			score -= 0.10
		}
	}

	switch {
	case in.LagDays <= timeNearDays:
		score += 0.15
	case in.LagDays <= timeFarDays:
		score += 0.05
	}

	if in.Contenders <= 1 {
		score += 0.05
	} else {
		score += math.Max(-0.15, -0.03*float64(in.Contenders-1))
	}

	confidence := clampRound(score)

	var caveats []string
	if countUnknown {
		caveats = append(caveats, CaveatCountUnknown)
	}
	if confidence < Tau {
		caveats = append(caveats, CaveatWeakLink)
	}
	sort.Strings(caveats)

	return ScoreResult{
		Confidence:  confidence,
		Granularity: granularity,
		Caveats:     caveats,
	}
}

// placeTerm computes the place score + the granularity actually used, given
// the permit's resolved-place tier and its distance to the nearest felling.
// A point-less permit (nil distance) or a buurt-resolved permit always scores
// the buurt floor. Otherwise the term graduates down from the input tier's
// ceiling toward the floor as distance grows.
func placeTerm(tier Tier, distM *float64) (float64, Tier) {
	if distM == nil || tier == TierBuurt {
		return placeBuurt, TierBuurt
	}
	d := *distM
	switch tier {
	case TierAddress:
		switch {
		case d <= addressRadiusM:
			return placeAddress, TierAddress
		case d <= postcodeRadiusM:
			return placePostcode, TierPostcode
		default:
			return placeBuurt, TierBuurt
		}
	case TierPostcode:
		if d <= postcodeRadiusM {
			return placePostcode, TierPostcode
		}
		return placeBuurt, TierBuurt
	default:
		return placeBuurt, TierBuurt
	}
}

func clampRound(v float64) float64 {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return math.Round(v*100) / 100
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func roundInt(v float64) int {
	return int(math.Round(v))
}
