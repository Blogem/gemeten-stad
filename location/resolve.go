package location

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Resolve ladders q down to the smallest place it can confidently establish — address (0.90) →
// postcode (0.70) → buurt (0.50) — entirely against the local PostGIS mirror (pool). It never
// calls out to PDOK Locatieserver, and it never snaps a query's own point to a nearer
// address/postcode: buurt is the floor.
//
// Match preference is address > postcode > buurt; within address, (postcode, huisnummer) > exact
// street > fuzzy street. Within the address tier, the best-known voorkomen valid at q.Date is
// preferred; if none is valid at that date, any best-known voorkomen is used and the result
// carries the "timeMismatch" caveat (see TimeMatchFor).
//
// Resolve returns an error only when nothing resolves (no address, postcode, or buurt candidate);
// a resolved-but-flagged result (any_time, unresolvedLocation) is not an error.
func Resolve(ctx context.Context, pool *pgxpool.Pool, q Query) (Result, error) {
	var (
		hasAddress, hasPostcode, hasBuurt bool
		geom, buurtID                     string
		validAtDate                       bool
	)

	if q.Postcode != "" && q.Huisnummer != 0 {
		g, v, found, err := queryAddressCandidate(ctx, pool, addressByPostcodeHuisnummerSQL, q.Postcode, q.Huisnummer, q)
		if err != nil {
			return Result{}, err
		}
		hasAddress, geom, validAtDate = found, g, v
	}

	if !hasAddress && q.Street != "" && q.Huisnummer != 0 {
		g, v, found, err := queryAddressCandidate(ctx, pool, addressByStreetExactSQL, q.Street, q.Huisnummer, q)
		if err != nil {
			return Result{}, err
		}
		hasAddress, geom, validAtDate = found, g, v
	}

	if !hasAddress && q.Street != "" && q.Huisnummer != 0 {
		g, v, found, err := queryAddressCandidate(ctx, pool, addressByStreetFuzzySQL, q.Street, q.Huisnummer, q)
		if err != nil {
			return Result{}, err
		}
		hasAddress, geom, validAtDate = found, g, v
	}

	if !hasAddress && q.Postcode != "" {
		found, err := queryPostcodeExists(ctx, pool, q.Postcode)
		if err != nil {
			return Result{}, err
		}
		hasPostcode = found
	}

	if !hasAddress && !hasPostcode && q.Point != nil {
		id, g, found, err := queryBuurtPIP(ctx, pool, *q.Point)
		if err != nil {
			return Result{}, err
		}
		hasBuurt, buurtID, geom = found, id, g
	}

	level, confidence, ok := PlaceLevelFor(hasAddress, hasPostcode, hasBuurt)
	if !ok {
		return Result{}, fmt.Errorf("location: resolve: no address, postcode, or buurt candidate for query")
	}

	result := Result{
		PlaceLevel: level,
		Geom:       geom,
		Confidence: confidence,
		BuurtID:    buurtID,
	}

	if hasAddress {
		result.TimeMatch = TimeMatchFor(validAtDate)
		if result.TimeMatch == timeMatchAnyTime {
			result.Caveats = append(result.Caveats, caveatTimeMismatch)
		}
	}

	if hasBuurt {
		result.Caveats = append(result.Caveats, caveatUnresolvedLocation)
	}

	return result, nil
}

// queryAddressCandidate runs one address-tier candidate query (any of
// addressByPostcodeHuisnummerSQL / addressByStreetExactSQL / addressByStreetFuzzySQL, which all
// share the same $1 = match value, $2 = huisnummer, $3 = valid-time date, and (geom, valid_at_date)
// result shape) and reports whether a row was found.
func queryAddressCandidate(ctx context.Context, pool *pgxpool.Pool, query, matchValue string, huisnummer int, q Query) (geom string, validAtDate, found bool, err error) {
	row := pool.QueryRow(ctx, query, matchValue, huisnummer, q.Date)
	if err := row.Scan(&geom, &validAtDate); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, false, nil
		}
		return "", false, false, fmt.Errorf("location: query address candidate: %w", err)
	}
	return geom, validAtDate, true, nil
}

// queryPostcodeExists checks the postcode tier: postcode exists in bag_nummeraanduiding under any
// best-known voorkomen.
func queryPostcodeExists(ctx context.Context, pool *pgxpool.Pool, postcode string) (bool, error) {
	var exists bool
	if err := pool.QueryRow(ctx, postcodeExistsSQL, postcode).Scan(&exists); err != nil {
		return false, fmt.Errorf("location: query postcode exists: %w", err)
	}
	return exists, nil
}

// queryBuurtPIP places point into a gebieden_buurten polygon via ST_Contains and reports the
// buurt's identificatie (gbdBuurtId) and its geometry.
func queryBuurtPIP(ctx context.Context, pool *pgxpool.Pool, point RDPoint) (buurtID, geom string, found bool, err error) {
	row := pool.QueryRow(ctx, buurtPIPSQL, point.X, point.Y)
	if err := row.Scan(&buurtID, &geom); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("location: query buurt point-in-polygon: %w", err)
	}
	return buurtID, geom, true, nil
}
