package koop

import "github.com/Blogem/gemeten-stad/location"

// Kind is the publication kind, from the dcterms:title prefix.
type Kind string

const (
	KindAanvraag       Kind = "aanvraag"
	KindBesluit        Kind = "besluit"
	KindOntwerpbesluit Kind = "ontwerpbesluit"
	KindVerlenging     Kind = "verlenging"
	KindIngetrokken    Kind = "ingetrokken"
	KindOther          Kind = "other"
)

// Publication is one landed KOOP publication (SRU record + metadata sidecar).
type Publication struct {
	ID          string // gmb identifier, e.g. "gmb-2022-291126"
	Zaaknummer  string // OVERHEIDop.referentienummer (sidecar); "" = keyless (no sidecar)
	Kind        Kind
	Activiteit  string            // e.g. "kappen"
	Point       *location.RDPoint // RD point from overheidwetgeving:geometrie; nil = absent
	Postcode    string            // parsed from title, e.g. "1024BB"; "" = absent
	Huisnummer  int               // parsed from title; 0 = absent
	Street      string            // parsed from title (best effort); "" = absent
	Title       string            // full dcterms:title
	Available   string            // dcterms:available, "YYYY-MM-DD"; "" = absent
	RawRecord   []byte            // verbatim landed SRU record inner XML
	RawMetadata []byte            // verbatim landed metadata sidecar; nil = keyless
}

// Resolved is the outcome of placing a besluit (filled by resolve.go, consumed by graph.go + PostGIS).
type Resolved struct {
	Identificatie string   // gebieden buurt identificatie — the Place identity; "" = unresolvable
	BuurtCode     string   // gebieden_buurten.code (for the Noord test); "" = unresolvable
	Confidence    float64  // resolver confidence carried onto the locatedAt edge
	Caveats       []string // resolver caveat terms (e.g. "unresolvedLocation")
	InNoord       bool     // resolved buurt.code LIKE 'N%'
	Unresolved    bool     // true when no address resolved and no RD point in a Noord buurt
	Geom          string   // EWKT of the resolver's precise point — set ONLY at the address tier; "" otherwise
	Tier          string   // location.Result.PlaceLevel: "address"/"postcode"/"buurt"; "" when unresolvable
}
