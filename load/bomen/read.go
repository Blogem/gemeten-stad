package bomen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// geoJSONFeature is the minimal GeoJSON feature shape a landed geojson export takes (stamgegevens,
// DATA_SOURCES.md §2a): each feature's flat properties plus its own geometry.
type geoJSONFeature struct {
	Properties map[string]any `json:"properties"`
	Geometry   map[string]any `json:"geometry"`
}

// readLandedRows reads the latest landed version of artifact from store (one newline-delimited
// JSON value per line — ingest/bomen.LandVersion's landing convention) and concatenates every
// line's rows into one slice for the whole snapshot. Each line is auto-detected as one of the two
// shapes this package lands verbatim:
//   - a paged-JSON HAL envelope (_embedded[embedKey] holds the row array — kapenherplant), or
//   - a GeoJSON FeatureCollection (features[].properties is the row, features[].geometry becomes
//     the row's "geometrie" field — stamgegevens).
func readLandedRows(store *shared.RawStore, artifact, embedKey string) ([]map[string]any, error) {
	relPath, ok, err := store.LatestVersion(artifact)
	if err != nil {
		return nil, fmt.Errorf("bomen: read landed %s: %w", artifact, err)
	}
	if !ok {
		return nil, fmt.Errorf("bomen: no landed version found for %s", artifact)
	}

	f, err := os.Open(filepath.Join(store.BasePath, relPath))
	if err != nil {
		return nil, fmt.Errorf("bomen: open landed %s: %w", artifact, err)
	}
	defer func() { _ = f.Close() }()

	// A landed artifact is a stream of one or more concatenated JSON values (a JSON value per
	// paged-JSON page for kapenherplant, or a single large FeatureCollection for the stamgegevens
	// geojson export — which can be ~210MB). A streaming json.Decoder reads value-by-value with no
	// line-length limit, so it handles both without buffering a whole line.
	var rows []map[string]any
	dec := json.NewDecoder(f)
	// UseNumber preserves each JSON number's exact source lexical form as json.Number instead of
	// decoding it into float64, which loses precision and renders large integer ids (e.g.
	// 4301189) in scientific notation. This matters for id/boomId, which must stay exact integer
	// strings to derive stable, canonical gs:Felling/gs:Tree IRIs.
	dec.UseNumber()
	for {
		var envelope struct {
			Embedded map[string]json.RawMessage `json:"_embedded"`
			Features []geoJSONFeature           `json:"features"`
		}
		if err := dec.Decode(&envelope); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("bomen: decode landed value for %s: %w", artifact, err)
		}

		switch {
		case envelope.Embedded != nil:
			raw, ok := envelope.Embedded[embedKey]
			if !ok {
				continue
			}
			var pageRows []map[string]any
			pageDec := json.NewDecoder(bytes.NewReader(raw))
			pageDec.UseNumber()
			if err := pageDec.Decode(&pageRows); err != nil {
				return nil, fmt.Errorf("bomen: decode %s rows for %s: %w", embedKey, artifact, err)
			}
			rows = append(rows, pageRows...)

		case envelope.Features != nil:
			for _, feature := range envelope.Features {
				row := make(map[string]any, len(feature.Properties)+1)
				for k, v := range feature.Properties {
					row[k] = v
				}
				row["geometrie"] = feature.Geometry
				rows = append(rows, row)
			}

		default:
			return nil, fmt.Errorf("bomen: landed value for %s has neither _embedded nor features", artifact)
		}
	}
	return rows, nil
}
