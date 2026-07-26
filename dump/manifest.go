package dump

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// formatVersion is the current bundle/manifest format version. Bump it if the bundle layout or
// manifest schema changes incompatibly.
const formatVersion = 1

// Bundle artifact filenames, fixed by the bundle layout (design D1).
const (
	ManifestFilename = "manifest.json"
	GraphFilename    = "graph.nq.gz"
	PostGISFilename  = "postgis.dump"
	NERCacheFilename = "ner-cache.tar.gz"
)

// ArtifactDescriptor describes one store's artifact within a bundle: enough to locate it
// (Filename), interpret it (Format), and validate it survived intact (Size, SHA256).
type ArtifactDescriptor struct {
	Filename string `json:"filename"`
	Format   string `json:"format"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// GraphDescriptor is the graph store's artifact descriptor, extended with the set of named
// graphs and whether the default graph carries any quads — restore needs both to know exactly
// which graphs to DROP (and whether to CLEAR DEFAULT) before loading the bundle back in,
// without having to re-parse the N-Quads-star artifact (design D2/D6).
type GraphDescriptor struct {
	ArtifactDescriptor
	NamedGraphs     []string `json:"named_graphs"`
	HasDefaultGraph bool     `json:"has_default_graph"`
}

// Manifest is a snapshot bundle's self-describing metadata (design D1): format version,
// creation time, tool version, and one descriptor per store.
type Manifest struct {
	FormatVersion int                `json:"format_version"`
	CreatedAt     time.Time          `json:"created_at"`
	ToolVersion   string             `json:"tool_version"`
	SkipBAG       bool               `json:"skip_bag"`
	Graph         GraphDescriptor    `json:"graph"`
	PostGIS       ArtifactDescriptor `json:"postgis"`
	NERCache      ArtifactDescriptor `json:"ner_cache"`
}

// BuildManifest assembles a Manifest from the artifacts already written to bundleDir. createdAt
// is passed in by the caller (the CLI layer), never read from a clock inside this function, so
// manifest assembly stays pure and testable.
func BuildManifest(createdAt time.Time, skipBAG bool, graph GraphDescriptor, postgis, nerCache ArtifactDescriptor) Manifest {
	return Manifest{
		FormatVersion: formatVersion,
		CreatedAt:     createdAt,
		ToolVersion:   ToolVersion(),
		SkipBAG:       skipBAG,
		Graph:         graph,
		PostGIS:       postgis,
		NERCache:      nerCache,
	}
}

// ToolVersion reports the running binary's VCS revision (from the embedded build info), or
// "unknown" if it is not available (e.g. `go run`).
func ToolVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return "unknown"
}

// ChecksumFile computes the sha256 of path's contents, hex-encoded, and returns its size.
func ChecksumFile(path string) (sha256Hex string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("dump: open %q for checksum: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	size, err = io.Copy(h, f)
	if err != nil {
		return "", 0, fmt.Errorf("dump: checksum %q: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// WriteManifest writes manifest as manifest.json under bundleDir.
func WriteManifest(bundleDir string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("dump: marshal manifest: %w", err)
	}
	path := filepath.Join(bundleDir, ManifestFilename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("dump: write manifest %q: %w", path, err)
	}
	return nil
}

// ReadManifest reads and parses manifest.json from bundleDir.
func ReadManifest(bundleDir string) (Manifest, error) {
	path := filepath.Join(bundleDir, ManifestFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("dump: read manifest %q: %w", path, err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("dump: parse manifest %q: %w", path, err)
	}
	return manifest, nil
}

// ValidateArtifact checks that the file at filepath.Join(bundleDir, desc.Filename) matches
// desc's recorded size and checksum, rejecting a corrupted or truncated bundle before restore
// touches any store.
func ValidateArtifact(bundleDir string, desc ArtifactDescriptor) error {
	path := filepath.Join(bundleDir, desc.Filename)
	sha256Hex, size, err := ChecksumFile(path)
	if err != nil {
		return err
	}
	if size != desc.Size {
		return fmt.Errorf("dump: artifact %q: size %d does not match manifest size %d", desc.Filename, size, desc.Size)
	}
	if sha256Hex != desc.SHA256 {
		return fmt.Errorf("dump: artifact %q: checksum mismatch (bundle may be corrupted)", desc.Filename)
	}
	return nil
}

// ValidateBundle validates every artifact a Manifest describes against bundleDir's contents.
func ValidateBundle(bundleDir string, manifest Manifest) error {
	if err := ValidateArtifact(bundleDir, manifest.Graph.ArtifactDescriptor); err != nil {
		return err
	}
	if err := ValidateArtifact(bundleDir, manifest.PostGIS); err != nil {
		return err
	}
	if err := ValidateArtifact(bundleDir, manifest.NERCache); err != nil {
		return err
	}
	return nil
}
