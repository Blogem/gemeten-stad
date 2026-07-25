package location

import "time"

// RDPoint is a point in the Rijksdriehoeksstelsel (RD / EPSG:28992), the SRID every geometry in
// the geo backbone is stored in.
type RDPoint struct {
	X, Y float64
}

// Query is a location to resolve, at a given valid-time date. Zero/empty fields mean "absent":
// Huisnummer == 0 means no house number was supplied, Point == nil means no point was supplied.
type Query struct {
	Street     string
	Huisnummer int // 0 = absent
	Postcode   string
	Point      *RDPoint  // nil = absent
	Date       time.Time // valid-time
}

// PlaceLevel is the granularity a query resolved to.
type PlaceLevel string

const (
	// PlaceAddress is the finest tier: resolved to a single adresseerbaar object.
	PlaceAddress PlaceLevel = "address"
	// PlacePostcode is the middle tier: the PC6 exists in BAG but no huisnummer resolved.
	PlacePostcode PlaceLevel = "postcode"
	// PlaceBuurt is the floor tier: the query's own point was placed in a buurt polygon.
	PlaceBuurt PlaceLevel = "buurt"
)

// Confidence scores per place level (spec: address 0.90, postcode 0.70, buurt 0.50).
const (
	confidenceAddress  = 0.90
	confidencePostcode = 0.70
	confidenceBuurt    = 0.50
)

// Caveats surfaced on a Result. These are attached to the downstream AuditLink as-is.
const (
	caveatUnresolvedLocation = "unresolvedLocation"
	caveatTimeMismatch       = "timeMismatch"
)

// TimeMatch values recorded on a Result when the place level is address.
const (
	timeMatchValidAtDate = "valid_at_date"
	timeMatchAnyTime     = "any_time"
)

// Result is the outcome of resolving a Query.
type Result struct {
	PlaceLevel PlaceLevel
	Geom       string // EWKT of the resolved geometry ("" at the postcode tier: no single point applies)
	Confidence float64
	TimeMatch  string // "valid_at_date" | "any_time" | "" (only set at the address tier)
	Caveats    []string
	BuurtID    string // set at the buurt tier (gbdBuurtId)
}

// PlaceLevelFor decides the tier + confidence from which candidates resolved. Preference is
// address over postcode over buurt, matching the match-preference rule in the spec. ok is false
// when none of the three resolved — the caller (Resolve) turns that into an error.
func PlaceLevelFor(hasAddress, hasPostcode, hasBuurt bool) (level PlaceLevel, confidence float64, ok bool) {
	switch {
	case hasAddress:
		return PlaceAddress, confidenceAddress, true
	case hasPostcode:
		return PlacePostcode, confidencePostcode, true
	case hasBuurt:
		return PlaceBuurt, confidenceBuurt, true
	default:
		return "", 0, false
	}
}

// TimeMatchFor maps whether the chosen voorkomen was valid at the query date to the time_match
// value recorded on the result.
func TimeMatchFor(validAtDate bool) string {
	if validAtDate {
		return timeMatchValidAtDate
	}
	return timeMatchAnyTime
}
