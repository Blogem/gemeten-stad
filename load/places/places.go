package places

import (
	"fmt"
	"strings"
	"time"
)

// buurtRow is a single gebieden_buurten row (all rows are projected — live and soft-deleted, no
// source_deleted_at filter). naam is itself a nullable column (gebieden_buurten.naam has no NOT
// NULL constraint); read.go projects it through COALESCE(naam, '') so this field always holds a
// non-null string — an unnamed row simply yields an empty label rather than a scan error.
// ligtInWijkID and sourceDeletedAt are nullable columns too, but kept as pointers since their
// nil-ness is itself meaningful (unresolved containment / not soft-deleted).
type buurtRow struct {
	identificatie   string
	naam            string
	ligtInWijkID    *string    // nil when null
	sourceDeletedAt *time.Time // nil when live (not soft-deleted)
}

// wijkRow is a single gebieden_wijken row (all rows are projected — live and soft-deleted). naam
// is projected through COALESCE(naam, '') for the same reason as buurtRow.naam.
type wijkRow struct {
	identificatie   string
	naam            string
	sourceDeletedAt *time.Time
}

// dataNS/placeNS are the instance namespaces a Place IRI is minted under (design.md D1). placeNS
// stays under dataNS (http://gemetenstad.nl/id/) — the alignment constraint P12's change detection
// depends on (load/graph/signature.go's `FILTER(STRSTARTS(STR(?s), dataNS))`).
const (
	dataNS  = "http://gemetenstad.nl/id/"
	placeNS = dataNS + "place/"
)

// disallowedIdentificatieChars mirrors load/graph/signature.go's assertSafeIRI forbidden set: the
// characters a Turtle/SPARQL IRIREF may never contain unescaped, plus every control character and
// space. gebieden identificatie values are numeric strings and safe in practice; this is a sanity
// guard against a malformed/injected value ever reaching a minted IRI.
const disallowedIdentificatieChars = "<>\"{}|^`\\"

// assertSafeIdentificatie returns an error if identificatie contains a character that would make
// the minted Place IRI an unsafe/invalid IRIREF (mirrors load/graph/signature.go's assertSafeIRI).
// BuildCandidate is this package's single production entry point and validates every buurt/wijk
// identificatie through this guard before renderPlaces ever runs, so the pure render path
// (mintPlaceIRI, placeToken) can trust its input and stay panic-free.
func assertSafeIdentificatie(identificatie string) error {
	for _, r := range identificatie {
		if r <= 0x20 || strings.ContainsRune(disallowedIdentificatieChars, r) {
			return fmt.Errorf("places: unsafe gebieden identificatie %q: contains disallowed character %U", identificatie, r)
		}
	}
	return nil
}

// mintPlaceIRI returns the full Place IRI for a gebieden identificatie:
//
//	http://gemetenstad.nl/id/place/<identificatie>
//
// It trusts identificatie is already validated (BuildCandidate validates every identificatie via
// assertSafeIdentificatie before any candidate is rendered).
func mintPlaceIRI(identificatie string) string {
	return placeNS + identificatie
}
