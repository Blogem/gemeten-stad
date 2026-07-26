package bag

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// feedURL is the PDOK atom feed for the LV BAG 2.0 Extract (Standaard-Levering, national).
const feedURL = "https://service.pdok.nl/kadaster/adressen/atom/v1_0/index.xml"

// ExtractName is the landing-store artifact name for the BAG extract.
const ExtractName = "lvbag-extract-nl.zip"

// maxFeedHops bounds how many nested atom feed levels Ingest will follow to reach the .zip
// download. PDOK's feed is two levels deep (top feed -> dataset sub-feed -> download); one extra
// hop of slack guards against the shape shifting slightly without risking an infinite loop.
const maxFeedHops = 3

// Ingest fetches the extract into store, skipping the download when already landed.
// It resolves the download URL by following the atom feed (and any nested dataset sub-feed),
// downloads it, and lands it with provenance. Refresh is a full reload: lvbag only reads the
// Standaard-Levering snapshot, there is no daily Mutatie-Levering delta to apply.
//
// httpGet performs a GET against url and returns the response body; a nil httpGet defaults to a
// plain http.Client fetch, so callers can inject a fake for network-free testing.
func Ingest(ctx context.Context, store *shared.RawStore, httpGet func(ctx context.Context, url string) (io.ReadCloser, error)) error {
	if httpGet == nil {
		httpGet = defaultHTTPGet
	}

	landed, err := store.Landed(ExtractName)
	if err != nil {
		return fmt.Errorf("bag: check whether %q is already landed: %w", ExtractName, err)
	}
	if landed {
		slog.Info("bag: already landed, skipping", "name", ExtractName)
		return nil
	}

	downloadURL, err := resolveDownloadURL(ctx, httpGet)
	if err != nil {
		return err
	}

	extractBody, err := httpGet(ctx, downloadURL)
	if err != nil {
		return fmt.Errorf("bag: fetch extract %q: %w", downloadURL, err)
	}
	defer func() { _ = extractBody.Close() }()

	if _, err := store.Land(ExtractName, extractBody, downloadURL, time.Now()); err != nil {
		return fmt.Errorf("bag: land %q: %w", ExtractName, err)
	}
	return nil
}

// resolveDownloadURL follows the PDOK atom feed from feedURL through any nested dataset
// sub-feeds, bounded by maxFeedHops, until it reaches the .zip download link.
func resolveDownloadURL(ctx context.Context, httpGet func(ctx context.Context, url string) (io.ReadCloser, error)) (string, error) {
	url := feedURL
	for hop := 0; hop < maxFeedHops; hop++ {
		feedBody, err := httpGet(ctx, url)
		if err != nil {
			return "", fmt.Errorf("bag: fetch atom feed %q: %w", url, err)
		}
		feedXML, err := io.ReadAll(feedBody)
		_ = feedBody.Close()
		if err != nil {
			return "", fmt.Errorf("bag: read atom feed %q: %w", url, err)
		}

		link, isDownload, err := ParseAtomFeed(feedXML)
		if err != nil {
			return "", fmt.Errorf("bag: resolve atom feed %q: %w", url, err)
		}
		if isDownload {
			return link, nil
		}
		url = link
	}
	return "", fmt.Errorf("bag: exceeded %d atom feed hops without finding a download link", maxFeedHops)
}

// defaultHTTPGet is the production httpGet: a thin GET wrapper over http.DefaultClient.
func defaultHTTPGet(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("bag: build request for %q: %w", url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bag: GET %q: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("bag: GET %q: unexpected status %s", url, resp.Status)
	}
	return resp.Body, nil
}
