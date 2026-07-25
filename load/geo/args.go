package geo

// bagTables maps a BAG object-type code to the target table it eventually upserts into. The
// *_staging suffix (see StagingTable) is where ogr2ogr lands the raw lvbag rows before the pgx
// upsert reconciles them into these target tables.
var bagTables = map[string]string{
	"OPR": "bag_openbareruimte",
	"NUM": "bag_nummeraanduiding",
	"VBO": "bag_verblijfsobject",
	"LIG": "bag_ligplaats",
	"STA": "bag_standplaats",
}

// bagPromoteToMulti is the set of BAG object types whose geometry column must be widened to a
// MULTI* type (mixed Polygon/MultiPolygon in the source, per Spike D).
var bagPromoteToMulti = map[string]bool{
	"LIG": true,
	"STA": true,
}

// StagingTable maps a BAG object-type code (OPR/NUM/VBO/LIG/STA) to its *_staging table name.
// Returns "" for an unrecognized code.
func StagingTable(code string) string {
	table, ok := bagTables[code]
	if !ok {
		return ""
	}
	return table + "_staging"
}

// BAGStagingArgs builds the ogr2ogr args (WITHOUT a leading "ogr2ogr"; the sidecar adds it) to
// stage one BAG object type from its inner per-object-type zip into `<table>_staging`. Flags are
// ported verbatim from spikes/spike-d/load.sh: AUTOCORRECT_INVALID_DATA=YES (BAG has known invalid
// geometries), the '%.0363%' municipality filter (identificatie is the IMBAG URI
// NL.IMBAG.<Type>.<gemeente...>), GEOMETRY_NAME=geom, and PROMOTE_TO_MULTI for LIG/STA (mixed
// Polygon/MultiPolygon geometry). No -overwrite/-append: the caller drops the staging table first
// so ogr2ogr always creates it fresh (see load/geo's orchestrator). Returns nil for an unrecognized
// code.
func BAGStagingArgs(code, innerZipVsiPath, pgConn string) []string {
	table := StagingTable(code)
	if table == "" {
		return nil
	}

	args := []string{
		"-f", "PostgreSQL", pgConn,
		"-oo", "AUTOCORRECT_INVALID_DATA=YES",
		innerZipVsiPath,
		"-where", "identificatie LIKE '%.0363%'",
		"-nln", table,
		"-lco", "GEOMETRY_NAME=geom",
	}
	if bagPromoteToMulti[code] {
		args = append(args, "-nlt", "PROMOTE_TO_MULTI")
	}
	return args
}

// PolygonStagingArgs builds the ogr2ogr args (WITHOUT a leading "ogr2ogr") to stage a landed
// GeoJSON/CBS polygon file into stagingTable. All polygon sources are requested/loaded in RD, so
// -a_srs EPSG:28992 stamps the SRID explicitly (the source files carry no CRS metadata of their
// own); promote adds -nlt PROMOTE_TO_MULTI for sources with mixed Polygon/MultiPolygon geometry.
// No -overwrite/-append: the caller drops the staging table first.
func PolygonStagingArgs(landedFilePath, pgConn, stagingTable string, promote bool) []string {
	args := []string{
		"-f", "PostgreSQL", pgConn,
		landedFilePath,
		"-a_srs", "EPSG:28992",
		"-nln", stagingTable,
		"-lco", "GEOMETRY_NAME=geom",
	}
	if promote {
		args = append(args, "-nlt", "PROMOTE_TO_MULTI")
	}
	return args
}
