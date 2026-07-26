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

// Format identifies which wire format a Dataset is fetched in.
type Format string

const (
	// FormatPagedJSON pages through the DSO's default JSON HAL representation,
	// following _links.next.href. The DSO caps this at page 100, so it only
	// works for datasets small enough to fit under that cap.
	FormatPagedJSON Format = "paged"
	// FormatGeoJSON fetches the DSO's `?_format=geojson` export, which is not
	// page-capped, following its own _links.next.href until exhausted.
	FormatGeoJSON Format = "geojson"
)

// Dataset identifies one of the two bomen sub-datasets this package
// ingests. Name doubles as the _embedded key the Datapunt DSO API nests
// rows under in each page (FormatPagedJSON only); Artifact is the
// landing-store artifact name; Format selects the fetch strategy.
type Dataset struct {
	Name     string
	BaseURL  string
	Artifact string
	Format   Format
}

// The two sub-datasets this package ingests (DATA_SOURCES.md §2a). Neither
// query ever needs row/column filtering or spatial params: an [isnull]
// filter silently returns an empty result, and a geometrie[within] spatial
// filter 403s against kapenherplant.
//
// kapenherplant (36 pages) stays on the paged JSON API. stamgegevens (~324k
// rows, >100 pages) must use the uncapped GeoJSON export instead — the DSO
// hard-caps page-based pagination at page 100 ("Page number cannot exceed
// 100"), which stamgegevens exceeds (DATA_SOURCES.md §2a).
var (
	Kapenherplant = Dataset{
		Name:     "kapenherplant",
		BaseURL:  "https://api.data.amsterdam.nl/v1/bomen/kapenherplant/",
		Artifact: ArtifactKapenherplant,
		Format:   FormatPagedJSON,
	}
	Stamgegevens = Dataset{
		Name:     "stamgegevens",
		BaseURL:  "https://api.data.amsterdam.nl/v1/bomen/stamgegevens/",
		Artifact: ArtifactStamgegevens,
		Format:   FormatGeoJSON,
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
