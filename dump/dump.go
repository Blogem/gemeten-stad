package dump

import (
	"compress/gzip"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// ExportOptions configures a dump export run.
type ExportOptions struct {
	// SkipBAG excludes the BAG tables from the PostGIS artifact (design D3) — a space-constrained
	// snapshot that additive restore (design D6) can still apply without disturbing BAG data
	// already present in the target.
	SkipBAG bool
}

// Export snapshots the graph, PostGIS, and NER cache from cfg's stores into a single
// self-describing bundle at outDir, in that order, writing the manifest last so a failed or
// partial export is never mistaken for a complete bundle (ReadManifest simply fails on it).
// Export never mutates a source store. createdAt is the manifest's recorded creation time,
// passed in by the caller rather than read from a clock inside this function.
func Export(ctx context.Context, cfg Config, outDir string, opts ExportOptions, createdAt time.Time) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("dump: create bundle dir %q: %w", outDir, err)
	}

	if err := CheckPgTools(ctx, cfg.DatabaseURL); err != nil {
		return err
	}

	log.Printf("dump: exporting graph from %s", cfg.FusekiURL)
	graphDesc, err := exportGraphArtifact(ctx, cfg.FusekiURL, outDir)
	if err != nil {
		return fmt.Errorf("dump: export graph: %w", err)
	}

	log.Printf("dump: exporting PostGIS (skip-bag=%t)", opts.SkipBAG)
	postgisDesc, err := exportPostGISArtifact(ctx, cfg.DatabaseURL, outDir, opts.SkipBAG)
	if err != nil {
		return fmt.Errorf("dump: export postgis: %w", err)
	}

	log.Printf("dump: capturing NER cache from %s", cfg.NERCachePath)
	nerCacheDesc, err := captureNERCacheArtifact(cfg.NERCachePath, outDir)
	if err != nil {
		return fmt.Errorf("dump: capture NER cache: %w", err)
	}

	manifest := BuildManifest(createdAt, opts.SkipBAG, graphDesc, postgisDesc, nerCacheDesc)
	if err := WriteManifest(outDir, manifest); err != nil {
		return fmt.Errorf("dump: write manifest: %w", err)
	}

	log.Printf("dump: export complete: %s", outDir)
	return nil
}

// Restore rebuilds the graph, PostGIS, and NER cache from the bundle at inDir into cfg's stores,
// in that order, additively (design D6): each object, named graph, or cache entry the bundle
// carries replaces the corresponding target content exactly; everything else in the target is
// left untouched. The bundle's manifest and artifacts are validated before any store is touched.
func Restore(ctx context.Context, cfg Config, inDir string) error {
	manifest, err := ReadManifest(inDir)
	if err != nil {
		return err
	}
	if err := ValidateBundle(inDir, manifest); err != nil {
		return fmt.Errorf("dump: validate bundle %q: %w", inDir, err)
	}

	if err := CheckPgTools(ctx, cfg.DatabaseURL); err != nil {
		return err
	}

	log.Printf("dump: restoring graph into %s", cfg.FusekiURL)
	if err := restoreGraphArtifact(ctx, cfg.FusekiURL, inDir, manifest.Graph); err != nil {
		return fmt.Errorf("dump: restore graph: %w", err)
	}

	log.Printf("dump: restoring PostGIS into %s (bundle skip-bag=%t)", cfg.DatabaseURL, manifest.SkipBAG)
	if err := RestorePostGIS(ctx, cfg.DatabaseURL, filepath.Join(inDir, manifest.PostGIS.Filename)); err != nil {
		return fmt.Errorf("dump: restore postgis: %w", err)
	}

	log.Printf("dump: restoring NER cache into %s", cfg.NERCachePath)
	if err := restoreNERCacheArtifact(inDir, manifest.NERCache, cfg.NERCachePath); err != nil {
		return fmt.Errorf("dump: restore NER cache: %w", err)
	}

	log.Printf("dump: restore complete")
	return nil
}

func exportGraphArtifact(ctx context.Context, fusekiURL, outDir string) (GraphDescriptor, error) {
	namedGraphs, err := NamedGraphs(ctx, fusekiURL)
	if err != nil {
		return GraphDescriptor{}, err
	}
	hasDefault, err := HasDefaultGraphData(ctx, fusekiURL)
	if err != nil {
		return GraphDescriptor{}, err
	}

	path := filepath.Join(outDir, GraphFilename)
	f, err := os.Create(path)
	if err != nil {
		return GraphDescriptor{}, fmt.Errorf("dump: create %q: %w", path, err)
	}
	exportErr := ExportGraph(ctx, fusekiURL, f)
	closeErr := f.Close()
	if exportErr != nil {
		return GraphDescriptor{}, exportErr
	}
	if closeErr != nil {
		return GraphDescriptor{}, fmt.Errorf("dump: close %q: %w", path, closeErr)
	}

	sha256Hex, size, err := ChecksumFile(path)
	if err != nil {
		return GraphDescriptor{}, err
	}

	return GraphDescriptor{
		ArtifactDescriptor: ArtifactDescriptor{
			Filename: GraphFilename,
			Format:   "application/n-quads+gzip",
			Size:     size,
			SHA256:   sha256Hex,
		},
		NamedGraphs:     namedGraphs,
		HasDefaultGraph: hasDefault,
	}, nil
}

func restoreGraphArtifact(ctx context.Context, fusekiURL, inDir string, desc GraphDescriptor) error {
	path := filepath.Join(inDir, desc.Filename)
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("dump: open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("dump: open gzip %q: %w", path, err)
	}
	defer func() { _ = gz.Close() }()

	return RestoreGraph(ctx, fusekiURL, desc.NamedGraphs, desc.HasDefaultGraph, gz)
}

func exportPostGISArtifact(ctx context.Context, databaseURL, outDir string, skipBAG bool) (ArtifactDescriptor, error) {
	path := filepath.Join(outDir, PostGISFilename)
	if err := ExportPostGIS(ctx, databaseURL, path, skipBAG); err != nil {
		return ArtifactDescriptor{}, err
	}

	sha256Hex, size, err := ChecksumFile(path)
	if err != nil {
		return ArtifactDescriptor{}, err
	}
	return ArtifactDescriptor{
		Filename: PostGISFilename,
		Format:   "application/vnd.pgdump",
		Size:     size,
		SHA256:   sha256Hex,
	}, nil
}

func captureNERCacheArtifact(cachePath, outDir string) (ArtifactDescriptor, error) {
	path := filepath.Join(outDir, NERCacheFilename)
	f, err := os.Create(path)
	if err != nil {
		return ArtifactDescriptor{}, fmt.Errorf("dump: create %q: %w", path, err)
	}
	captureErr := CaptureNERCache(cachePath, f)
	closeErr := f.Close()
	if captureErr != nil {
		return ArtifactDescriptor{}, captureErr
	}
	if closeErr != nil {
		return ArtifactDescriptor{}, fmt.Errorf("dump: close %q: %w", path, closeErr)
	}

	sha256Hex, size, err := ChecksumFile(path)
	if err != nil {
		return ArtifactDescriptor{}, err
	}
	return ArtifactDescriptor{
		Filename: NERCacheFilename,
		Format:   "application/tar+gzip",
		Size:     size,
		SHA256:   sha256Hex,
	}, nil
}

func restoreNERCacheArtifact(inDir string, desc ArtifactDescriptor, cachePath string) error {
	path := filepath.Join(inDir, desc.Filename)
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("dump: open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	return RestoreNERCache(f, cachePath)
}
