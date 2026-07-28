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

// hashHex16 is the shared sha256->hex-16 digest used both for the period
// content-key (ContentKey) and the Observation's felling-set content-address
// (FellingSetKey) — one hashing convention for both content-addressing jobs.
func hashHex16(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])[:contentKeyLen]
}

// FellingSetKey (model-felled-trees D2) is the content-address for a matched
// permit's gs:Observation: a stable hash of the sorted assigned gs:Felling
// IRIs. The same felling set always yields the same key (order-independent —
// fellingIRIs is sorted internally); any changed member (add/remove/swap)
// yields a distinct key, which is exactly what makes the resulting
// Observation IRI (and, transitively, the period content-key via
// ContentKey's "obs=" term) version correctly.
func FellingSetKey(fellingIRIs []string) string {
	sorted := append([]string(nil), fellingIRIs...)
	sort.Strings(sorted)
	return hashHex16(strings.Join(sorted, ","))
}

// ContentKey (D7) is a stable lowercase hex hash of an Outcome's
// content-bearing fields — matched, the Observation IRI, the rounded
// confidence, granularity, and sorted caveats — excluding any timestamp. The
// same Outcome always yields the same key; any distinct field yields a
// distinct key. The Observation IRI is itself content-addressed by the
// assigned felling set (FellingSetKey), so hashing it here is sufficient to
// version the period on a changed felling set — no separate felling-set term
// is needed in this canonical string (model-felled-trees D3).
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

	return hashHex16(canonical)
}
