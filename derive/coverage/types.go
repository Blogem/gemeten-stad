// Package coverage implements the P14 coverage-audit derive step: scoring
// candidate permit↔felling links, assigning each felling to at most one
// permit, and content-keying the resulting outcome for idempotent re-runs.
package coverage

// Tier is the place-resolution granularity used by a coverage score.
type Tier string

// Tier values rank place resolution by precision: an exact address is the
// strongest signal, a whole buurt the weakest.
const (
	TierAddress  Tier = "address"
	TierPostcode Tier = "postcode"
	TierBuurt    Tier = "buurt"
)

// Tau is the AuditLink assert threshold: confidence >= Tau is a strong match,
// below Tau a matched-but-weak link (gs:caveat gs:weakLink).
const Tau = 0.60

// Caveat local names (emitted downstream as gs:<name>).
const (
	CaveatWeakLink     = "weakLink"
	CaveatCountUnknown = "countUnknown"
)

// ScoreInput is one scoring evaluation — used both per-pair (RegistryCount=1) during
// assignment and per-permit over its assigned set for the final confidence.
type ScoreInput struct {
	Tier          Tier     // place tier (resolved_tier); TierBuurt when point-less
	NearestDistM  *float64 // nil when the permit has no resolved point
	LagDays       int      // publication→felling gap in days (min across the set)
	RegistryCount int      // number of registry fellings in scope (assigned set size)
	PermitCount   *int     // permit-text count; nil = unknown (always nil in Phase 1)
	Contenders    int      // permits contesting these fellings; 1 = sole/clean
}

// ScoreResult is the outcome of scoring one ScoreInput.
type ScoreResult struct {
	Confidence  float64  // clamped [0,1], rounded to 2 decimals
	Granularity Tier     // the tier the place term used
	Caveats     []string // sorted; "countUnknown" when PermitCount==nil; "weakLink" when Confidence<Tau
}

// Pair is a scored permit↔felling candidate, the input to Assign.
type Pair struct {
	Zaaknummer string
	FellingID  string
	Score      float64
	DistanceM  *float64 // tie-break; nil sorts last
}

// Outcome is the content-bearing verdict for one permit, hashed into the period IRI.
type Outcome struct {
	Matched        bool
	ObservationIRI string   // "" when no-source; content-addressed by FellingIRIs (model-felled-trees D2)
	FellingIRIs    []string // sorted, assigned gs:Felling IRIs; nil/empty when no-source
	Confidence     float64  // used only when Matched; caller passes it already rounded 2dp
	Granularity    Tier     // "" when no-source
	Caveats        []string // ContentKey sorts internally; order-independent
}
