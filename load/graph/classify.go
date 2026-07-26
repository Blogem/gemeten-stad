package graph

import "sort"

// iri is a subject IRI key. Change detection is keyed on the stable subject IRI, per
// design.md D1 — a per-entity subgraph diff, not a whole-IRI delete-insert, so a superseded
// version can be closed rather than destroyed.
type iri string

// entitySignature is one entity's valid-time-agnostic content signature: a canonical string
// built (by the SPARQL extractor, elsewhere) from the entity's sorted (predicate, object) rows
// plus its RDF-star annotation rows, EXCLUDING gs:validFrom/gs:validTo (design.md D2/D3) — so an
// entity re-emitted with only a fresh valid-time stamp still compares equal.
//
// evolving reports whether the candidate asserts a gs:validFrom for this entity (design.md D4):
// that presence, not any per-predicate hardcoding, is what distinguishes evolving state
// (participates in open/close) from immutable facts (write-once, insert-if-absent).
type entitySignature struct {
	evolving  bool
	signature string
}

// classify compares the candidate entities against the live open versions already in the graph
// (both keyed on subject IRI) and sorts every candidate entity into exactly one of four disjoint
// buckets. It is a pure function over the two signature maps: no I/O, no RDF-star construction —
// the SPARQL signature extraction and the store mutation both live elsewhere (design.md D2, D6).
//
// For each iri in candidate:
//
//   - absent from live                                  -> newIRIs
//   - present in live, signature equal                  -> unchanged (regardless of evolving)
//   - present in live, signature differs, evolving       -> changed (open a new version, close the
//     prior — design.md D3)
//   - present in live, signature differs, NOT evolving   -> immutableConflict (an immutable fact
//     never overwrites; this is a distinct, surfaced bucket, deliberately not folded into
//     unchanged — design.md D4)
//
// Each returned slice is sorted ascending so callers (and tests) see a deterministic order.
func classify(candidate, live map[iri]entitySignature) (newIRIs, changed, unchanged, immutableConflict []iri) {
	for id, cand := range candidate {
		liveSig, present := live[id]
		switch {
		case !present:
			newIRIs = append(newIRIs, id)
		case cand.signature == liveSig.signature:
			unchanged = append(unchanged, id)
		case cand.evolving:
			changed = append(changed, id)
		default:
			immutableConflict = append(immutableConflict, id)
		}
	}

	sortIRIs(newIRIs)
	sortIRIs(changed)
	sortIRIs(unchanged)
	sortIRIs(immutableConflict)

	return newIRIs, changed, unchanged, immutableConflict
}

// sortIRIs sorts a slice of iri ascending in place, giving classify's buckets a deterministic
// order regardless of Go's randomized map iteration.
func sortIRIs(ids []iri) {
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
}
