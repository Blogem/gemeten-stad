package coverage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFellingIRIMatchesCanonicalNamespace guards derive/coverage's felling instance-IRI minting
// (mintFellingIRI/fellingNS in assemble.go) against silent drift from the canonical prefix
// ontology/shapes.ttl's gs:ObservationShape sh:pattern on gs:includesFelling pins
// ("^http://gemetenstad\.nl/id/felling/"). This namespace is declared a SECOND time, independently,
// in load/bomen/graph.go (its own fellingNS/mintFellingIRI, for the gs:Felling nodes it mints) — and
// derive/coverage's gs:includesFelling values MUST resolve to exactly those nodes. SHACL only
// regex-matches the IRI shape; it never verifies the referenced gs:Felling node exists, so a silent
// divergence between the two declarations would orphan every gs:includesFelling reference with no
// test catching it. This test (and load/bomen's own matching
// TestFellingAndTreeIRIsMatchCanonicalNamespace) pins each package's minting function to the same
// literal canonical prefix independently — mirroring load/bomen/contract_test.go's
// TestArtifactNamesMatchIngest guard for this repo's other duplicated-constant drift risk — rather
// than importing across packages or exporting new production API just for a test.
func TestFellingIRIMatchesCanonicalNamespace(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"F1", "http://gemetenstad.nl/id/felling/F1"},
		{"GMB-2022-1", "http://gemetenstad.nl/id/felling/GMB-2022-1"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, mintFellingIRI(tt.id))
		})
	}
}
