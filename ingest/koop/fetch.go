package koop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// validIdentifier is the strict shape of a KOOP gmb (Gemeenteblad) identifier, e.g.
// "gmb-2022-291126". Anchored and restricted to digits/hyphens so a hostile
// dcterms:identifier (e.g. containing "/" or "..") can never be used to build a path that
// escapes the intended koop/ landing prefix.
var validIdentifier = regexp.MustCompile(`^gmb-\d{4}-\d+$`)

// Ingest harvests Amsterdam kap/verplant omgevingsvergunningen into store: it reads the
// persisted dt.available high-water mark (defaulting to DefaultSinceDate on a clean volume),
// pages the scoped SRU query (D1) via shared.FetchSRUAll from (high-water mark - OverlapDays),
// lands each new or changed publication verbatim under a koop/ prefix (D2/D4), skips unchanged
// already-landed publications (id dedup), and persists the advanced high-water mark once paging
// completes.
//
// httpGet is always injected by the caller (cmd/pipeline builds an unauthenticated getter for
// KOOP), so a nil httpGet is a programmer error and returns an error immediately.
func Ingest(ctx context.Context, store *shared.RawStore, httpGet shared.HTTPGetFunc) error {
	if httpGet == nil {
		return fmt.Errorf("koop: httpGet must not be nil")
	}

	hwm, err := readCursor(store)
	if err != nil {
		return err
	}

	lowerBound, err := queryLowerBound(hwm)
	if err != nil {
		return err
	}
	query := BuildQuery(lowerBound)

	// A representative request URL for provenance: FetchSRUAll pages internally, so the exact
	// per-page URL isn't threaded through yield; the startRecord=1 request for this run's query
	// identifies the harvest that produced each landed artifact just as well.
	sourceURL := shared.SRURequestURL(SRUEndpoint, query, 1, shared.SRUPageSize)
	fetchedAt := time.Now().UTC()

	maxAvailable := hwm
	numberOfRecords, err := shared.FetchSRUAll(ctx, httpGet, SRUEndpoint, query, func(rec shared.SRURecord) error {
		if rec.Available > maxAvailable {
			maxAvailable = rec.Available
		}
		return landRecord(store, rec, sourceURL, fetchedAt)
	})
	if err != nil {
		return fmt.Errorf("koop: harvest: %w", err)
	}

	// Logged so a sudden drop to 0 (the §1 silent-zero SRU quirk: an index typo or an
	// over-narrow clause returns 0 results, never an error) is visible in run output.
	slog.Info("koop: SRU harvest complete", "numberOfRecords", numberOfRecords, "since", lowerBound)

	if err := writeCursor(store, maxAvailable); err != nil {
		return err
	}
	return nil
}

// landRecord lands rec verbatim under a koop/ prefix (D2), unless its identifier does not match
// the strict gmb-<year>-<number> shape (defensive: rec.Identifier is external input and is used
// to build a landing path, so anything not matching validIdentifier — including path-traversal
// payloads like "gmb-../../etc/passwd" — is skipped, not landed) or it is already landed with
// identical content (D4 no-op skip). A landed record whose content hash has changed is re-landed,
// which overwrites the artifact bytes and appends a new provenance record.
func landRecord(store *shared.RawStore, rec shared.SRURecord, sourceURL string, fetchedAt time.Time) error {
	if !validIdentifier.MatchString(rec.Identifier) {
		slog.Warn("koop: skipping record with invalid identifier", "identifier", rec.Identifier)
		return nil
	}

	name := "koop/" + rec.Identifier + ".xml"

	landed, err := store.Landed(name)
	if err != nil {
		return fmt.Errorf("koop: check landed %q: %w", name, err)
	}
	if landed {
		unchanged, err := contentUnchanged(store, name, rec.InnerXML)
		if err != nil {
			return err
		}
		if unchanged {
			return nil
		}
	}

	if _, err := store.Land(name, bytes.NewReader(rec.InnerXML), sourceURL, fetchedAt); err != nil {
		return fmt.Errorf("koop: land %q: %w", name, err)
	}
	return nil
}

// contentUnchanged reports whether newContent's sha256 matches the already-landed artifact
// name's bytes on disk.
func contentUnchanged(store *shared.RawStore, name string, newContent []byte) (bool, error) {
	path := filepath.Join(store.BasePath, name)
	existing, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("koop: read landed artifact %q: %w", path, err)
	}
	newSum := sha256.Sum256(newContent)
	existingSum := sha256.Sum256(existing)
	return newSum == existingSum, nil
}
