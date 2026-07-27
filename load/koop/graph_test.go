package koop

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file exercises task 4.4 (openspec/changes/load-koop-assembly) against the DOCUMENTED
// buildCandidate([]AuditedBesluit) ([]byte, error) contract — graph.go is implemented by a
// concurrent coder agent and is not visible here. Assertions are written tolerant of formatting
// (full angle-bracket IRIs vs a dedicated/shared prefix, whitespace) per design.md D6, mirroring
// load/places/render_test.go's approach for its own IRI-tolerant checks.
//
// Per openspec/changes/state-node-versioning (specs/koop-load/spec.md, design.md D3), the
// gs:locatedAt annotation is a refinable edge carrying gs:confidence (+ gs:caveat when
// confidence < 1.0) and MUST NOT carry gs:validFrom/gs:validTo — the Intervention holds no
// evolving edge at all. Tests below assert this directly rather than the earlier (now-removed)
// validFrom-stamping behavior.

// gtGSNS/gtInterventionNS/gtClaimNS/gtPlaceNS/gtActivityNS are re-derived from design.md D6 here
// (not imported from graph.go) so the test asserts against the documented IRI scheme, not
// whatever internal constant names the implementation happens to choose.
const (
	gtGSNS           = "http://gemetenstad.nl/ns#"
	gtDataNS         = "http://gemetenstad.nl/id/"
	gtInterventionNS = gtDataNS + "intervention/"
	gtClaimNS        = gtDataNS + "claim/"
	gtPlaceNS        = gtDataNS + "place/"
	gtActivityNS     = gtDataNS + "activity/"
)

// gtCaveatTerms is the controlled gs:Caveat vocabulary (ontology/vocab.ttl, shapes.ttl rule 1e):
// any gs:caveat value the builder emits must be one of these four terms.
var gtCaveatTerms = []string{"unresolvedLocation", "timeMismatch", "weakLink", "transplantOrigin"}

// gtConfidenceRe extracts the bare numeric literal following a gs:confidence predicate (the
// RDF-star annotation convention already used in load/graph/load_integration_test.go's
// wellFormed fixture, e.g. "gs:confidence 1.0" — no quotes, no datatype suffix).
var gtConfidenceRe = regexp.MustCompile(`gs:confidence\s+([0-9]+(?:\.[0-9]+)?)`)

// besluit builds a minimal AuditedBesluit for a single-item buildCandidate call: a besluit with
// the given zaaknummer/identificatie/confidence/caveats/available date. Activiteit is fixed to
// "kappen" (the only felling activiteit the resolver/assembler need map, per design.md D2/task
// 4.3) and InNoord is fixed true (buildCandidate is not responsible for scoping — task 3
// filters before assembly).
func besluit(zaaknummer, identificatie string, confidence float64, caveats []string, available string) AuditedBesluit {
	return AuditedBesluit{
		Pub: Publication{
			Zaaknummer: zaaknummer,
			Kind:       KindBesluit,
			Activiteit: "kappen",
			Available:  available,
		},
		Res: Resolved{
			Identificatie: identificatie,
			Confidence:    confidence,
			Caveats:       caveats,
			InNoord:       true,
		},
	}
}

// gtLinesContaining returns every line of turtle containing substr (mirrors
// load/places/render_test.go's linesContaining), for line-scoped assertions.
func gtLinesContaining(turtle, substr string) []string {
	var out []string
	for _, line := range strings.Split(turtle, "\n") {
		if strings.Contains(line, substr) {
			out = append(out, line)
		}
	}
	return out
}

// gtConfidenceValue extracts the first gs:confidence numeric literal in turtle as a float64, for
// value assertions that must tolerate "1" vs "1.0" formatting.
func gtConfidenceValue(t *testing.T, turtle string) float64 {
	t.Helper()
	m := gtConfidenceRe.FindStringSubmatch(turtle)
	require.NotNil(t, m, "expected a gs:confidence numeric literal in turtle:\n%s", turtle)
	v, err := strconv.ParseFloat(m[1], 64)
	require.NoError(t, err)
	return v
}

// gtFindPrefixFor returns the prefix name bound to ns in turtle's @prefix preamble (e.g.
// "intervention" for "@prefix intervention: <http://gemetenstad.nl/id/intervention/> ."), or ""
// if ns has no such binding — the builder may instead emit full angle-bracket IRIs, or bind a
// shared parent prefix (e.g. data: bound to http://gemetenstad.nl/id/) and rely on an
// escaped-slash local name (see gtAssertUnderNamespace).
func gtFindPrefixFor(turtle, ns string) string {
	for _, raw := range strings.Split(turtle, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "@prefix ") {
			continue
		}
		rest := strings.TrimPrefix(line, "@prefix ")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.TrimSuffix(val, ".")
		val = strings.TrimSpace(val)
		val = strings.TrimPrefix(val, "<")
		val = strings.TrimSuffix(val, ">")
		if val == ns {
			return name
		}
	}
	return ""
}

// gtAssertUnderNamespace asserts turtle references id under the IRI namespace ns, in whichever
// Turtle form the builder chose:
//  1. a full (angle-bracket or bare) IRI ns+id;
//  2. ns bound to its own dedicated prefix, used as name:id (the load/places convention for a
//     namespace whose local part would otherwise contain an illegal bare "/", e.g. place:);
//  3. a shared parent namespace (http://gemetenstad.nl/id/) bound to a prefix, with the entity
//     segment appended to the local name, bare or backslash-escaped (Turtle's PN_LOCAL_ESC
//     permits an escaped "/" in a prefixed-name local part).
func gtAssertUnderNamespace(t *testing.T, turtle, ns, id string) {
	t.Helper()

	full := ns + id
	if strings.Contains(turtle, full) {
		return
	}
	if name := gtFindPrefixFor(turtle, ns); name != "" && strings.Contains(turtle, name+":"+id) {
		return
	}
	if strings.HasPrefix(ns, gtDataNS) {
		suffix := strings.TrimPrefix(ns, gtDataNS) // e.g. "intervention/"
		if name := gtFindPrefixFor(turtle, gtDataNS); name != "" {
			bare := name + ":" + suffix + id
			escaped := name + ":" + strings.ReplaceAll(suffix, "/", `\/`) + id
			if strings.Contains(turtle, bare) || strings.Contains(turtle, escaped) {
				return
			}
		}
	}

	t.Fatalf("expected %q referenced under namespace %q (full IRI %q, a dedicated prefix, or a shared-prefix escaped local name), got turtle:\n%s",
		id, ns, full, turtle)
}

// TestBuildCandidate_ResolvedAddressBesluit covers scenario 1: a 0.90-confidence address-tier
// resolution renders a fully conforming Intervention/Claim/locatedAt/Place turtle, including a
// default caveat (0.9 < 1.0, satisfying shapes.ttl rule 1d) even though the resolver supplied
// none.
func TestBuildCandidate_ResolvedAddressBesluit(t *testing.T) {
	zaak := "Z2022-N002608"
	identificatie := "0363020000001234"

	items := []AuditedBesluit{besluit(zaak, identificatie, 0.9, nil, "2022-05-31")}

	turtle, skipped := buildCandidate(items)
	assert.Empty(t, skipped)
	text := string(turtle)

	// The Intervention and Claim are both keyed on the zaaknummer; gathering every line
	// mentioning it and asserting on the joined block is tolerant of predicate/line order.
	zaakBlock := strings.Join(gtLinesContaining(text, zaak), "\n")
	assert.Contains(t, zaakBlock, "gs:Intervention", "expected an Intervention for %s", zaak)
	assert.Contains(t, zaakBlock, "gs:claims", "expected a gs:claims edge from the Intervention to its Claim")
	assert.Contains(t, zaakBlock, "gs:Claim", "expected a gs:Claim declaration for %s", zaak)

	dctLines := gtLinesContaining(text, "dct:available")
	require.NotEmpty(t, dctLines, "expected a dct:available triple on the Intervention")
	assert.Contains(t, strings.Join(dctLines, "\n"), `"2022-05-31"^^xsd:date`,
		"expected the besluit's publication date on the Intervention as dct:available")

	activityLines := gtLinesContaining(text, "gs:activity")
	require.NotEmpty(t, activityLines, "expected a gs:activity triple")
	assert.Contains(t, strings.Join(activityLines, "\n"), "vellen", "kappen must map to the act:vellen activity concept")

	locatedAtLines := gtLinesContaining(text, "gs:locatedAt")
	require.NotEmpty(t, locatedAtLines, "expected a gs:locatedAt edge")
	assert.Contains(t, strings.Join(locatedAtLines, "\n"), identificatie, "gs:locatedAt must target the resolved Place")

	placeBlock := strings.Join(gtLinesContaining(text, identificatie), "\n")
	assert.Contains(t, placeBlock, "gs:Place", "expected the Place typed gs:Place for the SHACL gate (D5)")

	assert.InDelta(t, 0.9, gtConfidenceValue(t, text), 1e-9, "expected gs:confidence 0.9")

	locatedAtBlock := strings.Join(locatedAtLines, "\n")
	assert.Contains(t, locatedAtBlock, "gs:confidence", "the locatedAt annotation must carry gs:confidence")
	assert.NotContains(t, text, "gs:validFrom",
		"gs:locatedAt is a refinable edge (design.md D3): it holds and is never valid-time-versioned")

	caveatLines := gtLinesContaining(text, "gs:caveat")
	assert.NotEmpty(t, caveatLines, "confidence 0.9 < 1.0 must carry a gs:caveat (shapes.ttl rule 1d)")
	assert.Contains(t, strings.Join(caveatLines, "\n"), "unresolvedLocation",
		"expected the default caveat term when the resolver returned none")

	gtAssertUnderNamespace(t, text, gtInterventionNS, zaak)
	gtAssertUnderNamespace(t, text, gtClaimNS, zaak)
	gtAssertUnderNamespace(t, text, gtPlaceNS, identificatie)
}

// TestBuildCandidate_ExactConfidenceOmitsCaveat covers scenario 2: an exact (1.0) resolution
// omits gs:caveat entirely (shapes.ttl rule 1d only fires below 1.0).
func TestBuildCandidate_ExactConfidenceOmitsCaveat(t *testing.T) {
	zaak := "Z2022-N002700"
	identificatie := "0363020000005678"

	items := []AuditedBesluit{besluit(zaak, identificatie, 1.0, nil, "2022-06-01")}

	turtle, skipped := buildCandidate(items)
	assert.Empty(t, skipped)
	text := string(turtle)

	assert.InDelta(t, 1.0, gtConfidenceValue(t, text), 1e-9)
	assert.NotContains(t, text, "gs:caveat", "an exact (1.0) resolution must carry no gs:caveat")
}

// TestBuildCandidate_PointFloorCaveatPresent covers scenario 3: a point-in-buurt floor
// resolution (confidence 0.50) with a resolver-supplied caveat renders both the confidence and
// the caveat term.
func TestBuildCandidate_PointFloorCaveatPresent(t *testing.T) {
	zaak := "Z2022-N002701"
	identificatie := "0363020000009012"

	items := []AuditedBesluit{besluit(zaak, identificatie, 0.5, []string{"unresolvedLocation"}, "2022-06-02")}

	turtle, skipped := buildCandidate(items)
	assert.Empty(t, skipped)
	text := string(turtle)

	assert.InDelta(t, 0.5, gtConfidenceValue(t, text), 1e-9)

	caveatLines := gtLinesContaining(text, "gs:caveat")
	require.NotEmpty(t, caveatLines, "expected a gs:caveat for the point-floor resolution")
	assert.Contains(t, strings.Join(caveatLines, "\n"), "unresolvedLocation")
}

// TestBuildCandidate_CaveatValueControlled covers scenario 4: whatever gs:caveat value
// buildCandidate emits — whether passed through from the resolver or defaulted when the
// resolver supplied none — must be one of the four controlled gs:Caveat terms (shapes.ttl rule
// 1e). Only "unresolvedLocation"/"timeMismatch" are exercised as resolver-supplied inputs
// (location/types.go defines no other caveat constants today); the nil case exercises the
// default-caveat path (scenario 1).
func TestBuildCandidate_CaveatValueControlled(t *testing.T) {
	tests := []struct {
		name    string
		caveats []string
	}{
		{"resolver-supplied unresolvedLocation", []string{"unresolvedLocation"}},
		{"resolver-supplied timeMismatch", []string{"timeMismatch"}},
		{"default caveat when resolver supplied none", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zaak := "Z2022-N009999"
			identificatie := "0363020000000001"
			items := []AuditedBesluit{besluit(zaak, identificatie, 0.7, tt.caveats, "2022-01-01")}

			turtle, skipped := buildCandidate(items)
			assert.Empty(t, skipped)
			text := string(turtle)

			caveatLines := gtLinesContaining(text, "gs:caveat")
			require.NotEmpty(t, caveatLines, "confidence 0.7 < 1.0 must carry a gs:caveat")

			for _, line := range caveatLines {
				matched := false
				for _, term := range gtCaveatTerms {
					if strings.Contains(line, term) {
						matched = true
						break
					}
				}
				assert.True(t, matched, "gs:caveat value must be one of the controlled terms %v, got line: %q", gtCaveatTerms, line)
			}
		})
	}
}

// TestBuildCandidate_IRIScheme covers scenario 5: the Intervention, Claim, and Place IRIs are
// formed under the D6 http://gemetenstad.nl/id/{intervention,claim,place}/ scheme, keyed by the
// right zaaknummer / identificatie.
func TestBuildCandidate_IRIScheme(t *testing.T) {
	zaak := "Z2022-N003003"
	identificatie := "0363020000003003"

	items := []AuditedBesluit{besluit(zaak, identificatie, 0.9, nil, "2022-07-01")}

	turtle, skipped := buildCandidate(items)
	assert.Empty(t, skipped)
	text := string(turtle)

	gtAssertUnderNamespace(t, text, gtInterventionNS, zaak)
	gtAssertUnderNamespace(t, text, gtClaimNS, zaak)
	gtAssertUnderNamespace(t, text, gtPlaceNS, identificatie)
}

// TestBuildCandidate_PublicationDateAsDctAvailable covers the "Intervention carries its
// publication date as dct:available" scenario (openspec/changes/derive-coverage-audit,
// specs/koop-load/spec.md): the Intervention asserts dct:available "YYYY-MM-DD"^^xsd:date, sourced
// verbatim from Publication.Available (Dublin Core Terms, reused — not a minted gs: term), and this
// triple carries no gs:validFrom/gs:validTo — a timeless descriptive fact, never evolving state.
func TestBuildCandidate_PublicationDateAsDctAvailable(t *testing.T) {
	zaak := "Z2022-N003006"
	identificatie := "0363020000003006"
	available := "2022-09-15"

	items := []AuditedBesluit{besluit(zaak, identificatie, 1.0, nil, available)}

	turtle, skipped := buildCandidate(items)
	assert.Empty(t, skipped)
	text := string(turtle)

	dctLines := gtLinesContaining(text, "dct:available")
	require.NotEmpty(t, dctLines, "expected a dct:available triple on the Intervention")
	dctBlock := strings.Join(dctLines, "\n")
	assert.Contains(t, dctBlock, `"`+available+`"^^xsd:date`,
		"expected the besluit's publication date rendered as an xsd:date literal")

	assert.NotContains(t, text, "gs:validFrom",
		"dct:available is a timeless descriptive fact (design.md / RDF_MODELING.md §1): never gs:validFrom-stamped")
	assert.NotContains(t, text, "gs:validTo",
		"dct:available must never carry gs:validTo either")
}

// TestBuildCandidate_ClaimHasNoObligationCount covers scenario 6: this phase carries no
// obligation count on the Claim (design.md Non-Goals; task 4.1 says "no count") — the Claim's `a
// gs:Claim` triple must terminate the statement, not chain a count (or any other) predicate
// after it.
func TestBuildCandidate_ClaimHasNoObligationCount(t *testing.T) {
	zaak := "Z2022-N003004"
	identificatie := "0363020000003004"

	items := []AuditedBesluit{besluit(zaak, identificatie, 1.0, nil, "2022-07-02")}

	turtle, skipped := buildCandidate(items)
	assert.Empty(t, skipped)
	text := string(turtle)

	claimLines := gtLinesContaining(text, "a gs:Claim")
	require.NotEmpty(t, claimLines, "expected an `a gs:Claim` declaration")
	for _, line := range claimLines {
		trimmed := strings.TrimSpace(line)
		assert.True(t, strings.HasSuffix(trimmed, "."),
			"the Claim's `a gs:Claim` triple must terminate the statement (no obligation count or other predicate chained after it), got line: %q", trimmed)
	}
}

// TestBuildCandidate_EmptyInput covers scenario 7: buildCandidate(nil) must not error, and (since
// there is nothing to assemble) must emit no Intervention.
func TestBuildCandidate_EmptyInput(t *testing.T) {
	turtle, skipped := buildCandidate(nil)
	assert.Empty(t, skipped)
	assert.Nil(t, turtle, "empty items must yield a nil candidate")
	assert.NotContains(t, string(turtle), "gs:Intervention", "no items means no Intervention triples")
}

// TestBuildCandidate_LocatedAtNeverCarriesValidFrom covers scenario 8 under the
// state-node-versioning contract (specs/koop-load/spec.md, design.md D3): gs:locatedAt is a
// refinable-metadata edge, not a valid-time one, so buildCandidate must NEVER emit gs:validFrom
// on it, for a besluit with a present Available date. (An absent/empty Available is no longer
// exercised here — the Turtle-injection fix requires Available to parse as a canonical
// "2006-01-02" date, and "" fails that parse; that skip-on-invalid-date behavior is covered by
// TestBuildCandidate_InvalidAvailableDateIsSkipped instead.)
func TestBuildCandidate_LocatedAtNeverCarriesValidFrom(t *testing.T) {
	zaak := "Z2022-N003005"
	identificatie := "0363020000003005"

	items := []AuditedBesluit{besluit(zaak, identificatie, 0.7, []string{"timeMismatch"}, "2022-05-31")}

	turtle, skipped := buildCandidate(items)
	assert.Empty(t, skipped)
	text := string(turtle)

	assert.NotContains(t, text, "gs:validFrom",
		"gs:locatedAt is a refinable edge (design.md D3) and must never carry gs:validFrom, regardless of Available")
	assert.NotContains(t, text, "gs:validTo",
		"gs:locatedAt must never carry gs:validTo either — it is never a valid-time edge")

	assert.InDelta(t, 0.7, gtConfidenceValue(t, text), 1e-9)
	caveatLines := gtLinesContaining(text, "gs:caveat")
	require.NotEmpty(t, caveatLines)
	assert.Contains(t, strings.Join(caveatLines, "\n"), "timeMismatch")
}

// TestBuildCandidate_InvalidAvailableDateIsSkipped covers the CRITICAL Turtle-injection fix
// (security review): a besluit's Publication.Available is externally sourced (KOOP SRU XML,
// load/koop/parse.go), only whitespace-trimmed, and previously flowed verbatim into a Turtle
// string literal — so a crafted value containing a `"` could terminate the literal early and
// splice arbitrary triples into the assembled candidate (which is POSTed straight to Fuseki), or a
// stray quote could break the whole batch's Turtle syntax. renderAuditedBesluit now requires
// Available to parse as a canonical "2006-01-02" date (mirroring load/koop/stage.go's identical
// guard for the Postgres path) before emitting it — so an injection attempt, or any other
// non-YYYY-MM-DD value, is never written into the literal at all: buildCandidate's existing
// skip-and-record contract surfaces the failure via `skipped` instead.
func TestBuildCandidate_InvalidAvailableDateIsSkipped(t *testing.T) {
	tests := []struct {
		name      string
		available string
	}{
		{"quote-terminated injection attempt", `2024-01-01"^^xsd:date . <http://evil/x> a <http://evil/y> . x "`},
		{"empty (absent) date", ""},
		{"non-date garbage", "not-a-date"},
		{"wrong format (D/M/Y)", "31/05/2022"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zaak := "Z2022-N003007"
			identificatie := "0363020000003007"
			item := besluit(zaak, identificatie, 0.9, nil, tt.available)
			item.Pub.ID = "gmb-2022-900010"

			candidate, skipped := buildCandidate([]AuditedBesluit{item})
			text := string(candidate)

			require.Equal(t, []string{item.Pub.ID}, skipped,
				"expected the besluit with an invalid Available date to be skipped, got turtle:\n%s", text)
			assert.Nil(t, candidate, "the only item in the batch was invalid, so the candidate must be nil")

			assert.NotContains(t, text, "evil", "no injected triple must ever reach the candidate")
			assert.NotContains(t, text, zaak, "a skipped item's zaaknummer must not appear in the candidate")
		})
	}
}

// TestBuildCandidate_SkipsItemWithUnsafeIRI covers the resilience follow-up (load-koop-assembly):
// buildCandidate must skip (not abort on) a single item whose zaaknummer would mint an unsafe
// IRI, recording its gmb ID in skipped, while still rendering every other, valid item in the same
// batch. The bad zaaknummer here contains a bare space — assertSafeIRI (graph.go) rejects any rune
// <= 0x20, mirroring load/graph/signature.go's own assertSafeIRI.
func TestBuildCandidate_SkipsItemWithUnsafeIRI(t *testing.T) {
	goodZaak := "Z2022-N003100"
	goodIdentificatie := "0363020000003100"
	goodItem := besluit(goodZaak, goodIdentificatie, 0.9, nil, "2022-08-01")
	goodItem.Pub.ID = "gmb-2022-900001"

	badZaak := "Z2022 N1" // space: assertSafeIRI rejects any rune <= 0x20
	badItem := besluit(badZaak, "0363020000009999", 0.9, nil, "2022-08-01")
	badItem.Pub.ID = "gmb-2022-900002"

	items := []AuditedBesluit{goodItem, badItem}

	candidate, skipped := buildCandidate(items)
	text := string(candidate)

	require.Equal(t, []string{badItem.Pub.ID}, skipped,
		"expected exactly the bad item's gmb ID in skipped, got turtle:\n%s", text)

	gtAssertUnderNamespace(t, text, gtInterventionNS, goodZaak)
	assert.NotContains(t, text, badZaak, "the skipped item's zaaknummer must not appear in the candidate")
}

// TestBuildCandidate_AllSkippedReturnsNil covers the "all items skipped" edge of the same
// resilience follow-up: when the only item in the batch is unsafe, buildCandidate must return a
// nil candidate (mirroring the empty-input case, scenario 7) rather than a preamble-only Turtle
// document, while still reporting the skip.
func TestBuildCandidate_AllSkippedReturnsNil(t *testing.T) {
	badItem := besluit("Z2022 N1", "0363020000009999", 0.9, nil, "2022-08-01")
	badItem.Pub.ID = "gmb-2022-900003"

	candidate, skipped := buildCandidate([]AuditedBesluit{badItem})

	assert.Nil(t, candidate, "candidate must be nil when every item is skipped")
	assert.Equal(t, []string{badItem.Pub.ID}, skipped)
}
