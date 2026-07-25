package bag

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// atomFeed models just enough of the Atom XML feed format (RFC 4287) to find a download link:
// a feed carries feed-level links and zero or more entries, each of which may itself carry links.
type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Links   []atomLink  `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Links []atomLink `xml:"link"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

// ParseAtomFeed extracts the extract download URL from the PDOK atom feed XML. Pure — no I/O.
// The PDOK feed serves exactly one file: it is found as the first link (entry-level links take
// precedence over feed-level links) whose href points at a .zip file.
func ParseAtomFeed(feedXML []byte) (string, error) {
	var feed atomFeed
	if err := xml.Unmarshal(feedXML, &feed); err != nil {
		return "", fmt.Errorf("bag: parse atom feed: %w", err)
	}

	var links []atomLink
	for _, entry := range feed.Entries {
		links = append(links, entry.Links...)
	}
	links = append(links, feed.Links...)

	for _, link := range links {
		if link.Href != "" && strings.HasSuffix(strings.ToLower(link.Href), ".zip") {
			return link.Href, nil
		}
	}
	return "", fmt.Errorf("bag: no .zip download link found in atom feed")
}
