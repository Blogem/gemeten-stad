package koop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Blogem/gemeten-stad/location"
)

// availableDateLayout is the layout dcterms:available is landed in ("YYYY-MM-DD").
const availableDateLayout = "2006-01-02"

// buurtCodeByIdentificatieSQL looks up a buurt's code (the Noord test) from its identificatie,
// the value the buurt tier of location.Resolve already gives us. $1 = identificatie.
const buurtCodeByIdentificatieSQL = `SELECT code FROM gebieden_buurten WHERE identificatie = $1`

// buurtByPointSQL places an EWKT point into a gebieden_buurten polygon, for the address tier
// (where location.Resolve gives us a resolved point but not a buurt). $1 = point EWKT.
const buurtByPointSQL = `SELECT identificatie, code
FROM gebieden_buurten
WHERE ST_Contains(geom, ST_GeomFromEWKT($1))
LIMIT 1`

// buurtByRDPointSQL places a raw RD point into a gebieden_buurten polygon, for the postcode tier
// (where location.Resolve gives no geometry at all — we fall back to the publication's own point).
// $1 = point X (RD), $2 = point Y (RD).
const buurtByRDPointSQL = `SELECT identificatie, code
FROM gebieden_buurten
WHERE ST_Contains(geom, ST_SetSRID(ST_MakePoint($1, $2), 28992))
LIMIT 1`

// resolveBesluit places a besluit publication via the P6 resolver, determines its containing
// gebieden buurt (identificatie + code), and scopes it to Noord. It returns
// Resolved{Unresolved: true} — not an error — for every case where the besluit cannot be placed:
// no resolver candidate, no point available at the postcode tier, or no buurt containing the
// resolved point. A non-nil error is reserved for genuine infrastructure failures (a DB error from
// the buurt lookups below).
func resolveBesluit(ctx context.Context, pool *pgxpool.Pool, pub Publication) (Resolved, error) {
	date := parseAvailable(pub.Available)

	result, err := location.Resolve(ctx, pool, location.Query{
		Street:     pub.Street,
		Huisnummer: pub.Huisnummer,
		Postcode:   pub.Postcode,
		Point:      pub.Point,
		Date:       date,
	})
	if err != nil {
		// Nothing resolved at all (no address, postcode, or buurt candidate) — the besluit
		// cannot be placed. This is a normal outcome, not a failure.
		return Resolved{Unresolved: true}, nil
	}

	identificatie, code, err := buurtFor(ctx, pool, result, pub.Point)
	if err != nil {
		return Resolved{}, fmt.Errorf("koop: resolve: %w", err)
	}
	if identificatie == "" {
		return Resolved{Unresolved: true}, nil
	}

	inNoord := strings.HasPrefix(code, "N")

	var geom string
	if result.PlaceLevel == location.PlaceAddress {
		geom = result.Geom
	}

	return Resolved{
		Identificatie: identificatie,
		BuurtCode:     code,
		Confidence:    result.Confidence,
		Caveats:       result.Caveats,
		InNoord:       inNoord,
		Unresolved:    false,
		Geom:          geom,
		Tier:          string(result.PlaceLevel),
	}, nil
}

// buurtFor determines the containing gebieden buurt (identificatie + code) from a resolver
// result, per place level:
//   - buurt tier: the resolver already found the buurt (result.BuurtID); look up its code.
//   - address tier: result.Geom is an EWKT point; point-in-polygon it into gebieden_buurten.
//   - postcode tier: result.Geom is "" (no single point at that tier); fall back to
//     point-in-polygon the publication's own RD point, if present.
//
// identificatie == "" (with a nil error) means the besluit cannot be placed: no point-in-polygon
// match, or (postcode tier) no RD point to fall back on.
func buurtFor(ctx context.Context, pool *pgxpool.Pool, result location.Result, point *location.RDPoint) (identificatie, code string, err error) {
	switch result.PlaceLevel {
	case location.PlaceBuurt:
		code, err := lookupCodeByIdentificatie(ctx, pool, result.BuurtID)
		if err != nil {
			return "", "", err
		}
		return result.BuurtID, code, nil

	case location.PlaceAddress:
		return lookupBuurtByEWKT(ctx, pool, result.Geom)

	case location.PlacePostcode:
		if point == nil {
			return "", "", nil
		}
		return lookupBuurtByRDPoint(ctx, pool, *point)

	default:
		return "", "", nil
	}
}

// lookupCodeByIdentificatie fetches a buurt's code from its identificatie. identificatie is
// always non-empty here (only called at the buurt tier, where location.Resolve guarantees
// result.BuurtID is set) — a no-rows result would mean the resolver's own buurt is missing from
// gebieden_buurten, an inconsistency worth surfacing as an error rather than silently swallowing.
func lookupCodeByIdentificatie(ctx context.Context, pool *pgxpool.Pool, identificatie string) (string, error) {
	var code string
	if err := pool.QueryRow(ctx, buurtCodeByIdentificatieSQL, identificatie).Scan(&code); err != nil {
		return "", fmt.Errorf("lookup buurt code for identificatie %q: %w", identificatie, err)
	}
	return code, nil
}

// lookupBuurtByEWKT point-in-polygons an EWKT point (the address-tier resolved geometry) into
// gebieden_buurten. found == false (identificatie == "") means the point falls outside every
// loaded buurt polygon — not an error, just unresolvable.
func lookupBuurtByEWKT(ctx context.Context, pool *pgxpool.Pool, ewkt string) (identificatie, code string, err error) {
	row := pool.QueryRow(ctx, buurtByPointSQL, ewkt)
	if err := row.Scan(&identificatie, &code); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("lookup buurt for point %s: %w", ewkt, err)
	}
	return identificatie, code, nil
}

// lookupBuurtByRDPoint point-in-polygons a raw RD point (the publication's own geometry, used as
// the postcode-tier fallback) into gebieden_buurten. found == false means the point falls outside
// every loaded buurt polygon.
func lookupBuurtByRDPoint(ctx context.Context, pool *pgxpool.Pool, point location.RDPoint) (identificatie, code string, err error) {
	row := pool.QueryRow(ctx, buurtByRDPointSQL, point.X, point.Y)
	if err := row.Scan(&identificatie, &code); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("lookup buurt for RD point (%v, %v): %w", point.X, point.Y, err)
	}
	return identificatie, code, nil
}

// parseAvailable parses dcterms:available ("YYYY-MM-DD") into the valid-time date the resolver
// ladders against. An empty or malformed value (parse defensively: never fail the resolve over a
// landing quirk) yields the zero time.Time.
func parseAvailable(available string) time.Time {
	if available == "" {
		return time.Time{}
	}
	date, err := time.Parse(availableDateLayout, available)
	if err != nil {
		return time.Time{}
	}
	return date
}
