package koop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// CursorArtifact is the plain JSON cursor sidecar (relative to the raw store's BasePath)
// persisting the dt.available high-water mark between runs: {"high_water_mark":"YYYY-MM-DD"}.
// It is written directly via os.WriteFile, NEVER through shared.RawStore.Land: it is harvest
// bookkeeping, not a source artifact, so it must not get a provenance sidecar.
const CursorArtifact = "koop/_cursor.json"

// OverlapDays is the dt.available re-query overlap (D3): each run queries from the persisted
// high-water mark minus this many days, so same-day and late-arriving back-dated publications
// are not missed. Cheap because id-dedup skips already-landed records.
const OverlapDays = 7

// cursorDateLayout is the dt.available / high-water-mark date format ("YYYY-MM-DD").
const cursorDateLayout = "2006-01-02"

// cursorFile is the on-disk shape of the CursorArtifact sidecar.
type cursorFile struct {
	HighWaterMark string `json:"high_water_mark"`
}

// readCursor reads the persisted dt.available high-water mark from store's CursorArtifact,
// defaulting to DefaultSinceDate when the cursor file does not exist yet (first run on a clean
// volume).
func readCursor(store *shared.RawStore) (string, error) {
	path := filepath.Join(store.BasePath, CursorArtifact)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultSinceDate, nil
		}
		return "", fmt.Errorf("koop: read cursor %q: %w", path, err)
	}

	var cf cursorFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return "", fmt.Errorf("koop: parse cursor %q: %w", path, err)
	}
	if cf.HighWaterMark == "" {
		return DefaultSinceDate, nil
	}
	return cf.HighWaterMark, nil
}

// writeCursor persists highWaterMark to store's CursorArtifact, creating the koop/ directory as
// needed. Written directly (not via RawStore.Land): a cursor is bookkeeping, not a landed source
// artifact, and must not accumulate a provenance history.
func writeCursor(store *shared.RawStore, highWaterMark string) error {
	dir := filepath.Dir(filepath.Join(store.BasePath, CursorArtifact))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("koop: create cursor dir %q: %w", dir, err)
	}

	data, err := json.Marshal(cursorFile{HighWaterMark: highWaterMark})
	if err != nil {
		return fmt.Errorf("koop: marshal cursor: %w", err)
	}

	path := filepath.Join(store.BasePath, CursorArtifact)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("koop: write cursor %q: %w", path, err)
	}
	return nil
}

// queryLowerBound returns hwm (a "YYYY-MM-DD" high-water mark) minus OverlapDays, as
// "YYYY-MM-DD" — the dt.available>= floor passed to BuildQuery. The result is clamped to never
// go below DefaultSinceDate: on a first run (hwm == DefaultSinceDate) the overlap would otherwise
// push the floor before 2021-01-01, but the spec requires a clean-volume run to query from
// exactly the 2021-01-01 lower bound. Pure: no I/O.
func queryLowerBound(hwm string) (string, error) {
	t, err := time.Parse(cursorDateLayout, hwm)
	if err != nil {
		return "", fmt.Errorf("koop: parse high-water mark %q: %w", hwm, err)
	}

	floor, err := time.Parse(cursorDateLayout, DefaultSinceDate)
	if err != nil {
		return "", fmt.Errorf("koop: parse default since date %q: %w", DefaultSinceDate, err)
	}

	lowerBound := t.AddDate(0, 0, -OverlapDays)
	if lowerBound.Before(floor) {
		lowerBound = floor
	}
	return lowerBound.Format(cursorDateLayout), nil
}
