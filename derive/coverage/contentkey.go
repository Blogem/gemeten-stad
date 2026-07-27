package coverage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// contentKeyLen is the number of hex characters kept from the sha256 digest —
// compact enough for an IRI path segment while remaining collision-safe for
// this corpus's outcome cardinality.
const contentKeyLen = 16

// ContentKey (D7) is a stable lowercase hex hash of an Outcome's
// content-bearing fields — matched, the Observation IRI, the rounded
// confidence, granularity, and sorted caveats — excluding any timestamp. The
// same Outcome always yields the same key; any distinct field yields a
// distinct key.
func ContentKey(o Outcome) string {
	caveats := append([]string(nil), o.Caveats...)
	sort.Strings(caveats)

	confidence := "-"
	if o.Matched {
		confidence = fmt.Sprintf("%.2f", o.Confidence)
	}

	canonical := strings.Join([]string{
		fmt.Sprintf("matched=%t", o.Matched),
		"obs=" + o.ObservationIRI,
		"conf=" + confidence,
		"gran=" + string(o.Granularity),
		"caveats=" + strings.Join(caveats, ","),
	}, "|")

	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])[:contentKeyLen]
}
