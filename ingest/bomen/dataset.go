package bomen

import "fmt"

// Landing artifact names for the two bomen sub-datasets, versioned via
// shared.RawStore.LandVersion — every fetched version is kept for audit,
// never overwritten.
const (
	ArtifactKapenherplant = "bomen_kapenherplant"
	ArtifactStamgegevens  = "bomen_stamgegevens"
)

// DefaultPageSize bounds the request count for stamgegevens' large row count
// without hitting response-size limits. It is an implementation detail, not
// a spec-level requirement.
const DefaultPageSize = 1000

// Dataset identifies one of the two bomen sub-datasets this package
// ingests. Name doubles as the _embedded key the Datapunt DSO API nests
// rows under in each page; Artifact is the landing-store artifact name.
type Dataset struct {
	Name     string
	BaseURL  string
	Artifact string
}

// The two sub-datasets this package ingests (DATA_SOURCES.md §2a). Neither
// query ever needs row/column filtering or spatial params: an [isnull]
// filter silently returns an empty result, and a geometrie[within] spatial
// filter 403s against kapenherplant.
var (
	Kapenherplant = Dataset{
		Name:     "kapenherplant",
		BaseURL:  "https://api.data.amsterdam.nl/v1/bomen/kapenherplant/",
		Artifact: ArtifactKapenherplant,
	}
	Stamgegevens = Dataset{
		Name:     "stamgegevens",
		BaseURL:  "https://api.data.amsterdam.nl/v1/bomen/stamgegevens/",
		Artifact: ArtifactStamgegevens,
	}
)

// firstPageURL builds the first-page request URL for baseURL: _pageSize and
// an explicit page=1, per the DSO pagination convention. Every subsequent
// page is reached by following the response's own _links.next.href
// verbatim — never reconstructed client-side — so this builder is only
// ever used once per dataset per ingest run.
//
// It deliberately never adds an [isnull] filter or a geometrie[within]
// spatial filter: DATA_SOURCES.md §2a documents the former as silently
// returning an empty result and the latter as a 403 against kapenherplant.
func firstPageURL(baseURL string, pageSize int) string {
	return fmt.Sprintf("%s?_pageSize=%d&page=1", baseURL, pageSize)
}
