package coverage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// -- Contested felling goes to its best permit only --------------------------

func TestAssign_ContestedFellingGoesToBestPermitOnly(t *testing.T) {
	pairs := []Pair{
		{Zaaknummer: "A", FellingID: "F", Score: 0.8},
		{Zaaknummer: "B", FellingID: "F", Score: 0.6},
	}

	got := Assign(pairs)

	assert.Equal(t, []string{"F"}, got["A"])
	assert.NotContains(t, got["B"], "F")
}

// -- Multi-tree permit is assigned all of its best-scoring fellings ---------

func TestAssign_MultiTreePermitGetsAllBestScoringFellings(t *testing.T) {
	pairs := []Pair{
		{Zaaknummer: "A", FellingID: "F1", Score: 0.9},
		{Zaaknummer: "A", FellingID: "F2", Score: 0.85},
		{Zaaknummer: "A", FellingID: "F3", Score: 0.7},
		{Zaaknummer: "B", FellingID: "F1", Score: 0.5}, // A still wins F1
	}

	got := Assign(pairs)

	assert.Equal(t, 3, len(got["A"]))
	assert.Equal(t, []string{"F1", "F2", "F3"}, got["A"], "assigned set is sorted")
	assert.NotContains(t, got["B"], "F1")
}

// -- Deterministic tie-break: distance, then zaaknummer ----------------------

func TestAssign_TieBreakByDistanceThenZaaknummer(t *testing.T) {
	t.Run("smaller distance wins on equal score", func(t *testing.T) {
		pairs := []Pair{
			{Zaaknummer: "Z2", FellingID: "F", Score: 0.75, DistanceM: f64(50.0)},
			{Zaaknummer: "Z1", FellingID: "F", Score: 0.75, DistanceM: f64(10.0)},
			{Zaaknummer: "Z3", FellingID: "F", Score: 0.75, DistanceM: nil},
		}

		got := Assign(pairs)

		assert.Equal(t, []string{"F"}, got["Z1"])
		assert.NotContains(t, got["Z2"], "F")
		assert.NotContains(t, got["Z3"], "F")
	})

	t.Run("nil distance sorts last regardless of the concrete distance's magnitude", func(t *testing.T) {
		pairs := []Pair{
			{Zaaknummer: "Z1", FellingID: "H", Score: 0.5, DistanceM: nil},
			{Zaaknummer: "Z2", FellingID: "H", Score: 0.5, DistanceM: f64(999.0)},
		}

		got := Assign(pairs)

		assert.Equal(t, []string{"H"}, got["Z2"])
		assert.NotContains(t, got["Z1"], "H")
	})

	t.Run("equal score and nil distance breaks the tie by smaller zaaknummer", func(t *testing.T) {
		pairs := []Pair{
			{Zaaknummer: "Z9", FellingID: "G", Score: 0.5, DistanceM: nil},
			{Zaaknummer: "Z2", FellingID: "G", Score: 0.5, DistanceM: nil},
		}

		got := Assign(pairs)

		assert.Equal(t, []string{"G"}, got["Z2"])
		assert.NotContains(t, got["Z9"], "G")
	})
}

// -- Assignment is stable across runs, independent of input order -----------

func TestAssign_StableAcrossRuns(t *testing.T) {
	pairs := []Pair{
		{Zaaknummer: "A", FellingID: "F1", Score: 0.9},
		{Zaaknummer: "A", FellingID: "F2", Score: 0.85},
		{Zaaknummer: "B", FellingID: "F1", Score: 0.4},
		{Zaaknummer: "B", FellingID: "F3", Score: 0.6},
		{Zaaknummer: "C", FellingID: "F3", Score: 0.55},
		{Zaaknummer: "D", FellingID: "F4", Score: 0.75, DistanceM: f64(20.0)},
		{Zaaknummer: "E", FellingID: "F4", Score: 0.75, DistanceM: f64(5.0)},
	}

	shuffled := make([]Pair, len(pairs))
	for i, p := range pairs {
		shuffled[len(pairs)-1-i] = p // deterministic reorder
	}

	got1 := Assign(pairs)
	got2 := Assign(pairs)
	got3 := Assign(shuffled)

	assert.Equal(t, got1, got2, "two runs on the same input order must produce identical maps")
	assert.Equal(t, got1, got3, "input order must not affect the resulting assignment (no map-order dependence)")
}
