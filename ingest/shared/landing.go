package shared

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Provenance is the sidecar record written next to a landed artifact.
type Provenance struct {
	SourceURL     string    `json:"source_url"`
	FetchedAt     time.Time `json:"fetched_at"`
	ByteSize      int64     `json:"byte_size"`
	ContentSHA256 string    `json:"content_sha256"`
	// Version identifies the landed version when written via LandVersion (the fetch
	// timestamp formatted as "20060102T150405Z" UTC). Empty for records written by Land.
	Version string `json:"version,omitempty"`
}

// RawStore lands source artifacts as files under BasePath (the raw landing store, bronze).
type RawStore struct {
	BasePath string
}

// NewRawStore returns a RawStore rooted at basePath.
func NewRawStore(basePath string) *RawStore {
	return &RawStore{BasePath: basePath}
}

// RawDataPath resolves GS_RAW_DATA_PATH from env; error (fail loud) if unset/empty.
func RawDataPath(env func(string) string) (string, error) {
	path := env("GS_RAW_DATA_PATH")
	if path == "" {
		return "", fmt.Errorf("shared: GS_RAW_DATA_PATH is not set")
	}
	return path, nil
}

// artifactPath resolves the on-disk path of the landed artifact named name.
func (s *RawStore) artifactPath(name string) string {
	return filepath.Join(s.BasePath, name)
}

// provenancePath resolves the on-disk path of name's history-preserving provenance log.
func (s *RawStore) provenancePath(name string) string {
	return s.artifactPath(name) + ".prov.jsonl"
}

// Landed reports whether an artifact named `name` is already present in the store.
func (s *RawStore) Landed(name string) (bool, error) {
	_, err := os.Stat(s.artifactPath(name))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("shared: stat landed artifact %q: %w", name, err)
}

// Land writes r's bytes to <BasePath>/<name>, computing byte size + sha256, and appends a
// provenance record. Provenance is HISTORY-PRESERVING: a refresh appends a new dated record,
// never erasing prior provenance. The artifact bytes themselves may be overwritten (latest
// snapshot), but provenance is retained. Returns the Provenance just written.
func (s *RawStore) Land(name string, r io.Reader, sourceURL string, fetchedAt time.Time) (Provenance, error) {
	if err := os.MkdirAll(s.BasePath, 0o755); err != nil {
		return Provenance{}, fmt.Errorf("shared: create raw store dir %q: %w", s.BasePath, err)
	}

	dst, err := os.Create(s.artifactPath(name))
	if err != nil {
		return Provenance{}, fmt.Errorf("shared: create artifact %q: %w", name, err)
	}
	defer func() { _ = dst.Close() }()

	h := sha256.New()
	size, err := io.Copy(dst, io.TeeReader(r, h))
	if err != nil {
		return Provenance{}, fmt.Errorf("shared: write artifact %q: %w", name, err)
	}

	prov := Provenance{
		SourceURL:     sourceURL,
		FetchedAt:     fetchedAt,
		ByteSize:      size,
		ContentSHA256: hex.EncodeToString(h.Sum(nil)),
	}

	if err := s.appendProvenance(name, prov); err != nil {
		return Provenance{}, err
	}

	return prov, nil
}

// appendProvenance appends prov as one JSON line to name's provenance log, preserving any
// records already written for earlier refreshes.
func (s *RawStore) appendProvenance(name string, prov Provenance) error {
	f, err := os.OpenFile(s.provenancePath(name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("shared: open provenance log for %q: %w", name, err)
	}
	defer func() { _ = f.Close() }()

	line, err := json.Marshal(prov)
	if err != nil {
		return fmt.Errorf("shared: marshal provenance for %q: %w", name, err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("shared: append provenance for %q: %w", name, err)
	}
	return nil
}

// ProvenanceHistory returns all provenance records ever written for `name`, oldest first.
func (s *RawStore) ProvenanceHistory(name string) ([]Provenance, error) {
	data, err := os.ReadFile(s.provenancePath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("shared: read provenance log for %q: %w", name, err)
	}

	var history []Provenance
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var prov Provenance
		if err := json.Unmarshal(line, &prov); err != nil {
			return nil, fmt.Errorf("shared: parse provenance log for %q: %w", name, err)
		}
		history = append(history, prov)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("shared: scan provenance log for %q: %w", name, err)
	}
	return history, nil
}

// versionsDir resolves the directory holding every landed version of name, one file per
// version plus a version-scoped provenance log.
func (s *RawStore) versionsDir(name string) string {
	return filepath.Join(s.BasePath, name)
}

// versionsProvenancePath resolves the version-scoped provenance log for name, distinct from
// Land's <name>.prov.jsonl.
func (s *RawStore) versionsProvenancePath(name string) string {
	return filepath.Join(s.versionsDir(name), "provenance.jsonl")
}

// LandVersion writes r's bytes as a new version of the artifact named name, KEEPING every
// prior version (unlike Land, which overwrites the single landed file). Layout:
// <BasePath>/<name>/<version> per landed file, where version is fetchedAt formatted as
// "20060102T150405Z" UTC (sorts chronologically as a plain string). A per-name
// <BasePath>/<name>/provenance.jsonl log records one Provenance per version ever landed.
//
// LandVersion is content-hash idempotent: if the latest existing version's content sha256
// equals the new content's sha256, no new version is written and the existing latest
// Provenance is returned unchanged.
func (s *RawStore) LandVersion(name string, r io.Reader, sourceURL string, fetchedAt time.Time) (Provenance, error) {
	dir := s.versionsDir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Provenance{}, fmt.Errorf("shared: create versions dir %q: %w", name, err)
	}

	tmp, err := os.CreateTemp(dir, ".landversion-*")
	if err != nil {
		return Provenance{}, fmt.Errorf("shared: create temp version file for %q: %w", name, err)
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	h := sha256.New()
	size, err := io.Copy(tmp, io.TeeReader(r, h))
	closeErr := tmp.Close()
	if err != nil {
		return Provenance{}, fmt.Errorf("shared: write version for %q: %w", name, err)
	}
	if closeErr != nil {
		return Provenance{}, fmt.Errorf("shared: close temp version file for %q: %w", name, closeErr)
	}
	contentSHA256 := hex.EncodeToString(h.Sum(nil))

	history, err := s.versionProvenanceHistory(name)
	if err != nil {
		return Provenance{}, err
	}
	if len(history) > 0 {
		if latest := history[len(history)-1]; latest.ContentSHA256 == contentSHA256 {
			return latest, nil
		}
	}

	version := fetchedAt.UTC().Format("20060102T150405Z")
	if err := os.Rename(tmpPath, filepath.Join(dir, version)); err != nil {
		return Provenance{}, fmt.Errorf("shared: finalize version %q for %q: %w", version, name, err)
	}
	removeTmp = false

	prov := Provenance{
		SourceURL:     sourceURL,
		FetchedAt:     fetchedAt,
		ByteSize:      size,
		ContentSHA256: contentSHA256,
		Version:       version,
	}

	if err := s.appendVersionProvenance(name, prov); err != nil {
		return Provenance{}, err
	}

	return prov, nil
}

// appendVersionProvenance appends prov as one JSON line to name's version-scoped provenance
// log, preserving every prior version record.
func (s *RawStore) appendVersionProvenance(name string, prov Provenance) error {
	f, err := os.OpenFile(s.versionsProvenancePath(name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("shared: open version provenance log for %q: %w", name, err)
	}
	defer func() { _ = f.Close() }()

	line, err := json.Marshal(prov)
	if err != nil {
		return fmt.Errorf("shared: marshal version provenance for %q: %w", name, err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("shared: append version provenance for %q: %w", name, err)
	}
	return nil
}

// versionProvenanceHistory returns every provenance record for name's versioned landings,
// oldest first.
func (s *RawStore) versionProvenanceHistory(name string) ([]Provenance, error) {
	data, err := os.ReadFile(s.versionsProvenancePath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("shared: read version provenance log for %q: %w", name, err)
	}

	var history []Provenance
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var prov Provenance
		if err := json.Unmarshal(line, &prov); err != nil {
			return nil, fmt.Errorf("shared: parse version provenance log for %q: %w", name, err)
		}
		history = append(history, prov)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("shared: scan version provenance log for %q: %w", name, err)
	}
	return history, nil
}

// LatestVersion returns relPath (relative to BasePath, e.g. via filepath.Join(BasePath,
// relPath)) of the most recently landed version of name: the lexicographically greatest
// version entry under <BasePath>/<name>/ (the "20060102T150405Z" format sorts chronologically
// as a plain string). ok is false, with no error, if no version has been landed yet.
func (s *RawStore) LatestVersion(name string) (relPath string, ok bool, err error) {
	entries, err := os.ReadDir(s.versionsDir(name))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("shared: read versions dir %q: %w", name, err)
	}

	var latest string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || n == "provenance.jsonl" || strings.HasPrefix(n, ".") {
			continue
		}
		if n > latest {
			latest = n
		}
	}
	if latest == "" {
		return "", false, nil
	}
	return filepath.Join(name, latest), true, nil
}
