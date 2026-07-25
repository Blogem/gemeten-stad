package gebieden

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// Datapunt API base and paging parameters (ported from spike-d harvest.py, page_all/get_json).
const (
	datapuntBase = "https://api.data.amsterdam.nl/v1"
	pageSize     = 200
)

// Landing artifact names.
const (
	BuurtenName = "gebieden_buurten.geojson"
	WijkenName  = "gebieden_wijken.geojson"
	CBSName     = "cbs_buurten.geojson"
)

// httpGetFunc fetches url with the given headers and returns the response body for the caller
// to read and close. Injected so Ingest (and its paging) can be tested without network access.
type httpGetFunc func(ctx context.Context, url string, headers map[string]string) (io.ReadCloser, error)

// gebiedenEndpoint is one Datapunt gebieden collection harvested whole-city — no stadsdeel
// filter, unlike spike-d's noord_scope(): every current buurt/wijk the API returns is kept.
type gebiedenEndpoint struct {
	path     string // e.g. "gebieden/buurten"
	embedKey string // e.g. "buurten" — the _embedded key paged rows are nested under
	artifact string // landing artifact name
}

var gebiedenEndpoints = []gebiedenEndpoint{
	{path: "gebieden/buurten", embedKey: "buurten", artifact: BuurtenName},
	{path: "gebieden/wijken", embedKey: "wijken", artifact: WijkenName},
}

// Ingest harvests buurten+wijken (whole gemeente 0363, no stadsdeel scoping) and the CBS
// "wijken en buurten" WFS reference into store, skipping any artifact already landed.
//
// The CBS load is best-effort: on failure it logs a warning and continues rather than failing
// the harvest — CBS is a cross-reference only (DATA_SOURCES.md §0); gebieden polygons remain
// the primary point-in-polygon set.
func Ingest(ctx context.Context, store *shared.RawStore, httpGet httpGetFunc) error {
	for _, ep := range gebiedenEndpoints {
		if err := ingestGebiedenEndpoint(ctx, store, httpGet, ep); err != nil {
			return err
		}
	}

	if err := ingestCBS(ctx, store, httpGet); err != nil {
		log.Printf("gebieden: CBS wijkenbuurten WFS landing failed (non-fatal, cross-reference only): %v", err)
	}

	return nil
}

// ingestGebiedenEndpoint lands one whole-city gebieden collection (buurten or wijken), skipping
// it if already landed.
func ingestGebiedenEndpoint(ctx context.Context, store *shared.RawStore, httpGet httpGetFunc, ep gebiedenEndpoint) error {
	landed, err := store.Landed(ep.artifact)
	if err != nil {
		return fmt.Errorf("gebieden: check landed %q: %w", ep.artifact, err)
	}
	if landed {
		return nil
	}

	records, err := pageAll(ctx, httpGet, ep.path, ep.embedKey)
	if err != nil {
		return fmt.Errorf("gebieden: harvest %s: %w", ep.path, err)
	}

	body, err := BuildFeatureCollection(records)
	if err != nil {
		return fmt.Errorf("gebieden: build feature collection for %s: %w", ep.path, err)
	}

	sourceURL := fmt.Sprintf("%s/%s?_format=json&_pageSize=%d", datapuntBase, ep.path, pageSize)
	if _, err := store.Land(ep.artifact, bytes.NewReader(body), sourceURL, time.Now().UTC()); err != nil {
		return fmt.Errorf("gebieden: land %q: %w", ep.artifact, err)
	}
	return nil
}

// ingestCBS lands the CBS "wijken en buurten" WFS reference, skipping it if already landed.
// Errors are returned to the caller, which treats this landing as best-effort (logs, continues).
func ingestCBS(ctx context.Context, store *shared.RawStore, httpGet httpGetFunc) error {
	landed, err := store.Landed(CBSName)
	if err != nil {
		return fmt.Errorf("check landed %q: %w", CBSName, err)
	}
	if landed {
		return nil
	}

	wfsURL := CBSWFSURL()
	rc, err := httpGet(ctx, wfsURL, nil)
	if err != nil {
		return fmt.Errorf("fetch CBS WFS: %w", err)
	}
	defer func() { _ = rc.Close() }()

	body, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("read CBS WFS response: %w", err)
	}

	if _, err := store.Land(CBSName, bytes.NewReader(body), wfsURL, time.Now().UTC()); err != nil {
		return fmt.Errorf("land %q: %w", CBSName, err)
	}
	return nil
}

// pageAll walks path's paged Datapunt collection (ported from spike-d harvest.py's page_all),
// requesting RD geometry via Accept-Crs, and returns every record across all pages. Stops once
// a page returns fewer than pageSize rows — the same paging quirk spike-a/spike-d observed.
func pageAll(ctx context.Context, httpGet httpGetFunc, path, embedKey string) ([]map[string]any, error) {
	var all []map[string]any
	headers := map[string]string{"Accept-Crs": "EPSG:28992"}

	for page := 1; ; page++ {
		reqURL := fmt.Sprintf("%s/%s?_format=json&_pageSize=%d&page=%d", datapuntBase, path, pageSize, page)
		rc, err := httpGet(ctx, reqURL, headers)
		if err != nil {
			return nil, fmt.Errorf("fetch %s page %d: %w", path, page, err)
		}

		var resp struct {
			Embedded map[string][]map[string]any `json:"_embedded"`
		}
		decodeErr := json.NewDecoder(rc).Decode(&resp)
		_ = rc.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode %s page %d: %w", path, page, decodeErr)
		}

		rows := resp.Embedded[embedKey]
		all = append(all, rows...)
		if len(rows) < pageSize {
			return all, nil
		}
	}
}
