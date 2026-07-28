package coverage

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// gsNS/dataNS/provNS mirror the settled RDF namespaces (load/graph/signature.go, load/koop/graph.go):
// gs: for the TBox, data: for every instance IRI change detection is keyed on (design.md D1),
// prov: for the standard PROV vocabulary this package's gs:evidence/gs:validFrom sit alongside.
const (
	gsNS   = "http://gemetenstad.nl/ns#"
	dataNS = "http://gemetenstad.nl/id/"
	provNS = "http://www.w3.org/ns/prov#"

	auditlinkNS = dataNS + "auditlink/"
	fellingNS   = dataNS + "felling/"
)

// turtlePreamble declares the prefixes every assembled item needs: gs: (TBox), data: (documenting
// the instance-IRI namespace — instance IRIs below are written as full angle-bracket IRIs, not
// prefixed names, mirroring load/koop/graph.go's rationale: a zaaknummer/content-key is
// caller-supplied, untrusted-ish input, and a bracketed IRI needs no prefixed-name-local-part
// validity check the way a prefixed token would), prov: (prov:wasDerivedFrom), and xsd: (the
// gs:validFrom date literal and, via load/koop's formatConfidence-style rendering, gs:confidence).
const turtlePreamble = `@prefix gs: <` + gsNS + `> .
@prefix data: <` + dataNS + `> .
@prefix prov: <` + provNS + `> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .

`

// disallowedIRIChars mirrors load/graph/signature.go's assertSafeIRI forbidden set: the characters
// a Turtle IRIREF may never contain unescaped.
const disallowedIRIChars = "<>\"{}|^`\\"

// assertSafeIRI rejects id if it contains any character not permitted raw inside a Turtle IRIREF
// (control chars, space, or one of disallowedIRIChars) — the single choke point every
// caller-supplied zaaknummer/IRI passes through before it is written into an angle-bracket IRI
// (mirrors load/koop/graph.go's assertSafeIRI and load/graph/signature.go's assertSafeIRI).
func assertSafeIRI(id string) error {
	for _, r := range id {
		if r <= 0x20 || strings.ContainsRune(disallowedIRIChars, r) {
			return fmt.Errorf("coverage: unsafe IRI component %q: contains disallowed character %U", id, r)
		}
	}
	return nil
}

// escapeTurtleString escapes s for use inside a Turtle STRING_LITERAL_QUOTE ("..."), so a
// gs:evidence value containing a backslash, double quote, or raw newline/carriage-return/tab never
// breaks the surrounding literal.
func escapeTurtleString(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return r.Replace(s)
}

// mintAnchorIRI returns the stable per-permit gs:AuditLink anchor IRI (design.md D1):
// data:auditlink/<zaaknummer>.
func mintAnchorIRI(zaaknummer string) string { return auditlinkNS + zaaknummer }

// mintPeriodIRI returns the content-keyed gs:CoveragePeriod IRI for one derive outcome (design.md
// D7): data:auditlink/<zaaknummer>/<content-key>.
func mintPeriodIRI(zaaknummer, contentKey string) string {
	return auditlinkNS + zaaknummer + "/" + contentKey
}

// mintFellingIRI returns the gs:Felling IRI for a kapenherplant record id (model-felled-trees D1,
// "Open Questions": the felling is keyed by record id, 1:1 with the felling act): data:felling/<id>.
// MUST match ontology/shapes.ttl's gs:includesFelling sh:pattern and the bomen graph-projection's own
// minting scheme — this package never invents its own felling IRI scheme.
func mintFellingIRI(id string) string { return fellingNS + id }

// mintObservationIRI returns the content-addressed gs:Observation IRI for a matched permit
// (model-felled-trees D2): data:observation/<zaaknummer>/<felling-set-key>, where fellingSetKey is
// FellingSetKey's hash over the permit's sorted assigned gs:Felling IRIs. Because the IRI is a pure
// function of the felling set, an unchanged set reuses the same IRI (a true no-op) and a changed set
// (add/remove/swap, even at the same count) always mints a new one.
func mintObservationIRI(zaaknummer, fellingSetKey string) string {
	return dataNS + "observation/" + zaaknummer + "/" + fellingSetKey
}

// granularityToken maps a Tier to the gs:<tier> token gs:granularity takes (ontology/ontology.ttl's
// three controlled gs:address/gs:postcode/gs:buurt individuals). Returns an error for any other
// value, including the zero value — a matched Outcome without a valid Tier is a caller bug the
// assembler must fail loud on, not silently default.
func granularityToken(t Tier) (string, error) {
	switch t {
	case TierAddress:
		return "gs:address", nil
	case TierPostcode:
		return "gs:postcode", nil
	case TierBuurt:
		return "gs:buurt", nil
	default:
		return "", fmt.Errorf("coverage: assemble: invalid granularity tier %q", t)
	}
}

// formatConfidence renders c (expected in [0,1]) as a Turtle decimal literal with exactly two
// decimal places (e.g. 0.70, 1.00) — Score/ContentKey already round confidence to 2dp, so this is
// a pure format, not a second rounding. A fixed decimal point (unlike a whole-number '1' or '0')
// keeps the emitted term unambiguously xsd:decimal, mirroring load/koop/graph.go's
// formatConfidence.
func formatConfidence(c float64) string {
	return strconv.FormatFloat(c, 'f', 2, 64)
}

// formatValidFrom renders t as an xsd:date literal (YYYY-MM-DD). ValidFrom is run world-time — a
// calendar date, not an instant — so t is normalized to UTC before formatting to avoid a
// caller-local timezone shifting the emitted day.
func formatValidFrom(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// Assembled is one permit's rendered derive outcome: the write-once anchor plus the current
// content-keyed gs:CoveragePeriod (design.md D1/D7). Assemble renders one such block per item.
type Assembled struct {
	Zaaknummer      string
	InterventionIRI string
	Outcome         Outcome
	ValidFrom       time.Time // run world-time; emitted as xsd:date
	Evidence        string    // human-readable summary (always present)
	DerivedFrom     []string  // IRIs for prov:wasDerivedFrom (permit + registry rows)
}

// validate checks the structural preconditions Assemble needs to render valid turtle: safe IRI
// components, non-empty Evidence (the shape requires gs:evidence unconditionally), and — for a
// matched outcome only — a non-empty ObservationIRI and a valid Granularity (the shape's matched
// branch requires both). Fails loud rather than rendering a period doomed to be rejected by the
// SHACL gate downstream.
func (a Assembled) validate() error {
	if a.Zaaknummer == "" {
		return fmt.Errorf("coverage: assemble: empty Zaaknummer")
	}
	if err := assertSafeIRI(a.Zaaknummer); err != nil {
		return fmt.Errorf("coverage: assemble: zaaknummer: %w", err)
	}
	if a.InterventionIRI == "" {
		return fmt.Errorf("coverage: assemble %s: empty InterventionIRI", a.Zaaknummer)
	}
	if err := assertSafeIRI(a.InterventionIRI); err != nil {
		return fmt.Errorf("coverage: assemble %s: InterventionIRI: %w", a.Zaaknummer, err)
	}
	if a.Evidence == "" {
		return fmt.Errorf("coverage: assemble %s: empty Evidence", a.Zaaknummer)
	}
	for _, d := range a.DerivedFrom {
		if err := assertSafeIRI(d); err != nil {
			return fmt.Errorf("coverage: assemble %s: DerivedFrom: %w", a.Zaaknummer, err)
		}
	}

	if a.Outcome.Matched {
		if a.Outcome.ObservationIRI == "" {
			return fmt.Errorf("coverage: assemble %s: matched outcome missing ObservationIRI", a.Zaaknummer)
		}
		if err := assertSafeIRI(a.Outcome.ObservationIRI); err != nil {
			return fmt.Errorf("coverage: assemble %s: ObservationIRI: %w", a.Zaaknummer, err)
		}
		if len(a.Outcome.FellingIRIs) == 0 {
			// The shape requires a matched gs:Observation to carry gs:includesFelling >= 1
			// (ontology/shapes.ttl); a matched outcome with zero felling IRIs is a caller bug
			// this must fail loud on rather than render a period doomed to be SHACL-rejected.
			return fmt.Errorf("coverage: assemble %s: matched outcome has zero FellingIRIs", a.Zaaknummer)
		}
		for _, f := range a.Outcome.FellingIRIs {
			if err := assertSafeIRI(f); err != nil {
				return fmt.Errorf("coverage: assemble %s: FellingIRI: %w", a.Zaaknummer, err)
			}
		}
		if _, err := granularityToken(a.Outcome.Granularity); err != nil {
			return fmt.Errorf("coverage: assemble %s: %w", a.Zaaknummer, err)
		}
	}
	return nil
}

// renderDerivedFrom appends a prov:wasDerivedFrom clause listing every DerivedFrom IRI, or nothing
// if the list is empty (a caller omitting provenance is not this function's concern to invent).
func renderDerivedFrom(b *strings.Builder, derivedFrom []string) {
	if len(derivedFrom) == 0 {
		return
	}
	refs := make([]string, len(derivedFrom))
	for i, d := range derivedFrom {
		refs[i] = "<" + d + ">"
	}
	fmt.Fprintf(b, " ;\n    prov:wasDerivedFrom %s", strings.Join(refs, ", "))
}

// renderAssembled writes one item's turtle block into b: the write-once anchor (always rendered —
// the load path's unchanged-signature no-op handles idempotency at the store, per design.md D7),
// then the current gs:CoveragePeriod — matched (model-felled-trees D2/D4: mints the
// content-addressed gs:Observation with its gs:includesFelling members, carries
// gs:linksObservation/gs:confidence/gs:granularity/gs:caveat) or no-source (D3: gs:noSourceFound
// true only, no Observation, no confidence/granularity). Never emits gs:validTo — the writer stamps
// that on close (state-node-versioning).
func renderAssembled(b *strings.Builder, item Assembled) error {
	anchorIRI := mintAnchorIRI(item.Zaaknummer)
	periodIRI := mintPeriodIRI(item.Zaaknummer, ContentKey(item.Outcome))

	fmt.Fprintf(b, "<%s> a gs:AuditLink ;\n    gs:coversIntervention <%s> .\n\n", anchorIRI, item.InterventionIRI)

	if item.Outcome.Matched {
		granularity, err := granularityToken(item.Outcome.Granularity)
		if err != nil {
			// Unreachable: validate() already rejected this outcome. Guarded so a future caller
			// bypassing validate() still fails loud instead of rendering a malformed granularity.
			return fmt.Errorf("coverage: render %s: %w", item.Zaaknummer, err)
		}

		fellingIRIs := sortedStrings(item.Outcome.FellingIRIs)
		refs := make([]string, len(fellingIRIs))
		for i, f := range fellingIRIs {
			refs[i] = "<" + f + ">"
		}
		fmt.Fprintf(b, "<%s> a gs:Observation ;\n    gs:includesFelling %s .\n\n",
			item.Outcome.ObservationIRI, strings.Join(refs, ", "))

		fmt.Fprintf(b, "<%s> a gs:CoveragePeriod ;\n", periodIRI)
		fmt.Fprintf(b, "    gs:versionOf <%s> ;\n", anchorIRI)
		fmt.Fprintf(b, "    gs:validFrom \"%s\"^^xsd:date ;\n", formatValidFrom(item.ValidFrom))
		fmt.Fprintf(b, "    gs:linksObservation <%s> ;\n", item.Outcome.ObservationIRI)
		fmt.Fprintf(b, "    gs:confidence %s ;\n", formatConfidence(item.Outcome.Confidence))
		fmt.Fprintf(b, "    gs:granularity %s", granularity)

		if caveats := sortedCaveats(item.Outcome.Caveats); len(caveats) > 0 {
			tokens := make([]string, len(caveats))
			for i, c := range caveats {
				tokens[i] = "gs:" + c
			}
			fmt.Fprintf(b, " ;\n    gs:caveat %s", strings.Join(tokens, ", "))
		}

		fmt.Fprintf(b, " ;\n    gs:evidence \"%s\"", escapeTurtleString(item.Evidence))
		renderDerivedFrom(b, item.DerivedFrom)
		b.WriteString(" .\n\n")
		return nil
	}

	fmt.Fprintf(b, "<%s> a gs:CoveragePeriod ;\n", periodIRI)
	fmt.Fprintf(b, "    gs:versionOf <%s> ;\n", anchorIRI)
	fmt.Fprintf(b, "    gs:validFrom \"%s\"^^xsd:date ;\n", formatValidFrom(item.ValidFrom))
	fmt.Fprintf(b, "    gs:noSourceFound true")
	fmt.Fprintf(b, " ;\n    gs:evidence \"%s\"", escapeTurtleString(item.Evidence))
	renderDerivedFrom(b, item.DerivedFrom)
	b.WriteString(" .\n\n")
	return nil
}

// sortedStrings returns a sorted copy of ss, or nil for an empty input — the shared rendering-order
// helper for both gs:caveat (sortedCaveats) and gs:includesFelling, so re-runs emit byte-stable
// turtle regardless of the input slice's original order (ContentKey/FellingSetKey sort independently
// for hashing purposes; this is purely about deterministic rendering).
func sortedStrings(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	sorted := append([]string(nil), ss...)
	sort.Strings(sorted)
	return sorted
}

// sortedCaveats returns a sorted copy of caveats, so gs:caveat's rendering order is deterministic.
func sortedCaveats(caveats []string) []string {
	return sortedStrings(caveats)
}

// Assemble renders one turtle document for a whole derive run: a shared prefix preamble, then per
// item, the write-once gs:AuditLink anchor and the current gs:CoveragePeriod (matched or
// no-source), per design.md D1–D4/D7. Returns (nil, nil) for an empty items slice. Returns an
// error — rendering nothing — the moment any item fails validate(), so a single malformed outcome
// never produces a partial turtle document (the P12 load/graph.Load gate is the SHACL-level
// backstop; this is the assembler's own fail-loud input gate).
func Assemble(items []Assembled) ([]byte, error) {
	if len(items) == 0 {
		return nil, nil
	}

	var b strings.Builder
	b.WriteString(turtlePreamble)

	for _, item := range items {
		if err := item.validate(); err != nil {
			return nil, err
		}
		if err := renderAssembled(&b, item); err != nil {
			return nil, err
		}
	}

	return []byte(b.String()), nil
}
