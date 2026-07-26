package bag

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleTopFeedXML is a realistic PDOK top-level atom feed: its entry does not carry a download,
// it points to a dataset sub-feed. It also carries a feed-level "self" nav link that must be
// ignored.
const sampleTopFeedXML = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <link rel="self" type="application/atom+xml" href="https://service.pdok.nl/kadaster/adressen/atom/v1_0/index.xml"></link>
  <entry>
    <link href="https://service.pdok.nl/kadaster/bag-adressen/atom/adressen.xml" rel="alternate" type="application/atom+xml" hreflang="nl" title="Adressen ATOM"></link>
  </entry>
</feed>
`

// sampleSubFeedXML is a realistic PDOK dataset sub-feed: its entry carries the actual .zip
// download. It also carries feed-level "self"/"up"/"describedby" nav links that must be ignored.
const sampleSubFeedXML = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <link rel="self" type="application/atom+xml" href="https://service.pdok.nl/kadaster/bag-adressen/atom/adressen.xml"></link>
  <link rel="up" type="application/atom+xml" href="https://service.pdok.nl/kadaster/adressen/atom/v1_0/index.xml"></link>
  <link rel="describedby" type="application/atom+xml" href="https://service.pdok.nl/kadaster/bag-adressen/atom/adressen.xml"></link>
  <entry>
    <link href="https://service.pdok.nl/kadaster/bag-adressen/atom/downloads/lvbag-extract-nl.zip" rel="alternate" type="application/zip" hreflang="nl" length="3610187048" title="Adressen ATOM - lvbag-extract-nl.zip"></link>
  </entry>
</feed>
`

// sampleSubFeedXMLAltOrder is a variant where the entry link's attributes are ordered
// differently but still identifies the download purely by its .zip suffix (no explicit
// application/zip type) — atom feeds vary in attribute order and completeness across providers.
const sampleSubFeedXMLAltOrder = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <link length="3610187048" href="https://service.pdok.nl/kadaster/bag-adressen/atom/downloads/lvbag-extract-nl.zip"
          rel="alternate"/>
  </entry>
</feed>
`

func TestParseAtomFeed_ReturnsSubfeedLink(t *testing.T) {
	link, isDownload, err := ParseAtomFeed([]byte(sampleTopFeedXML))
	require.NoError(t, err)
	assert.False(t, isDownload, "the top feed's entry link is a dataset sub-feed, not a download")
	assert.Equal(t, "https://service.pdok.nl/kadaster/bag-adressen/atom/adressen.xml", link)
}

func TestParseAtomFeed_ReturnsDownloadURL(t *testing.T) {
	tests := []struct {
		name string
		xml  string
	}{
		{"application/zip type", sampleSubFeedXML},
		{"zip suffix, no explicit type", sampleSubFeedXMLAltOrder},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link, isDownload, err := ParseAtomFeed([]byte(tt.xml))
			require.NoError(t, err)
			assert.True(t, isDownload, "the sub-feed's entry link is the actual data download")
			assert.Equal(t, "https://service.pdok.nl/kadaster/bag-adressen/atom/downloads/lvbag-extract-nl.zip", link)
			assert.Contains(t, link, ExtractName, "the download URL must point at the extract filename")
		})
	}
}

func TestParseAtomFeed_IgnoresFeedLevelLinks(t *testing.T) {
	// Only feed-level self/up links, no entry at all: there is nothing to follow.
	const feedLevelOnlyXML = `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <link rel="self" type="application/atom+xml" href="https://service.pdok.nl/kadaster/adressen/atom/v1_0/index.xml"></link>
  <link rel="up" type="application/atom+xml" href="https://service.pdok.nl/kadaster/adressen/atom/v1_0/index.xml"></link>
</feed>
`
	_, _, err := ParseAtomFeed([]byte(feedLevelOnlyXML))
	assert.Error(t, err, "feed-level nav links must not be mistaken for entry links")
}

func TestParseAtomFeed_Errors(t *testing.T) {
	tests := []struct {
		name string
		xml  []byte
	}{
		{"empty input", []byte{}},
		{"malformed XML", []byte(`<feed><entry><link href="not closed`)},
		{"well-formed but no entries", []byte(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"></feed>`)},
		{
			"entry present but no link",
			[]byte(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><entry><title>no link here</title></entry></feed>`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ParseAtomFeed(tt.xml)
			assert.Error(t, err)
		})
	}
}

func TestExtractName(t *testing.T) {
	assert.Equal(t, "lvbag-extract-nl.zip", ExtractName)
}
