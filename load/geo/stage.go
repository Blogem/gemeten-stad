package geo

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Blogem/gemeten-stad/ingest/shared"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Landed artifact names load/geo expects ingest/bag and ingest/gebieden to have already landed
// under the shared raw store (bronze). These names are the seam between the two ingest packages
// and this load package.
const (
	// ArtifactBAGExtract is the landed national LV BAG 2.0 Extract (the outer zip containing one
	// inner zip per BAG object type).
	ArtifactBAGExtract = "lvbag-extract-nl.zip"
	// ArtifactGebiedenBuurten is the landed whole-city gebieden buurten GeoJSON (RD).
	ArtifactGebiedenBuurten = "gebieden_buurten.geojson"
	// ArtifactGebiedenWijken is the landed whole-city gebieden wijken GeoJSON (RD).
	ArtifactGebiedenWijken = "gebieden_wijken.geojson"
	// ArtifactCBSBuurten is the landed CBS "wijken en buurten" cross-reference (RD).
	ArtifactCBSBuurten = "cbs_buurten.geojson"
)

// bagInnerZipDir is the subdirectory (relative to the raw store root) the BAG outer extract's
// per-object-type inner zips are extracted into.
const bagInnerZipDir = "bag"

// gdalDataRoot is the path the raw-data volume is mounted at INSIDE the gdal sidecar container
// (see deploy/compose/compose.yaml: `raw-data:/data`). The Go orchestrator and the sidecar see
// the same raw-data volume at two different paths — store.BasePath on this side, gdalDataRoot on
// the sidecar's — so every path handed to ogr2ogr is built from gdalDataRoot, never store.BasePath.
const gdalDataRoot = "/data"

// bagObjectCodes is the fixed set of BAG object types load/geo stages, in load order.
var bagObjectCodes = []string{"OPR", "NUM", "VBO", "LIG", "STA"}

// stageBAG stages every BAG object type from the landed extract into its *_staging table: it
// extracts each object type's inner zip (if not already extracted) from the outer extract, drops
// the object type's staging table so ogr2ogr always creates it fresh (no -overwrite/-append —
// see BAGStagingArgs), and runs the staging ogr2ogr invocation through the sidecar.
func stageBAG(ctx context.Context, pool *pgxpool.Pool, sc *shared.Sidecar, store *shared.RawStore, pgConn string) error {
	outerZip := filepath.Join(store.BasePath, ArtifactBAGExtract)

	for _, code := range bagObjectCodes {
		innerZipName, err := extractInnerZip(outerZip, filepath.Join(store.BasePath, bagInnerZipDir), code)
		if err != nil {
			return fmt.Errorf("geo: stage BAG %s: %w", code, err)
		}

		staging := StagingTable(code)
		if err := dropStagingTable(ctx, pool, staging); err != nil {
			return fmt.Errorf("geo: stage BAG %s: %w", code, err)
		}

		innerZipVsiPath := vsizipPath(path.Join(bagInnerZipDir, innerZipName))
		args := BAGStagingArgs(code, innerZipVsiPath, pgConn)
		if _, err := sc.OGR2OGR(ctx, args...); err != nil {
			return fmt.Errorf("geo: stage BAG %s: %w", code, err)
		}
	}
	return nil
}

// stagePolygons stages the whole-city gebieden buurt/wijk polygons and the CBS cross-reference
// into their *_staging tables, dropping each staging table first (see stageBAG).
func stagePolygons(ctx context.Context, pool *pgxpool.Pool, sc *shared.Sidecar, pgConn string) error {
	polygons := []struct {
		artifact string
		staging  string
		promote  bool
	}{
		{ArtifactGebiedenBuurten, "gebieden_buurten_staging", true},
		{ArtifactGebiedenWijken, "gebieden_wijken_staging", true},
		{ArtifactCBSBuurten, "cbs_buurten_staging", true},
	}

	for _, p := range polygons {
		if err := dropStagingTable(ctx, pool, p.staging); err != nil {
			return fmt.Errorf("geo: stage %s: %w", p.artifact, err)
		}

		landedFilePath := containerPath(p.artifact)
		args := PolygonStagingArgs(landedFilePath, pgConn, p.staging, p.promote)
		if _, err := sc.OGR2OGR(ctx, args...); err != nil {
			return fmt.Errorf("geo: stage %s: %w", p.artifact, err)
		}
	}
	return nil
}

// dropStagingTable drops a *_staging table if present, so the following ogr2ogr invocation
// always creates it fresh from the current extract (no stale rows from a prior load survive).
func dropStagingTable(ctx context.Context, pool *pgxpool.Pool, staging string) error {
	stmt := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE;", staging)
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("drop staging table %s: %w", staging, err)
	}
	return nil
}

// containerPath resolves rel (a path relative to the raw store root) to its absolute path as
// seen from inside the gdal sidecar container.
func containerPath(rel string) string {
	return path.Join(gdalDataRoot, rel)
}

// vsizipPath resolves rel (a path relative to the raw store root, identifying a file INSIDE a
// zip) to a GDAL /vsizip/ virtual path as seen from inside the gdal sidecar container. The
// container path is absolute (/data/...), so it is appended after "/vsizip/" verbatim, yielding
// the double-slash form GDAL requires for an absolute archive path (e.g. /vsizip//data/bag/x.zip);
// a single slash would make GDAL treat the archive path as relative to the working dir and fail.
func vsizipPath(rel string) string {
	return "/vsizip/" + containerPath(rel)
}

// extractInnerZip extracts the BAG outer extract's per-object-type inner zip for code (matching
// the "9999<code>*.zip" naming Kadaster ships, e.g. "9999OPR20260101.zip") into destDir, skipping
// the extraction if already present (the inner zips are landed once and reused, same idempotency
// stance as the raw store itself). Returns the extracted inner zip's file name.
func extractInnerZip(outerZipPath, destDir, code string) (string, error) {
	r, err := zip.OpenReader(outerZipPath)
	if err != nil {
		return "", fmt.Errorf("open BAG extract %q: %w", outerZipPath, err)
	}
	defer func() { _ = r.Close() }()

	prefix := "9999" + code
	var match *zip.File
	for _, f := range r.File {
		name := filepath.Base(f.Name)
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".zip") {
			match = f
			break
		}
	}
	if match == nil {
		return "", fmt.Errorf("no inner zip matching %q*.zip in %q", prefix, outerZipPath)
	}

	name := filepath.Base(match.Name)
	destPath := filepath.Join(destDir, name)

	if _, err := os.Stat(destPath); err == nil {
		return name, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat extracted inner zip %q: %w", destPath, err)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create BAG inner zip dir %q: %w", destDir, err)
	}

	src, err := match.Open()
	if err != nil {
		return "", fmt.Errorf("open inner zip entry %q: %w", match.Name, err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("create extracted inner zip %q: %w", destPath, err)
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("write extracted inner zip %q: %w", destPath, err)
	}

	return name, nil
}
