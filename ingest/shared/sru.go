package shared

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HTTPGetFunc performs a GET against url and returns the response body for
// the caller to read and close. Mirrors the injected-httpGet shape already
// used by the other ingest sources (e.g. ingest/bomen).
type HTTPGetFunc func(ctx context.Context, url string) (io.ReadCloser, error)

// SRURecord is one <record> from an SRU searchRetrieve response.
type SRURecord struct {
	Identifier string // dcterms:identifier, e.g. "gmb-2022-291126"
	Available  string // dt.available, e.g. "2022-05-01"
	InnerXML   []byte // verbatim inner XML of the <record> element (for verbatim landing)
}

// SRUPageSize is the maximumRecords page size used when paging a
// searchRetrieve query to exhaustion.
const SRUPageSize = 100

// SRURateInterval is the fixed delay between successive SRU page requests,
// keeping sequential paging polite against the endpoint.
const SRURateInterval = 200 * time.Millisecond

// sruSleep is a seam over time.Sleep so tests can drive a multi-page fetch
// without incurring real rate-limit delays.
var sruSleep = func(d time.Duration) { time.Sleep(d) }

// SRURequestURL builds an SRU 2.0 searchRetrieve request URL against
// endpoint for query, paged at startRecord/maximumRecords. Pure: no I/O.
func SRURequestURL(endpoint, query string, startRecord, maximumRecords int) string {
	params := url.Values{}
	params.Set("operation", "searchRetrieve")
	params.Set("version", "2.0")
	params.Set("query", query)
	params.Set("startRecord", strconv.Itoa(startRecord))
	params.Set("maximumRecords", strconv.Itoa(maximumRecords))
	return endpoint + "?" + params.Encode()
}

// ParseSRUResponse parses an SRU 2.0 searchRetrieve response body, reading
// the envelope's numberOfRecords and splitting out each <record> element.
// Matching is by local element name only (namespace prefixes on the wire,
// e.g. srw:/sru:, are ignored) since KOOP's prefixes are not worth binding
// to. Each record's inner XML is captured verbatim for landing, alongside
// the two fields the incremental cursor needs: dcterms:identifier and
// dt.available (both matched by local name within the record).
func ParseSRUResponse(body []byte) (numberOfRecords int, records []SRURecord, err error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, tokErr := decoder.Token()
		if tokErr == io.EOF {
			break
		}
		if tokErr != nil {
			return 0, nil, fmt.Errorf("shared: parse SRU response: %w", tokErr)
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		switch se.Name.Local {
		case "numberOfRecords":
			var text string
			if err := decoder.DecodeElement(&text, &se); err != nil {
				return 0, nil, fmt.Errorf("shared: parse numberOfRecords: %w", err)
			}
			n, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil {
				return 0, nil, fmt.Errorf("shared: parse numberOfRecords value %q: %w", text, err)
			}
			numberOfRecords = n

		case "record":
			var raw struct {
				InnerXML []byte `xml:",innerxml"`
			}
			if err := decoder.DecodeElement(&raw, &se); err != nil {
				return 0, nil, fmt.Errorf("shared: parse SRU record: %w", err)
			}
			rec, err := parseSRURecordFields(raw.InnerXML)
			if err != nil {
				return 0, nil, fmt.Errorf("shared: parse SRU record fields: %w", err)
			}
			records = append(records, rec)
		}
	}
	return numberOfRecords, records, nil
}

// parseSRURecordFields walks a record's inner XML (KOOP nests the actual
// metadata inside recordData, several levels deep) looking for
// dcterms:identifier and dt.available by local element name, ignoring
// namespace prefixes and nesting depth.
func parseSRURecordFields(innerXML []byte) (SRURecord, error) {
	rec := SRURecord{InnerXML: innerXML}

	decoder := xml.NewDecoder(bytes.NewReader(innerXML))
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return SRURecord{}, fmt.Errorf("shared: parse SRU record fields: %w", err)
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		switch se.Name.Local {
		case "identifier":
			var text string
			if err := decoder.DecodeElement(&text, &se); err != nil {
				return SRURecord{}, fmt.Errorf("shared: parse record identifier: %w", err)
			}
			if rec.Identifier == "" {
				rec.Identifier = strings.TrimSpace(text)
			}
		case "available":
			var text string
			if err := decoder.DecodeElement(&text, &se); err != nil {
				return SRURecord{}, fmt.Errorf("shared: parse record available: %w", err)
			}
			if rec.Available == "" {
				rec.Available = strings.TrimSpace(text)
			}
		}
	}
	return rec, nil
}

// FetchSRUAll pages endpoint for query to exhaustion, invoking yield for
// each record encountered. It fetches startRecord=1 first, reads
// numberOfRecords from that page, then keeps advancing startRecord by
// SRUPageSize until every record has been yielded. Pages are rate-limited by
// SRURateInterval (no delay before the first request). If yield returns an
// error, paging stops immediately and that error is returned. Returns the
// numberOfRecords reported by the endpoint.
func FetchSRUAll(ctx context.Context, httpGet HTTPGetFunc, endpoint, query string, yield func(SRURecord) error) (numberOfRecords int, err error) {
	if httpGet == nil {
		return 0, fmt.Errorf("shared: httpGet must not be nil")
	}

	startRecord := 1
	firstPage := true
	for {
		if !firstPage {
			sruSleep(SRURateInterval)
		}
		firstPage = false

		reqURL := SRURequestURL(endpoint, query, startRecord, SRUPageSize)
		rc, err := httpGet(ctx, reqURL)
		if err != nil {
			return 0, fmt.Errorf("shared: fetch SRU page %s: %w", reqURL, err)
		}
		body, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			return 0, fmt.Errorf("shared: read SRU page body from %s: %w", reqURL, readErr)
		}
		if closeErr != nil {
			return 0, fmt.Errorf("shared: close SRU page body from %s: %w", reqURL, closeErr)
		}

		pageTotal, records, err := ParseSRUResponse(body)
		if err != nil {
			return 0, fmt.Errorf("shared: parse SRU page from %s: %w", reqURL, err)
		}
		numberOfRecords = pageTotal

		for _, rec := range records {
			if err := yield(rec); err != nil {
				return numberOfRecords, err
			}
		}

		startRecord += SRUPageSize
		if len(records) == 0 || startRecord > numberOfRecords {
			break
		}
	}
	return numberOfRecords, nil
}
