package bomen

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// dsoPage is the minimal Datapunt bomen DSO API HAL envelope shape needed to pull a page's row
// array back out of a landed page body: the rows live under _embedded[<dataset name>]
// (docs/DATA_SOURCES.md §2a).
type dsoPage struct {
	Embedded map[string]json.RawMessage `json:"_embedded"`
}

// readLandedRows reads the latest landed version of artifact from store (one newline-delimited
// raw page body per line — ingest/bomen.LandVersion's landing convention), decodes each line as a
// dsoPage, and concatenates every page's embedKey row array into one slice for the whole snapshot.
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

		var page dsoPage
		if err := json.Unmarshal(line, &page); err != nil {
			return nil, fmt.Errorf("bomen: decode landed page for %s: %w", artifact, err)
		}

		raw, ok := page.Embedded[embedKey]
		if !ok {
			continue
		}
		var pageRows []map[string]any
		if err := json.Unmarshal(raw, &pageRows); err != nil {
			return nil, fmt.Errorf("bomen: decode %s rows for %s: %w", embedKey, artifact, err)
		}
		rows = append(rows, pageRows...)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("bomen: scan landed %s: %w", artifact, err)
	}
	return rows, nil
}
