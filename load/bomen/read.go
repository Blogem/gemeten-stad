package bomen

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// geoJSONPage is the minimal GeoJSON FeatureCollection shape a landed geojson export line takes
// (stamgegevens, DATA_SOURCES.md §2a): each feature's flat properties plus its own geometry.
type geoJSONPage struct {
	Features []geoJSONFeature `json:"features"`
}

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

	var rows []map[string]any
	scanner := bufio.NewScanner(f)
	// Pages can be large (hundreds of rows each); grow past bufio.Scanner's 64KiB default.
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var envelope struct {
			Embedded map[string]json.RawMessage `json:"_embedded"`
			Features json.RawMessage            `json:"features"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			return nil, fmt.Errorf("bomen: decode landed line for %s: %w", artifact, err)
		}

		switch {
		case envelope.Embedded != nil:
			raw, ok := envelope.Embedded[embedKey]
			if !ok {
				continue
			}
			var pageRows []map[string]any
			if err := json.Unmarshal(raw, &pageRows); err != nil {
				return nil, fmt.Errorf("bomen: decode %s rows for %s: %w", embedKey, artifact, err)
			}
			rows = append(rows, pageRows...)

		case envelope.Features != nil:
			var page geoJSONPage
			if err := json.Unmarshal(line, &page); err != nil {
				return nil, fmt.Errorf("bomen: decode landed geojson page for %s: %w", artifact, err)
			}
			for _, feature := range page.Features {
				row := make(map[string]any, len(feature.Properties)+1)
				for k, v := range feature.Properties {
					row[k] = v
				}
				row["geometrie"] = feature.Geometry
				rows = append(rows, row)
			}

		default:
			return nil, fmt.Errorf("bomen: landed line for %s has neither _embedded nor features", artifact)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("bomen: scan landed %s: %w", artifact, err)
	}
	return rows, nil
}
