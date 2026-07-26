package places

import (
	"fmt"
	"strings"
	"time"
)

// buurtRow is a single gebieden_buurten row (all rows are projected — live and soft-deleted, no
// source_deleted_at filter). ligtInWijkID and sourceDeletedAt are nullable columns, hence pointers.
type buurtRow struct {
	identificatie   string
	naam            string
	ligtInWijkID    *string    // nil when null
	sourceDeletedAt *time.Time // nil when live (not soft-deleted)
}

// wijkRow is a single gebieden_wijken row (all rows are projected — live and soft-deleted).
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

// assertSafeIdentificatie panics if identificatie contains a character that would make the minted
// Place IRI an unsafe/invalid IRIREF. mintPlaceIRI and the Turtle renderer share this single choke
// point. A gebieden identificatie is a database PK we do not otherwise control the shape of, so a
// violation here indicates a data invariant break, not a normal error path — hence the panic
// rather than a speculative error-return plumbed through the pure renderer's fixed signature.
func assertSafeIdentificatie(identificatie string) {
	for _, r := range identificatie {
		if r <= 0x20 || strings.ContainsRune(disallowedIdentificatieChars, r) {
			panic(fmt.Sprintf("places: unsafe gebieden identificatie %q: contains disallowed character %U", identificatie, r))
		}
	}
}

// mintPlaceIRI returns the full Place IRI for a gebieden identificatie:
//
//	http://gemetenstad.nl/id/place/<identificatie>
func mintPlaceIRI(identificatie string) string {
	assertSafeIdentificatie(identificatie)
	return placeNS + identificatie
}
