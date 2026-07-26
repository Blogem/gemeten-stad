package dump

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// pgVersionNumberRe extracts the leading integer from a Postgres version string, e.g. "18" from
// "pg_dump (PostgreSQL) 18.4" or "180004" from server_version_num.
var pgVersionNumberRe = regexp.MustCompile(`\d+`)

// CheckPgTools verifies pg_dump and pg_restore are on PATH and that pg_dump's major version
// matches databaseURL's server major version, so export/restore fail fast with a clear error
// before touching any store (spec: "Export fails fast when a required tool is missing").
func CheckPgTools(ctx context.Context, databaseURL string) error {
	for _, bin := range []string{"pg_dump", "pg_restore"} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("dump: required tool %q not found in PATH: %w", bin, err)
		}
	}

	clientMajor, err := pgDumpMajorVersion(ctx)
	if err != nil {
		return err
	}

	serverMajor, err := postgresServerMajorVersion(ctx, databaseURL)
	if err != nil {
		return err
	}

	if clientMajor != serverMajor {
		return fmt.Errorf(
			"dump: pg_dump major version %d does not match server major version %d — install a matching pg_dump/pg_restore",
			clientMajor, serverMajor,
		)
	}
	return nil
}

func pgDumpMajorVersion(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "pg_dump", "--version").Output()
	if err != nil {
		return 0, fmt.Errorf("dump: run pg_dump --version: %w", err)
	}
	return parsePgMajorVersion(string(out), 1)
}

func postgresServerMajorVersion(ctx context.Context, databaseURL string) (int, error) {
	pool, err := shared.ConnectPostgres(ctx, databaseURL)
	if err != nil {
		return 0, err
	}
	defer pool.Close()

	var versionNum string
	if err := pool.QueryRow(ctx, "SHOW server_version_num").Scan(&versionNum); err != nil {
		return 0, fmt.Errorf("dump: query server_version_num: %w", err)
	}
	// server_version_num is e.g. "180004" for 18.4 — the major version is the leading digits
	// before the 4-digit minor/patch suffix (PG's own documented encoding).
	n, err := strconv.Atoi(versionNum)
	if err != nil {
		return 0, fmt.Errorf("dump: parse server_version_num %q: %w", versionNum, err)
	}
	return n / 10000, nil
}

// parsePgMajorVersion extracts the nth (1-indexed) integer token from a Postgres version/number
// string, e.g. parsePgMajorVersion("pg_dump (PostgreSQL) 18.4", 1) -> 18.
func parsePgMajorVersion(s string, n int) (int, error) {
	matches := pgVersionNumberRe.FindAllString(s, -1)
	if len(matches) < n {
		return 0, fmt.Errorf("dump: could not parse Postgres version from %q", strings.TrimSpace(s))
	}
	major, err := strconv.Atoi(matches[n-1])
	if err != nil {
		return 0, fmt.Errorf("dump: parse Postgres version %q: %w", matches[n-1], err)
	}
	return major, nil
}

// bagTableExcludePattern is pg_dump's --exclude-table pattern for the BAG tables: they live in
// `public` with a `bag_` prefix, not a dedicated schema (design D3, load/geo/schema.go), so this
// excludes exactly those tables and leaves the small gebieden_*/cbs_* geo-reference tables (same
// schema, same load) in the dump.
const bagTableExcludePattern = "public.bag_*"

// ExportPostGIS runs `pg_dump -Fc` against databaseURL, writing the custom-format archive to
// outPath. When skipBAG is set, the BAG tables are excluded (design D3) — the manifest records
// the choice, not this function.
func ExportPostGIS(ctx context.Context, databaseURL, outPath string, skipBAG bool) error {
	args := []string{"-Fc", "-f", outPath}
	if skipBAG {
		args = append(args, "--exclude-table="+bagTableExcludePattern)
	}
	args = append(args, "-d", databaseURL)

	out, err := exec.CommandContext(ctx, "pg_dump", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("dump: pg_dump failed: %w (output: %s)", err, out)
	}
	return nil
}

// RestorePostGIS runs `pg_restore --clean --if-exists` against databaseURL from the archive at
// inPath. Restore is additive (design D6): --clean --if-exists drops and recreates only the
// objects present in the archive, leaving tables absent from a BAG-less bundle untouched. The
// target must already have the postgis extension available.
func RestorePostGIS(ctx context.Context, databaseURL, inPath string) error {
	args := []string{"--clean", "--if-exists", "-d", databaseURL, inPath}

	out, err := exec.CommandContext(ctx, "pg_restore", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("dump: pg_restore failed: %w (output: %s)", err, out)
	}
	return nil
}
