package koop

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/Blogem/gemeten-stad/location"
)

// rdPointPattern extracts an RD (EPSG:28992) point out of a POINT(x y) WKT-ish fragment embedded
// in the overheidwetgeving:geometrie element text (see spikes/spike-b/harvest_permits.py's own
// `POINT\((\d+)\s+(\d+)\)` probe regex; the decimal-allowing variant here is defensive only — RD
// coordinates in the KOOP feed are integers).
var rdPointPattern = regexp.MustCompile(`POINT\((\d+(?:\.\d+)?)\s+(\d+(?:\.\d+)?)\)`)

// postcodePattern matches a Dutch PC4+PC2 postcode, optionally space-separated, e.g. "1024BB" or
// "1024 BB".
var postcodePattern = regexp.MustCompile(`\d{4}\s?[A-Z]{2}`)

// streetWordPattern matches a single "word" token plausible as (part of) a street name: letters
// (including accented ones), possibly with an internal apostrophe/hyphen/period.
var streetWordPattern = regexp.MustCompile(`^[\p{L}][\p{L}'.-]*$`)

// parseRecord token-walks a landed SRU record's inner XML (rec.InnerXML from ingest, i.e. no
// ancestor xmlns declarations survive) looking for title/available/activiteit/geometrie by local
// element name only, mirroring ingest/shared/sru.go's parseSRURecordFields. Missing fields come
// back as zero values; err is only returned for genuinely malformed XML, never for absent fields.
func parseRecord(innerXML []byte) (title, available, activiteit string, point *location.RDPoint, err error) {
	decoder := xml.NewDecoder(bytes.NewReader(innerXML))
	for {
		tok, tokErr := decoder.Token()
		if tokErr == io.EOF {
			break
		}
		if tokErr != nil {
			return "", "", "", nil, fmt.Errorf("koop: parse record: %w", tokErr)
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		switch se.Name.Local {
		case "title":
			var text string
			if err := decoder.DecodeElement(&text, &se); err != nil {
				return "", "", "", nil, fmt.Errorf("koop: parse record title: %w", err)
			}
			if title == "" {
				title = strings.TrimSpace(text)
			}

		case "available":
			var text string
			if err := decoder.DecodeElement(&text, &se); err != nil {
				return "", "", "", nil, fmt.Errorf("koop: parse record available: %w", err)
			}
			if available == "" {
				available = strings.TrimSpace(text)
			}

		case "activiteit":
			var text string
			if err := decoder.DecodeElement(&text, &se); err != nil {
				return "", "", "", nil, fmt.Errorf("koop: parse record activiteit: %w", err)
			}
			if activiteit == "" {
				activiteit = strings.TrimSpace(text)
			}

		case "geometrie":
			var text string
			if err := decoder.DecodeElement(&text, &se); err != nil {
				return "", "", "", nil, fmt.Errorf("koop: parse record geometrie: %w", err)
			}
			if point == nil {
				if m := rdPointPattern.FindStringSubmatch(text); m != nil {
					x, xErr := strconv.ParseFloat(m[1], 64)
					y, yErr := strconv.ParseFloat(m[2], 64)
					if xErr == nil && yErr == nil {
						point = &location.RDPoint{X: x, Y: y}
					}
				}
			}
		}
	}
	return title, available, activiteit, point, nil
}

// parseZaaknummer walks a landed metadata sidecar's <metadata name="..." content="..."/> entries
// (officiele bekendmakingen metadata.xml shape) and returns the content of the entry named
// "OVERHEIDop.referentienummer" — the zaaknummer that ties permit publications together. Returns
// "" (not an error) when no such entry is present.
func parseZaaknummer(metadataXML []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(metadataXML))
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("koop: parse metadata sidecar: %w", err)
		}

		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "metadata" {
			continue
		}

		var name, content string
		for _, attr := range se.Attr {
			switch attr.Name.Local {
			case "name":
				name = attr.Value
			case "content":
				content = attr.Value
			}
		}
		if name == "OVERHEIDop.referentienummer" {
			return content, nil
		}
	}
	return "", nil
}

// classifyKind reads title's leading word (case-insensitive) and returns the matching Kind, or
// KindOther if none match. Ontwerpbesluit is matched before Besluit: both are checked via
// strings.HasPrefix, and keeping Ontwerpbesluit first guards against a future, looser Besluit
// check (e.g. strings.Contains) misclassifying an "Ontwerpbesluit ..." title as KindBesluit.
func classifyKind(title string) Kind {
	t := strings.ToLower(strings.TrimSpace(title))
	switch {
	case strings.HasPrefix(t, "ontwerpbesluit"):
		return KindOntwerpbesluit
	case strings.HasPrefix(t, "besluit"):
		return KindBesluit
	case strings.HasPrefix(t, "aanvraag"):
		return KindAanvraag
	case strings.HasPrefix(t, "verlenging"):
		return KindVerlenging
	case strings.HasPrefix(t, "ingetrokken"):
		return KindIngetrokken
	default:
		return KindOther
	}
}

// extractAddress best-effort parses postcode/huisnummer/street out of a KOOP publication title
// (e.g. "Besluit omgevingsvergunning vellen van een houtopstand (kap) reguliere procedure Örehof 8
// 1024BB Amsterdam"). The postcode is the first PC4+PC2 match, normalized to no-space uppercase;
// the huisnummer is the integer token immediately preceding the postcode; the street is, best
// effort, the single word token immediately preceding the huisnummer (titles are free text, so
// anything more elaborate risks swallowing unrelated words like "reguliere procedure"). Any piece
// that can't be found comes back as its zero value ("", 0, "") — never an error.
func extractAddress(title string) (postcode string, huisnummer int, street string) {
	loc := postcodePattern.FindStringIndex(title)
	if loc == nil {
		return "", 0, ""
	}
	postcode = strings.ToUpper(strings.ReplaceAll(title[loc[0]:loc[1]], " ", ""))

	before := strings.Fields(title[:loc[0]])
	if len(before) == 0 {
		return postcode, 0, ""
	}

	huisnummerToken := before[len(before)-1]
	n, err := strconv.Atoi(huisnummerToken)
	if err != nil {
		return postcode, 0, ""
	}
	huisnummer = n

	if len(before) >= 2 {
		candidate := before[len(before)-2]
		if streetWordPattern.MatchString(candidate) {
			street = candidate
		}
	}
	return postcode, huisnummer, street
}
