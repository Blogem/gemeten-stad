// Package geo orchestrates the geo backbone load into PostGIS: BAG + gebieden + CBS are staged
// via the GDAL sidecar (ogr2ogr) into *_staging tables, then upserted into their target tables
// (soft-deleting rows the fresh staging snapshot no longer carries), indexed for the location
// resolver, and sanity-gated (SRID, Noord ground truth). See
// openspec/changes/geo-backbone-preload/design.md D1/D2/D4 for the shape decisions.
package geo
