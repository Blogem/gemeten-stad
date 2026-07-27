package koop

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// gmbIDPattern matches a landed SRU record's filename stem: an id shaped "gmb-<year>-<number>",
// e.g. "gmb-2022-291126". Only files matching this are SRU records; everything else under koop/
// (the cursor file, provenance logs, metadata sidecars) is skipped or handled separately.
var gmbIDPattern = regexp.MustCompile(`^gmb-\d{4}-\d+$`)

// readPublications enumerates every landed SRU record under <store.BasePath>/koop, pairing each
// with its metadata sidecar (<id>.metadata.xml) when present, and assembles a Publication per
// record. store.RawStore has no listing/glob method, so this walks the directory itself
// (os.ReadDir) rather than relying on one.
//
// Skip rules applied while walking the directory:
//   - entries not ending in ".xml" (e.g. "_cursor.json", "*.prov.jsonl") are skipped;
//   - entries ending in ".metadata.xml" are skipped here — they are read as a sidecar once their
//     paired record is found, not iterated as records themselves;
//   - entries whose name starts with "_" are skipped (e.g. "_cursor.json");
//   - entries ending in ".prov.jsonl" are skipped (redundant with the ".xml" check above, kept
//     explicit per the landing convention).
//
// A record with no sidecar file on disk is a keyless publication: Zaaknummer == "" and
// RawMetadata == nil.
//
// A per-record failure (unreadable record, unparseable record, an unreadable sidecar, or an
// unparseable zaaknummer) skips that one record with a slog.Warn rather than aborting the whole
// run — only a failure to enumerate the landing directory at all (os.ReadDir) is fatal.
func readPublications(store *shared.RawStore) ([]Publication, error) {
	koopDir := filepath.Join(store.BasePath, "koop")
	entries, err := os.ReadDir(koopDir)
	if err != nil {
		return nil, fmt.Errorf("koop: read koop landing dir: %w", err)
	}

	var pubs []Publication
	var skipped int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		if strings.HasPrefix(name, "_") {
			continue
		}
		if !strings.HasSuffix(name, ".xml") {
			continue
		}
		if strings.HasSuffix(name, ".metadata.xml") {
			continue
		}
		if strings.HasSuffix(name, ".prov.jsonl") {
			continue
		}

		id := strings.TrimSuffix(name, ".xml")
		if !gmbIDPattern.MatchString(id) {
			continue
		}

		recordPath := filepath.Join(koopDir, name)
		rawRecord, err := os.ReadFile(recordPath)
		if err != nil {
			slog.Warn("koop: skipping unreadable record", "id", id, "err", err)
			skipped++
			continue
		}

		title, available, activiteit, point, err := parseRecord(rawRecord)
		if err != nil {
			slog.Warn("koop: skipping unparseable record", "id", id, "err", err)
			skipped++
			continue
		}

		var rawMetadata []byte
		var zaaknummer string
		sidecarPath := filepath.Join(koopDir, id+".metadata.xml")
		rawMetadata, err = os.ReadFile(sidecarPath)
		switch {
		case err == nil:
			zaaknummer, err = parseZaaknummer(rawMetadata)
			if err != nil {
				slog.Warn("koop: skipping record with unparseable zaaknummer", "id", id, "err", err)
				skipped++
				continue
			}
		case os.IsNotExist(err):
			rawMetadata = nil
			zaaknummer = ""
		default:
			slog.Warn("koop: skipping record with unreadable metadata sidecar", "id", id, "err", err)
			skipped++
			continue
		}

		postcode, huisnummer, street := extractAddress(title)

		pubs = append(pubs, Publication{
			ID:          id,
			Zaaknummer:  zaaknummer,
			Kind:        classifyKind(title),
			Activiteit:  activiteit,
			Point:       point,
			Postcode:    postcode,
			Huisnummer:  huisnummer,
			Street:      street,
			Title:       title,
			Available:   available,
			RawRecord:   rawRecord,
			RawMetadata: rawMetadata,
		})
	}

	slog.Info("koop: read publications", "loaded", len(pubs), "skipped", skipped)
	return pubs, nil
}
