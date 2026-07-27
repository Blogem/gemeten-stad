package koop

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// AuditedBesluit pairs one audited besluit publication with its resolved location — the unit
// buildCandidate renders one Turtle block for (task 4). Pub carries the parsed besluit fields
// (Zaaknummer, Activiteit, Available); Res carries the resolver's placement outcome
// (Identificatie, Confidence, Caveats) from load/koop/resolve.go (task 3). Only resolved besluiten
// (Res.Unresolved == false, Res.Identificatie != "") are expected here — the caller (load.go, task
// 5) filters unresolvable besluiten out before assembly.
type AuditedBesluit struct {
	Pub Publication
	Res Resolved
}

// gsNS/dataNS mirror the settled RDF namespaces (load/graph/signature.go, load/places/places.go):
// gs: for the TBox, data: for every instance IRI change detection is keyed on (design.md D1).
// interventionNS/claimNS/placeNS mint the three instance IRIs this package writes; placeNS matches
// load/places' placeNS exactly (P12b alignment, task 9.2) so a koop-minted Place IRI and a
// places-seeded Place IRI for the same buurt identificatie are the same node.
const (
	gsNS           = "http://gemetenstad.nl/ns#"
	dataNS         = "http://gemetenstad.nl/id/"
	interventionNS = dataNS + "intervention/"
	claimNS        = dataNS + "claim/"
	placeNS        = dataNS + "place/"
)

// turtlePreamble declares the prefixes every emitted candidate needs: gs: (TBox) and act: bound
// to the activity concept namespace so mapActivity can emit the compact act:vellen token rather
// than a full bracketed IRI. Instance IRIs (intervention/claim/place) are deliberately written as
// full angle-bracket IRIs, not prefixed names — a zaaknummer is external, untrusted-ish input
// (task contract), and a bracketed IRI needs no prefixed-name-local-part validity check the way a
// prefixed token would.
const turtlePreamble = `@prefix gs: <` + gsNS + `> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
@prefix act: <` + dataNS + `activity/> .
@prefix dct: <http://purl.org/dc/terms/> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .

`

// disallowedIRIChars mirrors load/graph/signature.go's assertSafeIRI forbidden set: the characters
// a Turtle IRIREF may never contain unescaped.
const disallowedIRIChars = "<>\"{}|^`\\"

// assertSafeIRI rejects id if it contains any character not permitted raw inside a Turtle IRIREF
// (control chars, space, or one of disallowedIRIChars) — the single choke point every zaaknummer
// and identificatie passes through before it is minted into an IRI (mirrors
// load/graph/signature.go's assertSafeIRI and load/places/places.go's assertSafeIdentificatie).
func assertSafeIRI(id string) error {
	for _, r := range id {
		if r <= 0x20 || strings.ContainsRune(disallowedIRIChars, r) {
			return fmt.Errorf("koop: unsafe IRI component %q: contains disallowed character %U", id, r)
		}
	}
	return nil
}

// mintInterventionIRI, mintClaimIRI, mintPlaceIRI return the full instance IRI for a besluit's
// zaaknummer / a resolved buurt identificatie. Callers must validate the input via assertSafeIRI
// first (buildCandidate does this once per item before any minting) — these are pure string
// concatenation and trust their input.
func mintInterventionIRI(zaaknummer string) string { return interventionNS + zaaknummer }
func mintClaimIRI(zaaknummer string) string        { return claimNS + zaaknummer }
func mintPlaceIRI(identificatie string) string     { return placeNS + identificatie }

// controlledCaveats is the SHACL-controlled caveat vocabulary (ontology/shapes.ttl
// InterventionShape, clause 1e): a locatedAt edge's gs:caveat annotation may only ever reference
// one of these four terms.
var controlledCaveats = map[string]bool{
	"unresolvedLocation": true,
	"timeMismatch":       true,
	"weakLink":           true,
	"transplantOrigin":   true,
}

// mapActivity maps a besluit's raw activiteit string (e.g. "kappen", "vellen", "verplanten") to
// the single gs:activity vocabulary concept the current corpus needs. Spike C established
// verplanten ≡ vellen for audit purposes (a felling followed by a mandatory replant is still one
// felling-activity intervention), and every other activiteit value observed in the KOOP corpus
// (task 4.3) is likewise a felling variant — so this always returns "act:vellen" today. The
// parameter is kept so the mapping has one call site to extend if/when a non-felling activiteit
// enters the corpus; it does not yet need branching (project rule: no speculative knobs).
func mapActivity(activiteit string) string {
	_ = activiteit
	return "act:vellen"
}

// mapCaveats returns the gs: caveat local names (e.g. "unresolvedLocation") to annotate res's
// locatedAt edge with, per ontology/shapes.ttl InterventionShape:
//
//   - confidence == 1.0 (exact resolution): shape clause 1d only requires a caveat when
//     confidence < 1.0, so an exact edge gets none — even if the resolver attached one.
//   - confidence < 1.0: at least one controlled caveat MUST be present. res.Caveats already uses
//     the gs: local names verbatim (e.g. "unresolvedLocation", "timeMismatch" — task contract), so
//     values already in controlledCaveats pass through unchanged; anything outside that set is
//     dropped (clause 1e forbids a non-controlled caveat value). If nothing survives the filter,
//     gs:unresolvedLocation is the default: this graph places an Intervention at buurt
//     granularity, which is inherently coarser than the resolver's underlying point or address, so
//     "unresolved location" always applies at minimum.
func mapCaveats(res Resolved) []string {
	if res.Confidence >= 1.0 {
		return nil
	}

	var caveats []string
	for _, c := range res.Caveats {
		if controlledCaveats[c] {
			caveats = append(caveats, c)
		}
	}
	if len(caveats) == 0 {
		caveats = append(caveats, "unresolvedLocation")
	}
	return caveats
}

// formatConfidence renders c (expected in [0,1]) as a Turtle decimal literal that always carries a
// decimal point — strconv.FormatFloat's 'f' format drops the point for whole numbers (e.g. 1.0 ->
// "1"), and a Turtle integer-shaped token (no ".") parses as xsd:integer rather than xsd:decimal.
// ontology/shapes.ttl's confidence-range and caveat-presence checks both FILTER on ?c < 1.0 /
// ?c < 0 || ?c > 1, which need a numeric literal to compare against — forcing the decimal point
// keeps the emitted term unambiguous rather than relying on cross-type numeric promotion.
func formatConfidence(c float64) string {
	s := strconv.FormatFloat(c, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// renderLocatedAtAnnotations builds the RDF-star annotation body for item's locatedAt edge:
// gs:confidence always (shape clause 1b requires it unconditionally) and gs:caveat for each name
// mapCaveats returns (zero or more). locatedAt is a Flavour-1 refinable-metadata edge
// (docs/RDF_STAR_RELATIONSHIPS.md): a permit's location holds, and only its confidence is refined
// over transaction-time, so the edge never carries gs:validFrom/gs:validTo (design.md D3).
func renderLocatedAtAnnotations(item AuditedBesluit) string {
	parts := []string{"gs:confidence " + formatConfidence(item.Res.Confidence)}

	for _, caveat := range mapCaveats(item.Res) {
		parts = append(parts, "gs:caveat gs:"+caveat)
	}

	return strings.Join(parts, " ; ")
}

// renderAuditedBesluit validates item's zaaknummer and resolved identificatie, then writes its
// Turtle block into b: the Intervention (with dct:available, gs:activity, gs:claims, and the
// annotated gs:locatedAt edge), the bare Claim, and the bare Place (task 4.1 — the type triple lets
// the edge satisfy InterventionShape's sh:class gs:Place check against a candidate-only validation;
// against an already-seeded P12b Place of the same identificatie it becomes a harmless
// immutableConflict skip in load/graph, design.md D5).
//
// dct:available carries the besluit's publication date (Publication.Available) as a timeless
// descriptive-metadata literal — reusing Dublin Core Terms per docs/RDF_MODELING.md §3, not a
// minted gs: term, and deliberately carrying no gs:validFrom/gs:validTo (it never evolves).
// Available is externally sourced (decoded from the KOOP SRU XML, only whitespace-trimmed by
// load/koop/parse.go) so it is never trusted verbatim: it MUST be parsed as a canonical
// "2006-01-02" date before being emitted, and only the reformatted, validated lexical form is
// written into the literal. This closes a Turtle-injection vector (an unvalidated value could
// carry a stray quote to terminate the literal early and splice in arbitrary triples) and
// guarantees a syntactically valid xsd:date. A parse failure is a real data anomaly (mirrors
// load/koop/stage.go's identical time.Parse guard for the Postgres path) — this function has no
// sensible fallback date, so it fails loud with a descriptive error rather than coining one or
// silently emitting unvalidated text; buildCandidate's existing skip-and-record contract (task
// 4.4 resilience follow-up) is what surfaces this to the caller without aborting the whole batch.
func renderAuditedBesluit(b *strings.Builder, item AuditedBesluit) error {
	zaaknummer := item.Pub.Zaaknummer
	identificatie := item.Res.Identificatie

	if err := assertSafeIRI(zaaknummer); err != nil {
		return fmt.Errorf("koop: besluit %s: zaaknummer: %w", item.Pub.ID, err)
	}
	if err := assertSafeIRI(identificatie); err != nil {
		return fmt.Errorf("koop: besluit %s: identificatie: %w", item.Pub.ID, err)
	}
	available, err := time.Parse("2006-01-02", item.Pub.Available)
	if err != nil {
		return fmt.Errorf("koop: besluit %s: zaaknummer %s: invalid publication date %q: %w", item.Pub.ID, zaaknummer, item.Pub.Available, err)
	}

	interventionIRI := mintInterventionIRI(zaaknummer)
	claimIRI := mintClaimIRI(zaaknummer)
	placeIRI := mintPlaceIRI(identificatie)

	fmt.Fprintf(b, "<%s> a gs:Intervention ;\n", interventionIRI)
	fmt.Fprintf(b, "    dct:available \"%s\"^^xsd:date ;\n", available.Format("2006-01-02"))
	fmt.Fprintf(b, "    gs:activity %s ;\n", mapActivity(item.Pub.Activiteit))
	fmt.Fprintf(b, "    gs:claims <%s> ;\n", claimIRI)
	fmt.Fprintf(b, "    gs:locatedAt <%s> {| %s |} .\n", placeIRI, renderLocatedAtAnnotations(item))
	fmt.Fprintf(b, "<%s> a gs:Claim .\n", claimIRI)
	fmt.Fprintf(b, "<%s> a gs:Place .\n\n", placeIRI)

	return nil
}

// buildCandidate renders each audited besluit into the shared candidate, skipping (not failing on)
// any item whose IRIs are unsafe; skipped holds the gmb IDs of those items so the caller can log
// them. Returns nil candidate when items is empty OR every item was skipped.
//
// buildCandidate is a PURE function (no DB, no network, no clock, no logging — the caller logs
// skipped) from audited besluiten to a Turtle candidate (task 4): one shared prefix preamble
// followed by one block per rendered item, in input order.
func buildCandidate(items []AuditedBesluit) (candidate []byte, skipped []string) {
	if len(items) == 0 {
		return nil, nil
	}

	var b strings.Builder
	b.WriteString(turtlePreamble)

	rendered := 0
	for _, item := range items {
		if err := renderAuditedBesluit(&b, item); err != nil {
			skipped = append(skipped, item.Pub.ID)
			continue
		}
		rendered++
	}

	if rendered == 0 {
		return nil, skipped
	}
	return []byte(b.String()), skipped
}
