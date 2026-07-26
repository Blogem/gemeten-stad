package places

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// qualify renders table as an explicitly schema-qualified, properly quoted identifier
// (schema.table), mirroring load/geo/schema.go's qualify — every read this package issues names
// its target this way rather than relying on the connection's search_path.
func qualify(schema, table string) string {
	return pgx.Identifier{schema, table}.Sanitize()
}

// readBuurten reads every gebieden_buurten row (no source_deleted_at filter — live and
// soft-deleted rows are both projected, per design.md's whole-city, non-destructive scope).
func readBuurten(ctx context.Context, pool *pgxpool.Pool, schema string) ([]buurtRow, error) {
	stmt := fmt.Sprintf(
		"SELECT identificatie, COALESCE(naam, ''), ligtinwijkid, source_deleted_at FROM %s",
		qualify(schema, "gebieden_buurten"),
	)
	rows, err := pool.Query(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("places: read gebieden_buurten: %w", err)
	}
	defer rows.Close()

	var buurten []buurtRow
	for rows.Next() {
		var r buurtRow
		if err := rows.Scan(&r.identificatie, &r.naam, &r.ligtInWijkID, &r.sourceDeletedAt); err != nil {
			return nil, fmt.Errorf("places: scan gebieden_buurten row: %w", err)
		}
		buurten = append(buurten, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("places: read gebieden_buurten: %w", err)
	}
	return buurten, nil
}

// readWijken reads every gebieden_wijken row (no source_deleted_at filter).
func readWijken(ctx context.Context, pool *pgxpool.Pool, schema string) ([]wijkRow, error) {
	stmt := fmt.Sprintf(
		"SELECT identificatie, COALESCE(naam, ''), source_deleted_at FROM %s",
		qualify(schema, "gebieden_wijken"),
	)
	rows, err := pool.Query(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("places: read gebieden_wijken: %w", err)
	}
	defer rows.Close()

	var wijken []wijkRow
	for rows.Next() {
		var r wijkRow
		if err := rows.Scan(&r.identificatie, &r.naam, &r.sourceDeletedAt); err != nil {
			return nil, fmt.Errorf("places: scan gebieden_wijken row: %w", err)
		}
		wijken = append(wijken, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("places: read gebieden_wijken: %w", err)
	}
	return wijken, nil
}

// BuildCandidate reads the gebieden_buurten and gebieden_wijken tables (schema-qualified, all
// rows) and renders the Place skeleton Turtle candidate with the current time as the load
// timestamp, logging a loud warning for every buurt seeded without a resolvable gs:within edge
// (design.md D5, spec.md "Seed the buurt->wijk containment via gs:within").
func BuildCandidate(ctx context.Context, pool *pgxpool.Pool, schema string) ([]byte, error) {
	buurten, err := readBuurten(ctx, pool, schema)
	if err != nil {
		return nil, err
	}
	wijken, err := readWijken(ctx, pool, schema)
	if err != nil {
		return nil, err
	}

	// Validate every identificatie before it ever reaches the pure render path: BuildCandidate is
	// this package's single production entry point, so this guarantees renderPlaces/placeToken/
	// mintPlaceIRI only ever see already-safe input and can stay panic-free (places.go).
	for _, b := range buurten {
		if err := assertSafeIdentificatie(b.identificatie); err != nil {
			return nil, fmt.Errorf("places: unsafe buurt identificatie %q: %w", b.identificatie, err)
		}
	}
	for _, w := range wijken {
		if err := assertSafeIdentificatie(w.identificatie); err != nil {
			return nil, fmt.Errorf("places: unsafe wijk identificatie %q: %w", w.identificatie, err)
		}
	}

	turtle, dangling := renderPlaces(buurten, wijken, time.Now())

	if len(dangling) > 0 {
		naamByID := make(map[string]string, len(buurten))
		for _, b := range buurten {
			naamByID[b.identificatie] = b.naam
		}
		for _, id := range dangling {
			log.Printf("places: buurt %s (%q) has no resolvable wijk; seeded without gs:within", id, naamByID[id])
		}
	}

	return turtle, nil
}
