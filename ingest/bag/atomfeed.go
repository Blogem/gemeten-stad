package bag

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// atomFeed models just enough of the Atom XML feed format (RFC 4287) to find a download or
// sub-feed link. Feed-level navigation links (self/up/describedby) carry no data pointers for
// PDOK's feeds and are deliberately not modeled here — only <entry> links matter.
type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
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

// ParseAtomFeed inspects the <entry> links of a PDOK atom feed. PDOK's BAG feed is two levels
// deep: the top feed's entry links to a dataset sub-feed (type "application/atom+xml"), and that
// sub-feed's entry links to the actual data download (type "application/zip", or an href ending
// in ".zip"). Feed-level self/up/describedby links are ignored.
//
// If an entry link is the data download, ParseAtomFeed returns (href, true, nil). If an entry
// link points to a dataset sub-feed instead, it returns (href, false, nil) so the caller can
// follow one more level. It errors if neither is found among the entry links.
func ParseAtomFeed(feedXML []byte) (link string, isDownload bool, err error) {
	var feed atomFeed
	if err := xml.Unmarshal(feedXML, &feed); err != nil {
		return "", false, fmt.Errorf("bag: parse atom feed: %w", err)
	}

	var subfeedHref string
	for _, entry := range feed.Entries {
		for _, l := range entry.Links {
			if l.Href == "" {
				continue
			}
			if l.Type == "application/zip" || strings.HasSuffix(strings.ToLower(l.Href), ".zip") {
				return l.Href, true, nil
			}
			if subfeedHref == "" && l.Type == "application/atom+xml" {
				subfeedHref = l.Href
			}
		}
	}
	if subfeedHref != "" {
		return subfeedHref, false, nil
	}
	return "", false, fmt.Errorf("bag: no .zip download or dataset sub-feed link found among atom feed entries")
}
