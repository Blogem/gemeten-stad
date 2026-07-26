package shared

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
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
	Available  string // dcterms:available (local name "available"), e.g. "2022-05-01"
	InnerXML   []byte // verbatim inner XML of the <record> element (for verbatim landing)
}

// SRUPageSize is the maximumRecords page size used when paging a
// searchRetrieve query to exhaustion.
const SRUPageSize = 100

// SRURateInterval is the fixed delay between successive SRU page requests,
// keeping sequential paging polite against the endpoint.
const SRURateInterval = 200 * time.Millisecond

// SRUMaxAttempts is the maximum number of attempts made to fetch a single
// SRU page before giving up, retrying only transient fetch/read errors.
const SRUMaxAttempts = 8

// SRURetryBaseDelay is the base delay for the capped exponential backoff
// between retry attempts of a failed page fetch: the delay after attempt n
// (1-based) is min(SRURetryBaseDelay<<(n-1), SRUMaxRetryDelay), giving the
// sequence 1s, 2s, 4s, 8s, 16s, 30s, 30s (slept between attempts 1..7 of
// SRUMaxAttempts=8; no sleep after the final attempt).
const SRURetryBaseDelay = 1 * time.Second

// SRUMaxRetryDelay caps the per-attempt backoff delay computed from
// SRURetryBaseDelay, so the doubling plateaus instead of growing unbounded
// (and never overflows time.Duration) across SRUMaxAttempts retries.
const SRUMaxRetryDelay = 30 * time.Second

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
// dcterms:available (both matched by local name within the record).
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
// metadata inside recordData, several levels deep) looking for the
// identifier and available elements by local element name, ignoring
// namespace prefixes and nesting depth. The publication date comes from
// dcterms:available (matched by local name "available"); "dt.available" is
// only the CQL query index name used to build the request, not the response
// element name, so it must not be matched here.
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

// fetchSRUPage performs a single fetch+read+close of an SRU page at url,
// returning the raw response body. Errors from httpGet or from reading/
// closing the body are transient (network-level) failures that the caller
// may retry.
func fetchSRUPage(ctx context.Context, httpGet HTTPGetFunc, url string) ([]byte, error) {
	rc, err := httpGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("shared: fetch SRU page %s: %w", url, err)
	}
	body, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	if readErr != nil {
		return nil, fmt.Errorf("shared: read SRU page body from %s: %w", url, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("shared: close SRU page body from %s: %w", url, closeErr)
	}
	return body, nil
}

// sruRetryBackoff computes the capped exponential backoff delay to sleep
// after a failed attempt n (1-based): min(SRURetryBaseDelay<<(n-1),
// SRUMaxRetryDelay). The shift is clamped before it can overflow by bailing
// out to the cap as soon as doubling would meet or exceed it.
func sruRetryBackoff(attempt int) time.Duration {
	delay := SRURetryBaseDelay
	for i := 1; i < attempt; i++ {
		if delay >= SRUMaxRetryDelay {
			return SRUMaxRetryDelay
		}
		delay *= 2
	}
	if delay > SRUMaxRetryDelay {
		return SRUMaxRetryDelay
	}
	return delay
}

// FetchSRUAll pages endpoint for query to exhaustion, invoking yield for
// each record encountered. It fetches startRecord=1 first, reads
// numberOfRecords from that page, then keeps advancing startRecord by the
// number of records the previous page actually returned (not the requested
// page size, so a short/partial page is handled correctly) until every
// record has been yielded. Pages are rate-limited by SRURateInterval (no
// delay before the first request). Each page fetch is retried up to
// SRUMaxAttempts times with capped exponential backoff (see
// SRURetryBaseDelay/SRUMaxRetryDelay); each retry is logged via slog.Warn,
// and exhausting all attempts is logged via slog.Error before the wrapped
// error is returned. A page whose XML fails to parse is not retried, since
// that failure is deterministic. If yield returns an error,
// paging stops immediately and that error is returned. Returns the
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

		var body []byte
		var fetchErr error
		for attempt := 1; attempt <= SRUMaxAttempts; attempt++ {
			body, fetchErr = fetchSRUPage(ctx, httpGet, reqURL)
			if fetchErr == nil {
				break
			}
			if attempt < SRUMaxAttempts {
				delay := sruRetryBackoff(attempt)
				slog.Warn("shared: SRU page fetch failed; retrying",
					"startRecord", startRecord,
					"attempt", attempt,
					"maxAttempts", SRUMaxAttempts,
					"backoff", delay,
					"error", fetchErr,
				)
				sruSleep(delay)
			}
		}
		if fetchErr != nil {
			slog.Error("shared: SRU page fetch exhausted retries",
				"startRecord", startRecord,
				"attempts", SRUMaxAttempts,
				"error", fetchErr,
			)
			return 0, fetchErr
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

		if len(records) == 0 {
			break
		}
		startRecord += len(records)
		if startRecord > numberOfRecords {
			break
		}
	}
	return numberOfRecords, nil
}
