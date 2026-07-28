package bomen

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFellingAndTreeIRIsMatchCanonicalNamespace guards load/bomen's felling/tree instance-IRI
// minting scheme against silent drift from the canonical prefixes ontology/shapes.ttl's SHACL
// sh:pattern gates pin: gs:FellingShape's gs:felledTree pattern ("^http://gemetenstad\.nl/id/tree/")
// and gs:ObservationShape's gs:includesFelling pattern ("^http://gemetenstad\.nl/id/felling/").
// The felling namespace is declared independently a second time in derive/coverage/assemble.go
// (mintFellingIRI/fellingNS there) — SHACL only regex-matches the IRI shape, it never verifies the
// referenced gs:Felling node actually exists, so a silent divergence between the two declarations
// would orphan every derive-minted gs:includesFelling reference with no test catching it. This test
// (and derive/coverage's own matching TestFellingIRIMatchesCanonicalNamespace) pins each package's
// minting function to the same literal canonical prefix independently — mirroring
// contract_test.go's TestArtifactNamesMatchIngest guard for this repo's other duplicated-constant
// drift risk — rather than importing across packages or exporting new production API just for a
// test.
func TestFellingAndTreeIRIsMatchCanonicalNamespace(t *testing.T) {
	tests := []struct {
		id   string
		want string
		mint func(string) string
	}{
		{"T-1", "http://gemetenstad.nl/id/tree/T-1", mintTreeIRI},
		{"kap-a", "http://gemetenstad.nl/id/felling/kap-a", mintFellingIRI},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.mint(tt.id))
		})
	}
}
