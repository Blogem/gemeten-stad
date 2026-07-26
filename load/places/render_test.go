package places

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadTS is the fixed, deterministic load-run timestamp used across scenarios (task 2.4):
// a live row's gs:validFrom is stamped with this value (D5).
var loadTS = time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

// softDeletedAt is the fixed source_deleted_at used for soft-deleted rows: a soft-deleted
// row's gs:validFrom is stamped with THIS value, not loadTS (D5).
var softDeletedAt = time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)

func strPtr(s string) *string { return &s }

func timePtr(tm time.Time) *time.Time { return &tm }

// linesContaining returns every line of turtle containing substr, for line-scoped assertions
// (e.g. "no gs:within line starts with the wijk IRI").
func linesContaining(turtle, substr string) []string {
	var out []string
	for _, line := range strings.Split(turtle, "\n") {
		if strings.Contains(line, substr) {
			out = append(out, line)
		}
	}
	return out
}

// refToken returns whichever representation of the Place minted for identificatie actually
// appears in turtle: the full IRI mintPlaceIRI returns, or the place:<identificatie> prefixed
// form (design D1/D5 leaves the exact prefix binding as an implementation detail settled in
// code review). Empty string if neither is present.
func refToken(turtle, identificatie string) string {
	full := mintPlaceIRI(identificatie)
	if strings.Contains(turtle, full) {
		return full
	}
	prefixed := "place:" + identificatie
	if strings.Contains(turtle, prefixed) {
		return prefixed
	}
	return ""
}

// assertWithinEdge asserts the rendered turtle contains a gs:within line linking the buurt
// Place to the wijk Place (scenario: "a buurt is contained in its wijk").
func assertWithinEdge(t *testing.T, turtle, buurtID, wijkID string) {
	t.Helper()

	buurtRef := refToken(turtle, buurtID)
	wijkRef := refToken(turtle, wijkID)
	require.NotEmpty(t, buurtRef, "buurt Place IRI/prefixed form must appear in turtle for %s", buurtID)
	require.NotEmpty(t, wijkRef, "wijk Place IRI/prefixed form must appear in turtle for %s", wijkID)

	for _, line := range linesContaining(turtle, "gs:within") {
		if strings.Contains(line, buurtRef) && strings.Contains(line, wijkRef) {
			return
		}
	}
	t.Fatalf("expected a gs:within line linking buurt %s to wijk %s, got turtle:\n%s", buurtID, wijkID, turtle)
}

// TestMintPlaceIRI covers scenario 7 (IRI minting): the Place IRI is minted under the
// http://gemetenstad.nl/id/place/ instance namespace, keyed on the gebieden identificatie (D1).
func TestMintPlaceIRI(t *testing.T) {
	got := mintPlaceIRI("03630000000123")
	assert.Equal(t, "http://gemetenstad.nl/id/place/03630000000123", got)
}

// TestRenderPlaces covers task 2.4 scenarios 1, 3, 4, 5, 7, 8 via a table of row inputs and the
// substrings/dangling list the rendered turtle must (or must not) contain. Scenarios 2 (wijk
// never a gs:within subject) and 6 (label escaping) need line-scoped/string-shape assertions
// beyond simple Contains/NotContains and are covered by their own dedicated tests below.
func TestRenderPlaces(t *testing.T) {
	tests := []struct {
		name string

		buurten []buurtRow
		wijken  []wijkRow

		wantContains    []string
		wantNotContains []string
		wantDangling    []string

		// withinEdge, if set, asserts a gs:within line links buurtID -> wijkID.
		withinEdge *[2]string
	}{
		{
			// Scenario 1 + 7 + 8: a live buurt within its wijk, both live. Also exercises the
			// "no geometry / no confidence" invariant (scenario 8) since this is the most
			// representative render.
			name: "live buurt within its wijk is seeded active with a gs:within edge",
			buurten: []buurtRow{
				{identificatie: "03630000000121", naam: "Buurt Een", ligtInWijkID: strPtr("03630000001")},
			},
			wijken: []wijkRow{
				{identificatie: "03630000001", naam: "Wijk Een"},
			},
			wantContains: []string{
				"a gs:Place",
				"Buurt Een",
				"Wijk Een",
				"gs:active true",
				`gs:validFrom "2026-07-26"^^xsd:date`,
			},
			wantNotContains: []string{
				"gs:confidence",
				"gs:evidence",
				"POLYGON",
				"MULTIPOLYGON",
				"geo:",
				"wkt",
				"WKT",
			},
			wantDangling: nil,
			withinEdge:   &[2]string{"03630000000121", "03630000001"},
		},
		{
			// Scenario 3: a soft-deleted buurt is still seeded, but inactive, with validFrom
			// equal to source_deleted_at (not loadTS). ligtInWijkID resolves so this case is
			// isolated from the dangling-containment scenarios below.
			name: "soft-deleted buurt is seeded inactive with validFrom = source_deleted_at",
			buurten: []buurtRow{
				{
					identificatie:   "03630000000122",
					naam:            "Buurt Twee",
					ligtInWijkID:    strPtr("03630000001"),
					sourceDeletedAt: timePtr(softDeletedAt),
				},
			},
			wijken: []wijkRow{
				{identificatie: "03630000001", naam: "Wijk Een"},
			},
			wantContains: []string{
				"a gs:Place",
				"Buurt Twee",
				"gs:active false",
				`gs:validFrom "2025-03-01"^^xsd:date`,
			},
			wantNotContains: []string{
				`gs:active true`,
			},
			wantDangling: nil,
			withinEdge:   &[2]string{"03630000000122", "03630000001"},
		},
		{
			// Scenario 3, wijk side: soft-deletion applies identically to a wijk row.
			name: "soft-deleted wijk is seeded inactive with validFrom = source_deleted_at",
			wijken: []wijkRow{
				{
					identificatie:   "03630000002",
					naam:            "Wijk Twee",
					sourceDeletedAt: timePtr(softDeletedAt),
				},
			},
			wantContains: []string{
				"a gs:Place",
				"Wijk Twee",
				"gs:active false",
				`gs:validFrom "2025-03-01"^^xsd:date`,
			},
			wantDangling: nil,
		},
		{
			// Scenario 4: a buurt with a null ligtInWijkID is still seeded as a full Place
			// (label + gs:active) but carries no gs:within edge, and is reported dangling.
			name: "buurt with null ligtInWijkID is seeded without a gs:within edge and reported dangling",
			buurten: []buurtRow{
				{identificatie: "03630000000123", naam: "Buurt Drie", ligtInWijkID: nil},
			},
			wantContains: []string{
				"a gs:Place",
				"Buurt Drie",
				"gs:active true",
				`gs:validFrom "2026-07-26"^^xsd:date`,
			},
			wantNotContains: []string{"gs:within"},
			wantDangling:    []string{"03630000000123"},
		},
		{
			// Scenario 5: a buurt whose ligtInWijkID does not resolve to any projected wijk is
			// seeded the same way as the null case: full Place, no gs:within, dangling.
			name: "buurt with unresolvable ligtInWijkID is seeded without a gs:within edge and reported dangling",
			buurten: []buurtRow{
				{identificatie: "03630000000124", naam: "Buurt Vier", ligtInWijkID: strPtr("nonexistent-wijk")},
			},
			wijken: []wijkRow{
				{identificatie: "03630000001", naam: "Wijk Een"},
			},
			wantContains: []string{
				"a gs:Place",
				"Buurt Vier",
				"gs:active true",
			},
			wantNotContains: []string{"gs:within"},
			wantDangling:    []string{"03630000000124"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			turtle, dangling := renderPlaces(tt.buurten, tt.wijken, loadTS)
			text := string(turtle)

			for _, want := range tt.wantContains {
				assert.Contains(t, text, want)
			}
			for _, notWant := range tt.wantNotContains {
				assert.NotContains(t, text, notWant)
			}
			assert.ElementsMatch(t, tt.wantDangling, dangling)

			if tt.withinEdge != nil {
				assertWithinEdge(t, text, tt.withinEdge[0], tt.withinEdge[1])
			}
		})
	}
}

// TestRenderPlaces_WijkNeverSubjectOfWithin covers scenario 2: wijken are the top of the
// skeleton and carry no gs:within edge of their own, even though a buurt gs:within-links to
// them. No gs:within line may start with the wijk's Place IRI or prefixed form.
func TestRenderPlaces_WijkNeverSubjectOfWithin(t *testing.T) {
	buurten := []buurtRow{
		{identificatie: "03630000000121", naam: "Buurt Een", ligtInWijkID: strPtr("03630000001")},
	}
	wijken := []wijkRow{
		{identificatie: "03630000001", naam: "Wijk Een"},
	}

	turtle, dangling := renderPlaces(buurten, wijken, loadTS)
	text := string(turtle)
	assert.Empty(t, dangling)

	wijkIRI := mintPlaceIRI("03630000001")
	wijkPrefixed := "place:03630000001"

	withinLines := linesContaining(text, "gs:within")
	require.NotEmpty(t, withinLines, "expected at least one gs:within line for the buurt->wijk edge")

	for _, line := range withinLines {
		trimmed := strings.TrimSpace(line)
		startsAsWijkSubject := strings.HasPrefix(trimmed, wijkIRI) ||
			strings.HasPrefix(trimmed, "<"+wijkIRI+">") ||
			strings.HasPrefix(trimmed, wijkPrefixed+" ") ||
			strings.HasPrefix(trimmed, wijkPrefixed+";")
		assert.False(t, startsAsWijkSubject, "wijk Place must never be the subject of a gs:within edge, got line: %q", line)
	}
}

// TestRenderPlaces_LabelEscaping covers scenario 6: a naam containing a double quote and a
// backslash must be escaped in the emitted rdfs:label literal, not written raw (which would
// break the Turtle literal or allow injection).
func TestRenderPlaces_LabelEscaping(t *testing.T) {
	naam := `A "quoted" \ name`
	buurten := []buurtRow{
		{identificatie: "03630000000199", naam: naam},
	}

	turtle, dangling := renderPlaces(buurten, nil, loadTS)
	text := string(turtle)

	assert.ElementsMatch(t, []string{"03630000000199"}, dangling)

	assert.Contains(t, text, "rdfs:label")
	// The raw, unescaped quoted substring must never appear: it would either terminate the
	// literal early or otherwise break out of the Turtle string.
	assert.NotContains(t, text, `"quoted"`, "raw unescaped quotes must not appear in the rendered label")
	assert.Contains(t, text, `\"quoted\"`, "embedded double quotes must be escaped")
	assert.Contains(t, text, `\\`, "the embedded backslash must be escaped")
}
