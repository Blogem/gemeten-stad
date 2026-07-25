package shared

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGDALPrefix covers acceptance scenario 5: the GDAL sidecar exec prefix
// falls back to the docker-compose default when GS_GDAL_EXEC_PREFIX is
// unset, and is split on whitespace from the env var when set.
func TestGDALPrefix(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{
			name: "default when unset",
			env:  map[string]string{},
			want: []string{"docker", "compose", "-f", "deploy/compose/compose.yaml", "exec", "-T", "gdal"},
		},
		{
			name: "default when empty",
			env:  map[string]string{"GS_GDAL_EXEC_PREFIX": ""},
			want: []string{"docker", "compose", "-f", "deploy/compose/compose.yaml", "exec", "-T", "gdal"},
		},
		{
			name: "custom override split on whitespace",
			env:  map[string]string{"GS_GDAL_EXEC_PREFIX": "podman exec gdal-sidecar"},
			want: []string{"podman", "exec", "gdal-sidecar"},
		},
		{
			name: "custom override with extra whitespace",
			env:  map[string]string{"GS_GDAL_EXEC_PREFIX": "  kubectl  exec  -it  gdal-pod  -- "},
			want: []string{"kubectl", "exec", "-it", "gdal-pod", "--"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := func(k string) string { return tt.env[k] }
			got := GDALPrefix(env)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestSidecarCommand covers acceptance scenario 6: SidecarCommand is a pure
// function that assembles <prefix...> ogr2ogr <args...> with no other
// mutation of the inputs.
func TestSidecarCommand(t *testing.T) {
	tests := []struct {
		name   string
		prefix []string
		args   []string
		want   []string
	}{
		{
			name:   "docker compose gdal prefix with ogr2ogr load args",
			prefix: []string{"docker", "compose", "-f", "deploy/compose/compose.yaml", "exec", "-T", "gdal"},
			args:   []string{"-f", "PostgreSQL", "PG:dbname=gemeten_stad", "/data/bomen-noord.gml"},
			want: []string{
				"docker", "compose", "-f", "deploy/compose/compose.yaml", "exec", "-T", "gdal",
				"ogr2ogr", "-f", "PostgreSQL", "PG:dbname=gemeten_stad", "/data/bomen-noord.gml",
			},
		},
		{
			name:   "empty prefix",
			prefix: []string{},
			args:   []string{"-f", "GeoJSON", "out.json", "in.gml"},
			want:   []string{"ogr2ogr", "-f", "GeoJSON", "out.json", "in.gml"},
		},
		{
			name:   "empty args",
			prefix: []string{"docker", "exec", "gdal"},
			args:   []string{},
			want:   []string{"docker", "exec", "gdal", "ogr2ogr"},
		},
		{
			name:   "nil prefix and nil args",
			prefix: nil,
			args:   nil,
			want:   []string{"ogr2ogr"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SidecarCommand(tt.prefix, tt.args)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestSidecarCommand_DoesNotMutateInputs guards against an implementation
// that appends into the caller's prefix slice in place (which could corrupt
// a shared prefix across concurrent calls, e.g. building commands for
// multiple layers from the same base prefix).
func TestSidecarCommand_DoesNotMutateInputs(t *testing.T) {
	prefix := make([]string, 3, 3) // zero spare capacity: append must copy
	copy(prefix, []string{"docker", "exec", "gdal"})
	args := []string{"-f", "GeoJSON"}

	prefixBefore := append([]string(nil), prefix...)
	argsBefore := append([]string(nil), args...)

	_ = SidecarCommand(prefix, args)

	assert.Equal(t, prefixBefore, prefix, "SidecarCommand must not mutate the caller's prefix slice")
	assert.Equal(t, argsBefore, args, "SidecarCommand must not mutate the caller's args slice")
}
