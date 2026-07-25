// Package location holds the preloaded BAG + gebieden + CBS geometry and the smallest-area location resolver.
//
// Resolve ladders a query (street/huisnummer/postcode/point, at a valid-time date) down to the
// smallest place it can confidently establish — address (0.90) → postcode (0.70) → buurt (0.50) —
// entirely against the local PostGIS mirror (no PDOK Locatieserver). See
// openspec/changes/geo-backbone-preload/design.md D4/D5 and
// openspec/changes/geo-backbone-preload/specs/location-resolver/spec.md for the full contract;
// this package ports spikes/spike-d/sql/resolve.sql and pip.sql.
package location
