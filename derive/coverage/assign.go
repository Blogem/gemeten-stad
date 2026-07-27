package coverage

import "sort"

// Assign implements D6's exclusive assignment: each felling goes to the
// single permit whose pair score is highest, with a deterministic tie-break
// (smaller DistanceM wins, nil sorting last; then lexicographically smaller
// Zaaknummer). The result is a pure function of pairs — no map-iteration-order
// dependence — so it is stable across runs.
func Assign(pairs []Pair) map[string][]string {
	winners := make(map[string]Pair, len(pairs))
	for _, p := range pairs {
		cur, ok := winners[p.FellingID]
		if !ok || better(p, cur) {
			winners[p.FellingID] = p
		}
	}

	out := make(map[string][]string, len(winners))
	for fellingID, p := range winners {
		out[p.Zaaknummer] = append(out[p.Zaaknummer], fellingID)
	}
	for z := range out {
		sort.Strings(out[z])
	}
	return out
}

// better reports whether a should win over b for the same felling: higher
// score first; then smaller distance (nil sorts last); then smaller
// zaaknummer.
func better(a, b Pair) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if (a.DistanceM == nil) != (b.DistanceM == nil) {
		return a.DistanceM != nil
	}
	if a.DistanceM != nil && b.DistanceM != nil && *a.DistanceM != *b.DistanceM {
		return *a.DistanceM < *b.DistanceM
	}
	return a.Zaaknummer < b.Zaaknummer
}
