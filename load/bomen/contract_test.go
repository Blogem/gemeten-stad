package bomen

import (
	"testing"

	ingestbomen "github.com/Blogem/gemeten-stad/ingest/bomen"
	"github.com/stretchr/testify/assert"
)

// TestArtifactNamesMatchIngest guards the load/geo-style "re-declare matching-value consts"
// pattern load/bomen uses for its landing-artifact names (load.go): load/bomen deliberately does
// not import ingest/bomen for its non-test code (keeping the load package's dependency surface
// small), so it re-declares the same artifact name strings ingest/bomen lands under. A test
// importing ingest/bomen is fine (only non-test load code must avoid the dependency) — this test
// is what keeps the two declarations from silently drifting apart, which would otherwise make a
// real `pipeline ingest bomen` -> `pipeline load bomen` run fail to find the landed data.
func TestArtifactNamesMatchIngest(t *testing.T) {
	assert.Equal(t, ingestbomen.ArtifactKapenherplant, artifactKapenherplant,
		"load/bomen's artifactKapenherplant must match ingest/bomen.ArtifactKapenherplant")
	assert.Equal(t, ingestbomen.ArtifactStamgegevens, artifactStamgegevens,
		"load/bomen's artifactStamgegevens must match ingest/bomen.ArtifactStamgegevens")
}
