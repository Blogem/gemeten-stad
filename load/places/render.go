package places

import (
	"fmt"
	"strings"
	"time"
)

// turtlePreamble declares the prefixes every emitted candidate needs: gs: (TBox), rdfs:label,
// xsd:date (the gs:validFrom annotation range), and a dedicated place: prefix bound to placeNS —
// a `/` is not legal in a Turtle prefixed-name local part, so Place IRIs are written as
// place:<identificatie> rather than under data: directly (design.md D1).
const turtlePreamble = `@prefix gs: <` + gsNS + `> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix place: <` + placeNS + `> .

`

const gsNS = "http://gemetenstad.nl/ns#"

// dateLayout is the xsd:date lexical form (YYYY-MM-DD) gs:validFrom is rendered in.
const dateLayout = "2006-01-02"

// literalEscaper escapes a naam value for safe embedding inside a Turtle double-quoted string
// literal, in a single left-to-right pass — backslash first, so the backslash introduced by
// escaping a quote/control char is never itself re-escaped (mirrors
// load/graph/signature.go's literalEscaper).
var literalEscaper = strings.NewReplacer(
	`\`, `\\`,
	`"`, `\"`,
	"\n", `\n`,
	"\r", `\r`,
	"\t", `\t`,
)

// turtleString renders s as a quoted Turtle string literal with its lexical value escaped.
func turtleString(s string) string {
	return `"` + literalEscaper.Replace(s) + `"`
}

// placeToken renders identificatie as a place:-prefixed Turtle token, after validating it through
// the same safety guard mintPlaceIRI uses.
func placeToken(identificatie string) string {
	assertSafeIdentificatie(identificatie)
	return "place:" + identificatie
}

// renderActivePlace writes one Place's `a gs:Place`, `rdfs:label`, and RDF-star-annotated
// `gs:active` block into b. A live row (sourceDeletedAt nil) asserts gs:active true with loadTS as
// its gs:validFrom; a soft-deleted row asserts gs:active false with its own sourceDeletedAt as
// gs:validFrom (design.md D5).
func renderActivePlace(b *strings.Builder, identificatie, naam string, sourceDeletedAt *time.Time, loadTS time.Time) {
	active := "true"
	validFrom := loadTS.Format(dateLayout)
	if sourceDeletedAt != nil {
		active = "false"
		validFrom = sourceDeletedAt.Format(dateLayout)
	}

	fmt.Fprintf(b, "%s a gs:Place ;\n", placeToken(identificatie))
	fmt.Fprintf(b, "    rdfs:label %s ;\n", turtleString(naam))
	fmt.Fprintf(b, "    gs:active %s {| gs:validFrom \"%s\"^^xsd:date |} .\n\n", active, validFrom)
}

// renderPlaces is a PURE function (no DB, no logging, no clock) from gebieden rows to a Turtle
// candidate. Every buurt and every wijk — live and soft-deleted — is projected as a full gs:Place
// (identity, rdfs:label, gs:active). A buurt's gs:within edge to its wijk is emitted only when its
// ligtInWijkID is non-nil and resolves to a wijk present in wijken; every other buurt is still
// seeded as a full Place, without a gs:within edge, and its identificatie is appended to
// danglingBuurten so the caller can warn (design.md D5, spec.md "Seed the buurt->wijk containment
// via gs:within").
func renderPlaces(buurten []buurtRow, wijken []wijkRow, loadTS time.Time) (turtle []byte, danglingBuurten []string) {
	wijkIDs := make(map[string]struct{}, len(wijken))
	for _, wijk := range wijken {
		wijkIDs[wijk.identificatie] = struct{}{}
	}

	var b strings.Builder
	b.WriteString(turtlePreamble)

	for _, buurt := range buurten {
		renderActivePlace(&b, buurt.identificatie, buurt.naam, buurt.sourceDeletedAt, loadTS)

		resolved := false
		if buurt.ligtInWijkID != nil {
			if _, ok := wijkIDs[*buurt.ligtInWijkID]; ok {
				fmt.Fprintf(&b, "%s gs:within %s .\n\n", placeToken(buurt.identificatie), placeToken(*buurt.ligtInWijkID))
				resolved = true
			}
		}
		if !resolved {
			danglingBuurten = append(danglingBuurten, buurt.identificatie)
		}
	}

	for _, wijk := range wijken {
		renderActivePlace(&b, wijk.identificatie, wijk.naam, wijk.sourceDeletedAt, loadTS)
	}

	return []byte(b.String()), danglingBuurten
}
