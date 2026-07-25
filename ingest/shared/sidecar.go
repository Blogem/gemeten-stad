package shared

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// GDALPrefix resolves the sidecar command prefix from GS_GDAL_EXEC_PREFIX, splitting on
// whitespace. Default when unset: docker compose -f deploy/compose/compose.yaml exec -T gdal
func GDALPrefix(env func(string) string) []string {
	prefix := env("GS_GDAL_EXEC_PREFIX")
	if prefix == "" {
		return []string{"docker", "compose", "-f", "deploy/compose/compose.yaml", "exec", "-T", "gdal"}
	}
	return strings.Fields(prefix)
}

// Sidecar runs ogr2ogr inside the GDAL sidecar via <Prefix> ogr2ogr <args...>.
type Sidecar struct {
	Prefix []string
}

// NewSidecar returns a Sidecar that runs ogr2ogr with the given command prefix.
func NewSidecar(prefix []string) *Sidecar {
	return &Sidecar{Prefix: prefix}
}

// SidecarCommand builds the full argv (<prefix> ogr2ogr <args...>) — a PURE function, unit-tested
// for command assembly without executing anything.
func SidecarCommand(prefix []string, args []string) []string {
	cmd := make([]string, 0, len(prefix)+1+len(args))
	cmd = append(cmd, prefix...)
	cmd = append(cmd, "ogr2ogr")
	cmd = append(cmd, args...)
	return cmd
}

// redactArgs returns a copy of args with any element containing "password="
// masked from that point to the end of the token, preserving the prefix
// before "password=". It never mutates the input slice. Used for error/log
// output only — the real, unredacted args must still be passed to exec.
func redactArgs(args []string) []string {
	redacted := make([]string, len(args))
	for i, arg := range args {
		if idx := strings.Index(arg, "password="); idx != -1 {
			redacted[i] = arg[:idx] + "password=***REDACTED***"
			continue
		}
		redacted[i] = arg
	}
	return redacted
}

// OGR2OGR executes SidecarCommand(s.Prefix, args) via os/exec and returns combined output.
func (s *Sidecar) OGR2OGR(ctx context.Context, args ...string) ([]byte, error) {
	cmd := SidecarCommand(s.Prefix, args)

	out, err := exec.CommandContext(ctx, cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		tail := out
		const maxTail = 2000
		if len(tail) > maxTail {
			tail = tail[len(tail)-maxTail:]
		}
		return out, fmt.Errorf("shared: ogr2ogr %v failed: %w (output: %s)", redactArgs(args), err, tail)
	}

	return out, nil
}
