package koop

import (
	"fmt"
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
func readPublications(store *shared.RawStore) ([]Publication, error) {
	koopDir := filepath.Join(store.BasePath, "koop")
	entries, err := os.ReadDir(koopDir)
	if err != nil {
		return nil, fmt.Errorf("koop: read koop landing dir: %w", err)
	}

	var pubs []Publication
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
			return nil, fmt.Errorf("koop: read record %s: %w", name, err)
		}

		title, available, activiteit, point, err := parseRecord(rawRecord)
		if err != nil {
			return nil, fmt.Errorf("koop: parse record %s: %w", name, err)
		}

		var rawMetadata []byte
		var zaaknummer string
		sidecarPath := filepath.Join(koopDir, id+".metadata.xml")
		rawMetadata, err = os.ReadFile(sidecarPath)
		switch {
		case err == nil:
			zaaknummer, err = parseZaaknummer(rawMetadata)
			if err != nil {
				return nil, fmt.Errorf("koop: parse metadata sidecar %s: %w", id+".metadata.xml", err)
			}
		case os.IsNotExist(err):
			rawMetadata = nil
			zaaknummer = ""
		default:
			return nil, fmt.Errorf("koop: read metadata sidecar %s: %w", id+".metadata.xml", err)
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

	return pubs, nil
}
