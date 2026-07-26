package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestClassify covers the pure change-detection core (task 1.3 / design D1-D2, D4):
// classify(candidate, live map[iri]entitySignature) buckets every candidate entity into
// exactly one of newIRIs, changed, unchanged, immutableConflict, keyed on stable subject
// IRI and comparing valid-time-agnostic content signatures.
func TestClassify(t *testing.T) {
	tests := []struct {
		name string

		candidate map[iri]entitySignature
		live      map[iri]entitySignature

		wantNew               []iri
		wantChanged           []iri
		wantUnchanged         []iri
		wantImmutableConflict []iri
	}{
		{
			// Scenario: "A changing run gets its own named graph" — an entity that has
			// never been loaded before has no counterpart in live at all.
			name: "new IRI absent from live is classified as new",
			candidate: map[iri]entitySignature{
				"data:place-1": {evolving: false, signature: "sig-place-1"},
			},
			live:    map[iri]entitySignature{},
			wantNew: []iri{"data:place-1"},
		},
		{
			// Scenario: "Re-running unchanged input is a true no-op" — identical signature
			// on both sides, regardless of evolving/immutable, means nothing to write.
			name: "unchanged re-run with identical signature",
			candidate: map[iri]entitySignature{
				"data:located-at-1": {evolving: true, signature: "place=data:place-1;confidence=0.9"},
			},
			live: map[iri]entitySignature{
				"data:located-at-1": {evolving: true, signature: "place=data:place-1;confidence=0.9"},
			},
			wantUnchanged: []iri{"data:located-at-1"},
		},
		{
			// Scenario: "A changed tracked field opens a new version and closes the prior"
			// — evolving entity, resolved place changed between runs.
			name: "changed evolving edge: place change",
			candidate: map[iri]entitySignature{
				"data:located-at-1": {evolving: true, signature: "place=data:place-2;confidence=0.9"},
			},
			live: map[iri]entitySignature{
				"data:located-at-1": {evolving: true, signature: "place=data:place-1;confidence=0.9"},
			},
			wantChanged: []iri{"data:located-at-1"},
		},
		{
			// Same scenario, but the differing content is the confidence annotation rather
			// than the resolved place — still an evolving-entity content change.
			name: "changed evolving edge: confidence-only change",
			candidate: map[iri]entitySignature{
				"data:located-at-1": {evolving: true, signature: "place=data:place-1;confidence=0.6"},
			},
			live: map[iri]entitySignature{
				"data:located-at-1": {evolving: true, signature: "place=data:place-1;confidence=0.9"},
			},
			wantChanged: []iri{"data:located-at-1"},
		},
		{
			// Scenario: "An unchanged immutable fact is skipped" — identical content on an
			// immutable (evolving==false) entity is unchanged, NOT immutableConflict.
			name: "immutable re-assert with identical signature is skipped (unchanged, not conflict)",
			candidate: map[iri]entitySignature{
				"data:place-1": {evolving: false, signature: "sig-place-1"},
			},
			live: map[iri]entitySignature{
				"data:place-1": {evolving: false, signature: "sig-place-1"},
			},
			wantUnchanged: []iri{"data:place-1"},
		},
		{
			// Scenario: "A conflicting immutable fact is skipped and surfaced" — differing
			// content on an immutable entity must land in its own bucket, distinct from
			// both unchanged and changed (D4: "not folded into unchanged").
			name: "immutable conflict is bucketed separately from unchanged and changed",
			candidate: map[iri]entitySignature{
				"data:place-1": {evolving: false, signature: "sig-place-1-v2"},
			},
			live: map[iri]entitySignature{
				"data:place-1": {evolving: false, signature: "sig-place-1-v1"},
			},
			wantImmutableConflict: []iri{"data:place-1"},
		},
		{
			// D3: signature is already computed excluding gs:validFrom/gs:validTo upstream
			// (SPARQL SELECT strips them before GROUP_CONCAT), so two entities differing
			// only in valid-time arrive here with the SAME signature string — classify has
			// no valid-time concept of its own, it just sees equal signatures.
			name: "valid-time-only difference is pre-normalized to an equal signature -> unchanged",
			candidate: map[iri]entitySignature{
				// upstream candidate carried gs:validFrom "2024-01-01"; stripped before diff.
				"data:located-at-1": {evolving: true, signature: "place=data:place-1;confidence=0.9"},
			},
			live: map[iri]entitySignature{
				// upstream live version carries no gs:validTo (still open); stripped before diff.
				"data:located-at-1": {evolving: true, signature: "place=data:place-1;confidence=0.9"},
			},
			wantUnchanged: []iri{"data:located-at-1"},
		},
		{
			// Realistic whole-candidate batch: one classify() call spans all four buckets
			// at once, as a single Load run would submit its full candidate.
			name: "multi-entity candidate batch lands entities across all buckets",
			candidate: map[iri]entitySignature{
				"data:place-new":          {evolving: false, signature: "sig-new"},
				"data:located-unchanged":  {evolving: true, signature: "place=data:place-1;confidence=0.9"},
				"data:located-changed":    {evolving: true, signature: "place=data:place-2;confidence=0.9"},
				"data:place-immutable-ok": {evolving: false, signature: "sig-ok"},
				"data:place-conflict":     {evolving: false, signature: "sig-conflict-v2"},
			},
			live: map[iri]entitySignature{
				"data:located-unchanged":  {evolving: true, signature: "place=data:place-1;confidence=0.9"},
				"data:located-changed":    {evolving: true, signature: "place=data:place-1;confidence=0.9"},
				"data:place-immutable-ok": {evolving: false, signature: "sig-ok"},
				"data:place-conflict":     {evolving: false, signature: "sig-conflict-v1"},
			},
			wantNew:               []iri{"data:place-new"},
			wantChanged:           []iri{"data:located-changed"},
			wantUnchanged:         []iri{"data:located-unchanged", "data:place-immutable-ok"},
			wantImmutableConflict: []iri{"data:place-conflict"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newIRIs, changed, unchanged, immutableConflict := classify(tt.candidate, tt.live)

			assert.ElementsMatch(t, tt.wantNew, newIRIs, "newIRIs")
			assert.ElementsMatch(t, tt.wantChanged, changed, "changed")
			assert.ElementsMatch(t, tt.wantUnchanged, unchanged, "unchanged")
			assert.ElementsMatch(t, tt.wantImmutableConflict, immutableConflict, "immutableConflict")

			// D4: immutableConflict is its own bucket, never folded into unchanged or
			// changed. Assert explicit absence for every conflict IRI in the other buckets,
			// even though ElementsMatch above already implies it via exact set equality.
			for _, conflictIRI := range tt.wantImmutableConflict {
				assert.NotContains(t, unchanged, conflictIRI, "immutable conflict must not appear in unchanged")
				assert.NotContains(t, changed, conflictIRI, "immutable conflict must not appear in changed")
				assert.NotContains(t, newIRIs, conflictIRI, "immutable conflict must not appear in newIRIs")
			}
		})
	}
}
