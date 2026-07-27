package ontology

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmbeddedArtifactsPresent is a fast smoke test that the three .ttl files are embedded and
// carry their defining markers. Full RDF-syntax + SKOS-validity + vocab assertions run in the
// integration lane (loaded into Fuseki + SPARQL) — Go has no Turtle parser here.
func TestEmbeddedArtifactsPresent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content []byte
		markers []string
	}{
		{"ontology", Ontology, []string{"gs:Intervention", "gs:locatedAt", "gs:confidence", "gs:within", "gs:active", "gs:versionOf", "gs:coversIntervention", "gs:CoveragePeriod", "gs:linksObservation", "gs:granularity", "gs:noSourceFound", "gs:countUnknown"}},
		{"vocab", Vocab, []string{"sch:tree-audit", "skos:ConceptScheme", "act:vellen"}},
		{"shapes", Shapes, []string{"gs:InterventionShape", "sh:NodeShape", "sh:sparql", "sh:targetClass", "gs:AuditLinkShape", "gs:CoveragePeriodShape", "sh:xone", "dct:available"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NotEmpty(t, tc.content, "%s.ttl must be embedded", tc.name)
			for _, m := range tc.markers {
				assert.True(t, strings.Contains(string(tc.content), m), "%s.ttl should contain %q", tc.name, m)
			}
		})
	}
}
