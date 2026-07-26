package dump

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CaptureNERCache tars+gzips cachePath's contents to w, one entry per file under cachePath
// (relative, slash-separated paths, so the archive is portable across hosts). An absent
// cachePath produces a valid, empty archive: the NER cache is only populated in Phase 2, so dump
// must round-trip cleanly before any entry exists.
func CaptureNERCache(cachePath string, w io.Writer) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	if _, err := os.Stat(cachePath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("dump: stat NER cache path %q: %w", cachePath, err)
		}
		if err := tw.Close(); err != nil {
			return fmt.Errorf("dump: close empty NER cache archive: %w", err)
		}
		return gz.Close()
	}

	walkErr := filepath.WalkDir(cachePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		return addNERCacheEntry(tw, cachePath, path, d)
	})
	if walkErr != nil {
		return walkErr
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("dump: close NER cache archive: %w", err)
	}
	return gz.Close()
}

func addNERCacheEntry(tw *tar.Writer, cachePath, path string, d fs.DirEntry) error {
	rel, err := filepath.Rel(cachePath, path)
	if err != nil {
		return fmt.Errorf("dump: relative path for %q: %w", path, err)
	}

	info, err := d.Info()
	if err != nil {
		return fmt.Errorf("dump: stat %q: %w", path, err)
	}

	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("dump: build tar header for %q: %w", path, err)
	}
	hdr.Name = filepath.ToSlash(rel)

	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("dump: write tar header for %q: %w", path, err)
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("dump: open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("dump: write %q to archive: %w", path, err)
	}
	return nil
}

// RestoreNERCache extracts the tar+gzip archive read from r into cachePath, creating it if
// necessary. Same-key entries are overwritten; entries not present in the archive are left in
// place (restore is additive — design D6).
func RestoreNERCache(r io.Reader, cachePath string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("dump: open NER cache archive: %w", err)
	}
	defer func() { _ = gz.Close() }()

	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		return fmt.Errorf("dump: create NER cache dir %q: %w", cachePath, err)
	}

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("dump: read NER cache archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if err := extractNERCacheEntry(tr, cachePath, hdr.Name); err != nil {
			return err
		}
	}
}

// extractNERCacheEntry writes one tar entry's content to <cachePath>/<name>, rejecting any name
// that would escape cachePath (a corrupted or hostile archive must not write outside the cache
// directory — the classic "zip slip" path-traversal case).
func extractNERCacheEntry(r io.Reader, cachePath, name string) error {
	dest := filepath.Join(cachePath, filepath.FromSlash(name))
	if dest != filepath.Clean(cachePath) && !strings.HasPrefix(dest, filepath.Clean(cachePath)+string(os.PathSeparator)) {
		return fmt.Errorf("dump: NER cache archive entry %q escapes %q", name, cachePath)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("dump: create dir for %q: %w", dest, err)
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("dump: create %q: %w", dest, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("dump: write %q: %w", dest, err)
	}
	return nil
}
