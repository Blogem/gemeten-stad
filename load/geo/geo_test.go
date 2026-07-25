package geo

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -- test helpers -----------------------------------------------------------

// valueAfterFlag returns the arg immediately following the first occurrence of
// flag in args, mirroring how ogr2ogr consumes "-flag value" pairs.
func valueAfterFlag(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// valuesAfterFlag returns every value immediately following an occurrence of
// flag (ogr2ogr allows repeated -lco flags).
func valuesAfterFlag(args []string, flag string) []string {
	var out []string
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

func joined(args []string) string {
	return strings.Join(args, " ")
}

// -- 7.1 StagingTable ---------------------------------------------------------

func TestStagingTable(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{"OPR", "bag_openbareruimte_staging"},
		{"NUM", "bag_nummeraanduiding_staging"},
		{"VBO", "bag_verblijfsobject_staging"},
		{"LIG", "bag_ligplaats_staging"},
		{"STA", "bag_standplaats_staging"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			assert.Equal(t, tt.want, StagingTable(tt.code))
		})
	}
}

// -- 7.1 BAGStagingArgs -------------------------------------------------------

func TestBAGStagingArgs_CommonFlags(t *testing.T) {
	const (
		innerZip = "/vsizip//data/bag/9999NUM20260101.zip"
		pgConn   = "PG:host=db dbname=geo user=geo password=geo"
	)

	tests := []string{"OPR", "NUM", "VBO", "LIG", "STA"}

	for _, code := range tests {
		t.Run(code, func(t *testing.T) {
			args := BAGStagingArgs(code, innerZip, pgConn)
			require.NotEmpty(t, args)

			// -f PostgreSQL
			f, ok := valueAfterFlag(args, "-f")
			require.True(t, ok, "expected -f flag in %v", args)
			assert.Equal(t, "PostgreSQL", f)

			// the pg connection string is present verbatim somewhere
			assert.Contains(t, args, pgConn)

			// AUTOCORRECT_INVALID_DATA=YES, as an -oo open option
			assert.Contains(t, valuesAfterFlag(args, "-oo"), "AUTOCORRECT_INVALID_DATA=YES",
				"args = %v", args)

			// -where "identificatie LIKE '%.0363%'" — the municipality filter, exact value
			where, ok := valueAfterFlag(args, "-where")
			require.True(t, ok, "expected -where flag in %v", args)
			assert.Equal(t, "identificatie LIKE '%.0363%'", where)

			// -lco GEOMETRY_NAME=geom
			assert.Contains(t, valuesAfterFlag(args, "-lco"), "GEOMETRY_NAME=geom",
				"args = %v", args)

			// -nln <staging table for this code>
			nln, ok := valueAfterFlag(args, "-nln")
			require.True(t, ok, "expected -nln flag in %v", args)
			assert.Equal(t, StagingTable(code), nln)
		})
	}
}

func TestBAGStagingArgs_PromoteToMultiPerObjectType(t *testing.T) {
	const (
		innerZip = "/vsizip//data/bag/9999LIG20260101.zip"
		pgConn   = "PG:host=db dbname=geo user=geo password=geo"
	)

	tests := []struct {
		code            string
		wantPromoteFlag bool
	}{
		{"OPR", false},
		{"NUM", false},
		{"VBO", false},
		{"LIG", true},
		{"STA", true},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			args := BAGStagingArgs(tt.code, innerZip, pgConn)
			hasPromote := strings.Contains(joined(args), "PROMOTE_TO_MULTI")
			assert.Equal(t, tt.wantPromoteFlag, hasPromote,
				"code=%s args=%v", tt.code, args)
		})
	}
}

// -- 7.1 PolygonStagingArgs ----------------------------------------------------

func TestPolygonStagingArgs(t *testing.T) {
	const (
		landed       = "/data/gebieden_buurten.geojson"
		pgConn       = "PG:host=db dbname=geo user=geo password=geo"
		stagingTable = "gebieden_buurten_staging"
	)

	t.Run("promote=true", func(t *testing.T) {
		args := PolygonStagingArgs(landed, pgConn, stagingTable, true)
		require.NotEmpty(t, args)

		assert.Contains(t, joined(args), "PROMOTE_TO_MULTI")

		srs, ok := valueAfterFlag(args, "-a_srs")
		require.True(t, ok, "expected -a_srs flag in %v", args)
		assert.Equal(t, "EPSG:28992", srs)

		assert.Contains(t, valuesAfterFlag(args, "-lco"), "GEOMETRY_NAME=geom",
			"args = %v", args)

		nln, ok := valueAfterFlag(args, "-nln")
		require.True(t, ok, "expected -nln flag in %v", args)
		assert.Equal(t, stagingTable, nln)
	})

	t.Run("promote=false", func(t *testing.T) {
		args := PolygonStagingArgs(landed, pgConn, stagingTable, false)
		require.NotEmpty(t, args)

		assert.NotContains(t, joined(args), "PROMOTE_TO_MULTI")

		srs, ok := valueAfterFlag(args, "-a_srs")
		require.True(t, ok, "expected -a_srs flag in %v", args)
		assert.Equal(t, "EPSG:28992", srs)
	})
}
