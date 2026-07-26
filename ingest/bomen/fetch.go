package bomen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// httpGetFunc performs a GET against url and returns the response body for
// the caller to read and close.
type httpGetFunc func(ctx context.Context, url string) (io.ReadCloser, error)

// nextLinkEnvelope is the minimal shape this package needs from a DSO page
// response: the link to the next page, if any. The raw body is landed
// verbatim regardless — this envelope is only used to drive pagination,
// never to reshape what gets landed.
type nextLinkEnvelope struct {
	Links struct {
		Next struct {
			Href string `json:"href"`
		} `json:"next"`
	} `json:"_links"`
}

// geoJSONLinksEnvelope is the minimal shape this package needs from a DSO
// GeoJSON export response. Unlike the paged-JSON representation's _links
// object, the GeoJSON export's _links is an ARRAY of {href, rel, ...} link
// objects; the next page (if any) is the element whose Rel is "next".
type geoJSONLinksEnvelope struct {
	Links []struct {
		Href string `json:"href"`
		Rel  string `json:"rel"`
	} `json:"_links"`
}

// geoJSONNextLink extracts the next-page URL from a landed GeoJSON export
// response body, per geoJSONLinksEnvelope's array shape. It returns "" when
// there is no "next" link (the last page).
func geoJSONNextLink(body []byte) (string, error) {
	var env geoJSONLinksEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return "", fmt.Errorf("bomen: decode geojson links: %w", err)
	}
	for _, link := range env.Links {
		if link.Rel == "next" {
			return link.Href, nil
		}
	}
	return "", nil
}

// Ingest fetches both bomen sub-datasets (kapenherplant, stamgegevens) in
// full and lands each as a versioned artifact: every page fetched during
// the run is written as one JSONL line (the raw page body, compacted to a
// single line) into a buffer, and once the last page is fetched the whole
// buffer is landed in one call to store.LandVersion. LandVersion keeps
// every version ever landed — required here so past bomen snapshots remain
// available for audit even as the registry changes.
//
// httpGet performs the actual network fetch; it is always injected by the
// caller (cmd/pipeline builds it to carry the optional X-Api-Key header), so
// a nil httpGet is a programmer error and returns an error immediately
// rather than silently defaulting to an unauthenticated client.
func Ingest(ctx context.Context, store *shared.RawStore, httpGet httpGetFunc) error {
	if httpGet == nil {
		return fmt.Errorf("bomen: httpGet must not be nil")
	}

	for _, dataset := range []Dataset{Kapenherplant, Stamgegevens} {
		if err := ingestDataset(ctx, store, httpGet, dataset); err != nil {
			return err
		}
	}
	return nil
}

// ingestDataset fetches dataset in full per its Format, accumulating each
// response body as one JSONL line, then lands the accumulated run as a
// single new version of dataset.Artifact.
func ingestDataset(ctx context.Context, store *shared.RawStore, httpGet httpGetFunc, dataset Dataset) error {
	switch dataset.Format {
	case FormatGeoJSON:
		return ingestGeoJSON(ctx, store, httpGet, dataset)
	default:
		return ingestPagedJSON(ctx, store, httpGet, dataset)
	}
}

// ingestPagedJSON pages through dataset's default JSON HAL representation,
// following _links.next.href until exhausted, accumulating each page's raw
// body as one JSONL line, then lands the accumulated run as a single new
// version of dataset.Artifact.
func ingestPagedJSON(ctx context.Context, store *shared.RawStore, httpGet httpGetFunc, dataset Dataset) error {
	var buf bytes.Buffer

	url := firstPageURL(dataset.BaseURL, DefaultPageSize)
	for url != "" {
		body, nextURL, err := fetchPage(ctx, httpGet, url)
		if err != nil {
			return err
		}
		if err := json.Compact(&buf, body); err != nil {
			return fmt.Errorf("bomen: compact page body from %s: %w", url, err)
		}
		buf.WriteByte('\n')
		url = nextURL
	}

	sourceURL := fmt.Sprintf("%s?_pageSize=%d", dataset.BaseURL, DefaultPageSize)
	if _, err := store.LandVersion(dataset.Artifact, &buf, sourceURL, time.Now()); err != nil {
		return fmt.Errorf("bomen: land %q: %w", dataset.Artifact, err)
	}
	return nil
}

// ingestGeoJSON fetches dataset's `?_format=geojson` export page by page
// (`_pageSize` bounds each response — the full unpaged export is ~210MB, too
// large to buffer or land as one line), following each response's own
// _links "next"-rel entry until exhausted. Unlike the paged-JSON
// representation, the GeoJSON export is not page-capped at page 100. Each
// page's response body is accumulated as one JSONL line, then the
// accumulated run is landed as a single new version of dataset.Artifact.
func ingestGeoJSON(ctx context.Context, store *shared.RawStore, httpGet httpGetFunc, dataset Dataset) error {
	var buf bytes.Buffer

	url := dataset.BaseURL + "?_format=geojson&_pageSize=" + strconv.Itoa(DefaultPageSize)
	for url != "" {
		body, nextURL, err := fetchGeoJSONPage(ctx, httpGet, url)
		if err != nil {
			return err
		}
		if err := json.Compact(&buf, body); err != nil {
			return fmt.Errorf("bomen: compact geojson body from %s: %w", url, err)
		}
		buf.WriteByte('\n')
		url = nextURL
	}

	sourceURL := dataset.BaseURL + "?_format=geojson"
	if _, err := store.LandVersion(dataset.Artifact, &buf, sourceURL, time.Now()); err != nil {
		return fmt.Errorf("bomen: land %q: %w", dataset.Artifact, err)
	}
	return nil
}

// fetchPage fetches a single page at url, returning its raw body verbatim
// alongside the next page's URL (empty when this was the last page).
func fetchPage(ctx context.Context, httpGet httpGetFunc, url string) (body []byte, nextURL string, err error) {
	rc, err := httpGet(ctx, url)
	if err != nil {
		return nil, "", fmt.Errorf("bomen: fetch %s: %w", url, err)
	}
	defer func() { _ = rc.Close() }()

	body, err = io.ReadAll(rc)
	if err != nil {
		return nil, "", fmt.Errorf("bomen: read body from %s: %w", url, err)
	}

	var env nextLinkEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, "", fmt.Errorf("bomen: decode page from %s: %w", url, err)
	}
	return body, env.Links.Next.Href, nil
}

// fetchGeoJSONPage fetches a single GeoJSON export page at url, returning its
// raw body verbatim alongside the next page's URL (empty when this was the
// last page), per the GeoJSON export's array-shaped _links (geoJSONNextLink).
func fetchGeoJSONPage(ctx context.Context, httpGet httpGetFunc, url string) (body []byte, nextURL string, err error) {
	rc, err := httpGet(ctx, url)
	if err != nil {
		return nil, "", fmt.Errorf("bomen: fetch %s: %w", url, err)
	}
	defer func() { _ = rc.Close() }()

	body, err = io.ReadAll(rc)
	if err != nil {
		return nil, "", fmt.Errorf("bomen: read body from %s: %w", url, err)
	}

	nextURL, err = geoJSONNextLink(body)
	if err != nil {
		return nil, "", fmt.Errorf("bomen: %w (from %s)", err, url)
	}
	return body, nextURL, nil
}
