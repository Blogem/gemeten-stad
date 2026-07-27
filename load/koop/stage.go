package koop

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Blogem/gemeten-stad/location"
)

// PublicationRow is one publication to persist. Res is non-nil only for the audited besluit in a
// trail — every other publication the trail assembles (aanvraag, ontwerpbesluit, verlenging,
// ingetrokken, other besluiten) carries no resolution and is staged with Res == nil.
type PublicationRow struct {
	Pub Publication
	Res *Resolved
}

// rawPublication is the shape marshaled into koop_publications.raw. It snapshots Publication's own
// fields (title/activiteit/kind/point/etc. and the two raw landed payloads as strings) but
// deliberately excludes Resolved — the resolved_* columns are their own change-detection inputs
// (see upsert.go), so a publication whose content is unchanged but whose resolution was recomputed
// still triggers a MERGE update via those columns, without raw needing to carry them too.
type rawPublication struct {
	ID          string            `json:"id"`
	Zaaknummer  string            `json:"zaaknummer,omitempty"`
	Kind        string            `json:"kind"`
	Activiteit  string            `json:"activiteit,omitempty"`
	Point       *location.RDPoint `json:"point,omitempty"`
	Postcode    string            `json:"postcode,omitempty"`
	Huisnummer  int               `json:"huisnummer,omitempty"`
	Street      string            `json:"street,omitempty"`
	Title       string            `json:"title"`
	Available   string            `json:"available,omitempty"`
	RawRecord   string            `json:"rawRecord,omitempty"`
	RawMetadata string            `json:"rawMetadata,omitempty"`
}

// marshalRaw builds p's raw jsonb snapshot (see rawPublication).
func marshalRaw(p Publication) ([]byte, error) {
	r := rawPublication{
		ID:          p.ID,
		Zaaknummer:  p.Zaaknummer,
		Kind:        string(p.Kind),
		Activiteit:  p.Activiteit,
		Point:       p.Point,
		Postcode:    p.Postcode,
		Huisnummer:  p.Huisnummer,
		Street:      p.Street,
		Title:       p.Title,
		Available:   p.Available,
		RawRecord:   string(p.RawRecord),
		RawMetadata: string(p.RawMetadata),
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("marshal raw publication %s: %w", p.ID, err)
	}
	return b, nil
}

// publicationInsertSQL is stagePublications' per-row INSERT, parameterized in the same order as
// publicationColumns (see upsert.go) except that geom consumes two params ($5, $6 — x, y) rather
// than one: ST_MakePoint is STRICT, so a nil Point (both args NULL) yields a NULL geom, exactly
// like every other absent field below. resolved_geom is bound as EWKT text through
// ST_GeomFromEWKT ($12), also STRICT, so a NULL bind (Res == nil, unresolved, or Geom == "" at the
// postcode/buurt tier) likewise yields a NULL geom.
const publicationInsertSQL = `
INSERT INTO %s (
    gmb_id, zaaknummer, kind, available, geom, postcode, huisnummer,
    resolved_identificatie, resolved_buurt_code, resolved_confidence, resolved_geom, resolved_tier,
    caveats, in_noord, unresolved, raw
) VALUES (
    $1, $2, $3, $4, ST_SetSRID(ST_MakePoint($5, $6), 28992), $7, $8,
    $9, $10, $11, ST_GeomFromEWKT($12), $13,
    $14, $15, $16, $17
)`

// stagePublications loads rows into koop_publications_staging, replacing whatever was staged
// there before: the staging table always reflects exactly the latest assembled batch, so
// upsertPublications can diff against it.
//
// Each row is inserted independently (no transaction), so a single unstageable or rejected row is
// skipped with a slog.Warn rather than aborting the whole batch — only the initial TRUNCATE
// failure is batch-level and returned as an error.
func stagePublications(ctx context.Context, pool *pgxpool.Pool, schema string, rows []PublicationRow) error {
	stagingTable := qualify(schema, publicationsStagingTable)

	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", stagingTable)
	if _, err := pool.Exec(ctx, truncateSQL); err != nil {
		return fmt.Errorf("koop: truncate %s: %w", stagingTable, err)
	}

	stmt := fmt.Sprintf(publicationInsertSQL, stagingTable)
	var staged, skipped int
	for _, row := range rows {
		args, err := publicationArgs(row)
		if err != nil {
			slog.Warn("koop: skipping unstageable row", "id", row.Pub.ID, "err", err)
			skipped++
			continue
		}
		if _, err := pool.Exec(ctx, stmt, args...); err != nil {
			slog.Warn("koop: skipping unstageable row", "id", row.Pub.ID, "err", err)
			skipped++
			continue
		}
		staged++
	}
	slog.Info("koop: staged publications", "staged", staged, "skipped", skipped)
	return nil
}

// publicationArgs builds the 17 bind values for publicationInsertSQL from row: gmb_id, zaaknummer,
// kind, available, geom's x/y, postcode, huisnummer, the six resolved_* fields, caveats, in_noord,
// unresolved, and raw. A zero-value Go field (Huisnummer == 0, Point == nil, Available == "",
// Res == nil) is bound as SQL NULL, matching Publication/Resolved's own "zero means absent" docs
// (types.go) — except unresolved, which is NOT NULL and defaults to false when Res == nil (a
// non-besluit trail row is not "unresolved", it simply was never subject to resolution).
//
// resolved_identificatie, resolved_buurt_code, resolved_confidence, resolved_geom, resolved_tier,
// caveats, and in_noord are only populated when row.Res carries a real resolution (Res != nil &&
// !Res.Unresolved). A keyless publication or an unresolvable besluit (Res != nil &&
// Res.Unresolved) is not a placement — its resolution columns must stay NULL rather than fabricate
// empty-string/zero values, so callers can tell "resolution attempted but failed"
// (unresolved=true, columns NULL) apart from "resolved" (unresolved=false, columns populated).
// resolved_geom is further NULL whenever Res.Geom == "" (postcode/buurt tier — see resolve.go),
// even though the resolution itself succeeded: only the address tier has a single precise point.
func publicationArgs(row PublicationRow) ([]any, error) {
	p := row.Pub

	raw, err := marshalRaw(p)
	if err != nil {
		return nil, err
	}

	var pointX, pointY any
	if p.Point != nil {
		pointX, pointY = p.Point.X, p.Point.Y
	}

	var available any
	if p.Available != "" {
		t, err := time.Parse("2006-01-02", p.Available)
		if err != nil {
			return nil, fmt.Errorf("unparsable available date %q: %w", p.Available, err)
		}
		available = t
	}

	var zaaknummer any
	if p.Zaaknummer != "" {
		zaaknummer = p.Zaaknummer
	}
	var postcode any
	if p.Postcode != "" {
		postcode = p.Postcode
	}
	var huisnummer any
	if p.Huisnummer != 0 {
		huisnummer = p.Huisnummer
	}

	var identificatie, buurtCode, confidence, resolvedGeom, resolvedTier, caveats, inNoord any
	unresolved := false
	if row.Res != nil {
		unresolved = row.Res.Unresolved
		if !row.Res.Unresolved {
			identificatie = row.Res.Identificatie
			buurtCode = row.Res.BuurtCode
			confidence = row.Res.Confidence
			if row.Res.Geom != "" {
				resolvedGeom = row.Res.Geom
			}
			resolvedTier = row.Res.Tier
			if len(row.Res.Caveats) > 0 {
				caveats = row.Res.Caveats
			}
			inNoord = row.Res.InNoord
		}
	}

	return []any{
		p.ID,
		zaaknummer,
		string(p.Kind),
		available,
		pointX, pointY,
		postcode,
		huisnummer,
		identificatie,
		buurtCode,
		confidence,
		resolvedGeom,
		resolvedTier,
		caveats,
		inNoord,
		unresolved,
		raw,
	}, nil
}
