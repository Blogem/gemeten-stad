package bomen

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// model-felled-trees task 2.3: unit tests for the bomen -> graph turtle assembler (task 2.1),
// exercised as a PURE function over felled kapenherplant rows — the coder's implementation is not
// visible to this (pass 1) worktree, so these tests are written against
// openspec/changes/model-felled-trees/specs/bomen-load/spec.md's scenarios and design.md D1/D4,
// tolerant of exact turtle formatting (full angle-bracket IRIs vs a prefix), mirroring
// load/koop/graph_test.go's approach for its own concurrently-implemented buildCandidate.
//
// TODO(pass-2): felledRowFixture (the input row shape) and buildFellingCandidate (the assembler
// function name/signature) below are BEST-GUESS names chosen to mirror load/koop/graph.go's
// buildCandidate([]AuditedBesluit) ([]byte, error) convention, per the task contract's "prefer
// matching the koop assembler's naming convention" guidance. Once the coder's actual bomen graph
// projection (task 2.1/2.2) is visible, replace:
//   1. felledRowFixture with whichever row type the real assembler consumes (a Go struct read back
//      from the loaded kapenherplant table's id/boomId/kapmaatregelDatumUitgevoerd columns, or the
//      same map[string]any landed-row shape load/koop's buildCandidate consumes — check which one
//      the coder chose).
//   2. the buildFellingCandidate(...) call sites below with the coder's real function name/signature.
// The turtle-content assertions themselves (data:tree/<boomId> a gs:Tree, data:felling/<id> a
// gs:Felling ; gs:felledTree ... ; gs:felledOn "..."^^xsd:date, only-felled-rows-projected) are
// spec-derived (bomen-load/spec.md) and should not need to change once the wiring is fixed.

// felledRowFixture is this test file's local stand-in for one kapenherplant row's fields the
// assembler needs: id (the felling's own record id, model-felled-trees design.md's "Felling
// identity key"), boomId (the felled tree's identity), and FelledOn — nil for a row that was NEVER
// felled (kapmaatregelDatumUitgevoerd IS NULL), a non-nil date for a felled row.
type felledRowFixture struct {
	ID       string
	BoomID   string
	FelledOn *time.Time
}

func felledOn(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

// treeNS/fellingNS mirror the settled namespaces (ontology/ontology.ttl, ontology/shapes.ttl):
// gs:Tree instances live under tree/, gs:Felling instances under felling/.
const (
	graphTestTreeNS    = "http://gemetenstad.nl/id/tree/"
	graphTestFellingNS = "http://gemetenstad.nl/id/felling/"
)

// gtLinesContaining returns every line of turtle containing substr (mirrors
// load/koop/graph_test.go's gtLinesContaining / load/places/render_test.go's linesContaining), for
// line-scoped assertions tolerant of predicate/line order.
func gtLinesContaining(turtle, substr string) []string {
	var out []string
	for _, line := range strings.Split(turtle, "\n") {
		if strings.Contains(line, substr) {
			out = append(out, line)
		}
	}
	return out
}

// gtAssertUnderNamespace asserts turtle references id under the IRI namespace ns, in whichever
// Turtle form the builder chose (a full angle-bracket/bare IRI, or a dedicated prefix) — mirrors
// load/koop/graph_test.go's gtAssertUnderNamespace, trimmed to the two forms this package's
// namespaces (tree/, felling/) are likely to need (no shared-parent-prefix escaped-local-name case,
// since load/koop's own precedent uses full IRIs for exactly this kind of untrusted-ish identifier).
func gtAssertUnderNamespace(t *testing.T, turtle, ns, id string) {
	t.Helper()

	full := ns + id
	if strings.Contains(turtle, full) {
		return
	}

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
		if val == ns && strings.Contains(turtle, name+":"+id) {
			return
		}
	}

	t.Fatalf("expected %q referenced under namespace %q (full IRI %q or a dedicated prefix), got turtle:\n%s",
		id, ns, full, turtle)
}

// TestBuildFellingCandidate_FelledRowProjectsTreeAndFelling covers bomen-load/spec.md's scenario "A
// felled tree is projected as a felling event": a felled row (non-nil FelledOn) yields both
// `data:tree/<boomId> a gs:Tree` and `data:felling/<id> a gs:Felling` written through the shared
// candidate.
func TestBuildFellingCandidate_FelledRowProjectsTreeAndFelling(t *testing.T) {
	rows := []felledRowFixture{
		{ID: "kap-a", BoomID: "stam-1", FelledOn: felledOn(2023, 2, 1)},
	}

	turtle := string(buildFellingCandidate(rows))

	gtAssertUnderNamespace(t, turtle, graphTestTreeNS, "stam-1")
	gtAssertUnderNamespace(t, turtle, graphTestFellingNS, "kap-a")

	treeBlock := strings.Join(gtLinesContaining(turtle, "stam-1"), "\n")
	assert.Contains(t, treeBlock, "gs:Tree", "expected the felled tree typed gs:Tree")

	fellingBlock := strings.Join(gtLinesContaining(turtle, "kap-a"), "\n")
	assert.Contains(t, fellingBlock, "gs:Felling", "expected the felling typed gs:Felling")
}

// TestBuildFellingCandidate_FellingReferencesItsTree covers "gs:felledTree names the felled tree":
// the felling's gs:felledTree object must be exactly the SAME tree IRI minted for the row's boomId.
func TestBuildFellingCandidate_FellingReferencesItsTree(t *testing.T) {
	rows := []felledRowFixture{
		{ID: "kap-b", BoomID: "stam-2", FelledOn: felledOn(2023, 5, 1)},
	}

	turtle := string(buildFellingCandidate(rows))

	felledTreeLines := gtLinesContaining(turtle, "gs:felledTree")
	require.NotEmpty(t, felledTreeLines, "expected a gs:felledTree triple")
	assert.Contains(t, strings.Join(felledTreeLines, "\n"), "stam-2",
		"gs:felledTree must target the tree minted for this row's boomId")
}

// TestBuildFellingCandidate_FelledOnIsXSDDate covers "gs:felledOn the date": the felling date must
// be emitted as an xsd:date-typed literal, not a bare/untyped string.
func TestBuildFellingCandidate_FelledOnIsXSDDate(t *testing.T) {
	rows := []felledRowFixture{
		{ID: "kap-c", BoomID: "stam-3", FelledOn: felledOn(2023, 8, 15)},
	}

	turtle := string(buildFellingCandidate(rows))

	dateLines := gtLinesContaining(turtle, "gs:felledOn")
	require.NotEmpty(t, dateLines, "expected a gs:felledOn triple")
	block := strings.Join(dateLines, "\n")
	assert.Contains(t, block, `"2023-08-15"^^xsd:date`, "expected the felling date as an xsd:date literal")
}

// TestBuildFellingCandidate_OnlyFelledRowsProjected covers "Only felled trees are loaded": a row
// with FelledOn == nil (kapmaatregelDatumUitgevoerd IS NULL, a tree never felled) must contribute
// NOTHING to the candidate, even alongside a genuinely felled row in the same batch.
func TestBuildFellingCandidate_OnlyFelledRowsProjected(t *testing.T) {
	rows := []felledRowFixture{
		{ID: "kap-felled", BoomID: "stam-felled", FelledOn: felledOn(2023, 3, 1)},
		{ID: "kap-neverfelled", BoomID: "stam-neverfelled", FelledOn: nil},
	}

	turtle := string(buildFellingCandidate(rows))

	gtAssertUnderNamespace(t, turtle, graphTestFellingNS, "kap-felled")
	assert.NotContains(t, turtle, "kap-neverfelled", "a never-felled row's felling id must not appear in the candidate")
	assert.NotContains(t, turtle, "stam-neverfelled", "a never-felled row's tree must not appear in the candidate")
}

// TestBuildFellingCandidate_NeverFelledRowAloneProducesNothing covers the same scenario in
// isolation: a batch containing ONLY never-felled rows must project no gs:Tree/gs:Felling at all.
func TestBuildFellingCandidate_NeverFelledRowAloneProducesNothing(t *testing.T) {
	rows := []felledRowFixture{
		{ID: "kap-x", BoomID: "stam-x", FelledOn: nil},
	}

	turtle := buildFellingCandidate(rows)

	assert.NotContains(t, string(turtle), "gs:Felling", "a never-felled row must not produce a gs:Felling")
	assert.NotContains(t, string(turtle), "gs:Tree", "a never-felled row must not produce a gs:Tree")
}

// TestBuildFellingCandidate_EmptyInput mirrors load/koop/graph_test.go's
// TestBuildCandidate_EmptyInput: no rows must not error and must emit nothing.
func TestBuildFellingCandidate_EmptyInput(t *testing.T) {
	turtle := buildFellingCandidate(nil)
	assert.NotContains(t, string(turtle), "gs:Felling")
	assert.NotContains(t, string(turtle), "gs:Tree")
}

// TestBuildFellingCandidate_MultipleFelledRowsShareOnePreamble covers assembling more than one
// felled row into a single candidate: both fellings/trees present, under one shared Turtle prefix
// header (mirrors load/koop/graph_test.go's TestAssemble_TwoItemsBothAppearUnderOnePreamble /
// buildCandidate's own "one shared prefix preamble" contract).
func TestBuildFellingCandidate_MultipleFelledRowsShareOnePreamble(t *testing.T) {
	rows := []felledRowFixture{
		{ID: "kap-1", BoomID: "stam-10", FelledOn: felledOn(2022, 1, 1)},
		{ID: "kap-2", BoomID: "stam-20", FelledOn: felledOn(2022, 6, 1)},
	}

	turtle := string(buildFellingCandidate(rows))

	assert.Contains(t, turtle, "@prefix gs:", "a single valid Turtle prefix header must be present")
	gtAssertUnderNamespace(t, turtle, graphTestFellingNS, "kap-1")
	gtAssertUnderNamespace(t, turtle, graphTestFellingNS, "kap-2")
	gtAssertUnderNamespace(t, turtle, graphTestTreeNS, "stam-10")
	gtAssertUnderNamespace(t, turtle, graphTestTreeNS, "stam-20")
}
