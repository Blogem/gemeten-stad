package bag

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A small, realistic PDOK BAG atom feed sample: one entry carrying an
// "enclosure" link to the national LV BAG 2.0 Extract zip.
const sampleFeedXML = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <id>urn:uuid:lvbag-extract-nl-deliveries</id>
  <title>BAG Extract Deliveries</title>
  <updated>2026-07-01T00:00:00Z</updated>
  <entry>
    <id>urn:uuid:lvbag-extract-nl-20260701</id>
    <title>Levering 9999NL.IMBAG.Extract 2026-07-01</title>
    <updated>2026-07-01T00:00:00Z</updated>
    <category term="volledig"/>
    <link rel="enclosure" type="application/octet-stream"
          href="https://service.pdok.nl/lv/bag/atom/downloads/lvbag-extract-nl.zip"
          length="3865470000"/>
  </entry>
</feed>
`

// A variant where the entry's link is nested differently but still exposes
// the same href — atom feeds vary in attribute order across providers.
const sampleFeedXMLAltOrder = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <link length="3865470000" href="https://service.pdok.nl/lv/bag/atom/downloads/lvbag-extract-nl.zip"
          type="application/octet-stream" rel="enclosure"/>
  </entry>
</feed>
`

func TestParseAtomFeed_ReturnsDownloadURL(t *testing.T) {
	tests := []struct {
		name string
		xml  string
	}{
		{"standard attribute order", sampleFeedXML},
		{"alternate attribute order", sampleFeedXMLAltOrder},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, err := ParseAtomFeed([]byte(tt.xml))
			require.NoError(t, err)
			assert.Equal(t, "https://service.pdok.nl/lv/bag/atom/downloads/lvbag-extract-nl.zip", url)
			assert.Contains(t, url, ExtractName, "the download URL must point at the extract filename")
		})
	}
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
			_, err := ParseAtomFeed(tt.xml)
			assert.Error(t, err)
		})
	}
}

func TestExtractName(t *testing.T) {
	assert.Equal(t, "lvbag-extract-nl.zip", ExtractName)
}
