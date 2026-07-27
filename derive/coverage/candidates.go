package coverage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// interventionPrefix is the Intervention IRI mint pattern from load/koop/graph.go
// (dataNS + "intervention/" + zaaknummer). The derive step enumerates Interventions from the graph
// (D10) and strips this prefix to recover the zaaknummer used to join PostGIS.
const interventionPrefix = "http://gemetenstad.nl/id/intervention/"

// CandidateFelling is one kapenherplant row in a permit's buurt+time window (D6(1)): the individual
// matching unit — never grouped by the batch-assigned datumVergunningVerleend.
type CandidateFelling struct {
	ID          string
	BoomID      string
	FellingDate time.Time // kapmaatregelDatumUitgevoerd
	DistanceM   *float64  // nil when the permit has no resolved point
}

// Permit is a besluit Intervention enumerated from the graph, joined against koop_publications for
// its resolved place, plus its candidate fellings (D6(1)).
type Permit struct {
	Zaaknummer      string
	InterventionIRI string    // data:intervention/<zaaknummer>
	PublicationDate time.Time // from dct:available in the graph (D10)
	GbdBuurtID      string    // koop_publications.resolved_identificatie == kapenherplant.gbdBuurtId (the join key: 14-digit GBD buurt identificatie)
	BuurtCode       string    // koop_publications.resolved_buurt_code — the short, human-readable Amsterdam buurtcode (e.g. "NC02"); NOT the join key, used only for gs:evidence text
	Tier            Tier      // resolved_tier
	HasPoint        bool      // resolved_geom present
	Candidates      []CandidateFelling
}

// enumerateInterventionsQuery reads every besluit Intervention and its publication date from the
// graph (D10): the graph is the source of truth for which permits exist and when they were
// published; PostGIS is joined afterward only for the resolved place values.
const enumerateInterventionsQuery = `PREFIX gs:  <http://gemetenstad.nl/ns#>
PREFIX dct: <http://purl.org/dc/terms/>
SELECT ?intervention ?date WHERE { GRAPH ?g { ?intervention a gs:Intervention ; dct:available ?date . } }`

// GenerateCandidates implements D6(1) (tasks 2.1-2.3, 6.3): enumerate besluit Interventions from the
// graph, join koop_publications by zaaknummer for the resolved buurt identificatie / point / tier, and
// generate buurt+time-scoped candidate fellings from kapenherplant per permit, joining on the GBD
// buurt identificatie (the same 14-digit identifier kapenherplant."gbdBuurtId" carries) — never on
// the short, human-readable buurtcode, which is a different Amsterdam code system with no overlap.
// Assignment (D6(2)) and scoring (D5) are downstream of this — GenerateCandidates only produces the
// candidate set.
func GenerateCandidates(ctx context.Context, pool *pgxpool.Pool, schema, fusekiURL string) ([]Permit, error) {
	permits, err := enumeratePermits(ctx, fusekiURL)
	if err != nil {
		return nil, err
	}

	out := make([]Permit, 0, len(permits))
	for _, p := range permits {
		gbdBuurtID, buurtCode, tier, geomWKB, found, err := resolvePublication(ctx, pool, schema, p.Zaaknummer)
		if err != nil {
			return nil, err
		}
		if !found {
			// No audited-besluit row (or no resolved buurt identificatie) in koop_publications for
			// this zaaknummer. Kept in the output with zero candidates rather than dropped: D3
			// requires every enumerated permit to end up with a coverage outcome (matched or
			// no-source), and an unresolved permit is a genuine no-source case, not an omission.
			log.Printf("coverage: permit %s has no resolved buurt identificatie in koop_publications; recording with no candidates", p.Zaaknummer)
			out = append(out, p)
			continue
		}

		p.GbdBuurtID = gbdBuurtID
		p.BuurtCode = buurtCode
		p.Tier = tier
		p.HasPoint = len(geomWKB) > 0

		candidates, err := candidateFellings(ctx, pool, schema, gbdBuurtID, geomWKB, p.PublicationDate)
		if err != nil {
			return nil, err
		}
		p.Candidates = candidates

		out = append(out, p)
	}
	return out, nil
}

// enumeratePermits runs enumerateInterventionsQuery against the graph, dedups by Intervention IRI,
// and derives each Permit's Zaaknummer + PublicationDate. Task 6.3: an Intervention IRI whose
// zaaknummer strips to empty (P13's keyless-besluit case — expected never to occur per D10, since
// P13 emits no Intervention for a keyless/unresolved publication) is skipped and counted rather than
// erroring; the count is logged so an operator can confirm it is zero.
func enumeratePermits(ctx context.Context, fusekiURL string) ([]Permit, error) {
	rows, err := selectBindings(ctx, fusekiURL, enumerateInterventionsQuery)
	if err != nil {
		return nil, fmt.Errorf("coverage: enumerate interventions: %w", err)
	}

	seen := make(map[string]bool, len(rows))
	permits := make([]Permit, 0, len(rows))
	keyless := 0
	for _, row := range rows {
		iri := row["intervention"]
		if iri == "" || seen[iri] {
			continue
		}
		seen[iri] = true

		zaaknummer, ok := zaaknummerFromIRI(iri)
		if !ok {
			keyless++
			continue
		}

		dateStr := row["date"]
		pubDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return nil, fmt.Errorf("coverage: parse dct:available %q for %s: %w", dateStr, iri, err)
		}

		permits = append(permits, Permit{
			Zaaknummer:      zaaknummer,
			InterventionIRI: iri,
			PublicationDate: pubDate,
		})
	}
	if keyless > 0 {
		log.Printf("coverage: skipped %d keyless Intervention IRI(s) enumerated from the graph (no zaaknummer)", keyless)
	}
	return permits, nil
}

// zaaknummerFromIRI strips interventionPrefix from iri, reporting ok=false when iri does not carry
// the prefix or strips to an empty zaaknummer (task 6.3's keyless case).
func zaaknummerFromIRI(iri string) (string, bool) {
	if !strings.HasPrefix(iri, interventionPrefix) {
		return "", false
	}
	zaaknummer := strings.TrimPrefix(iri, interventionPrefix)
	if zaaknummer == "" {
		return "", false
	}
	return zaaknummer, true
}

// publicationQuery reads the audited besluit row for a zaaknummer — the one row in the trail
// (aanvraag/besluit/ontwerpbesluit/...) that carries a resolved place (design.md D8 in
// derive-coverage-audit's parent koop-publications context). resolved_identificatie is the 14-digit
// GBD buurt identificatie — the actual join key against kapenherplant."gbdBuurtId" — while
// resolved_buurt_code is the short, human-readable Amsterdam buurtcode kept only for gs:evidence text;
// the two are different Amsterdam code systems with no overlap. ST_AsBinary hands the point back to
// Go as portable WKB bytes rather than requiring a PostGIS-aware pgx type; candidateFellings below
// round-trips it via ST_GeomFromWKB.
const publicationQuery = `SELECT resolved_identificatie, resolved_buurt_code, resolved_tier, ST_AsBinary(resolved_geom) AS geom_wkb
FROM %s
WHERE zaaknummer = $1 AND resolved_identificatie IS NOT NULL
LIMIT 1`

// resolvePublication joins koop_publications by zaaknummer for the resolved GBD buurt identificatie
// (the join key), the short human-readable buurt code (evidence text only), the tier, and the point
// (as WKB, nil when the permit has no resolved_geom). found=false when no audited-besluit row exists
// for this zaaknummer.
func resolvePublication(ctx context.Context, pool *pgxpool.Pool, schema, zaaknummer string) (gbdBuurtID, buurtCode string, tier Tier, geomWKB []byte, found bool, err error) {
	table := pgx.Identifier{schema, "koop_publications"}.Sanitize()
	query := fmt.Sprintf(publicationQuery, table)

	var tierText string
	err = pool.QueryRow(ctx, query, zaaknummer).Scan(&gbdBuurtID, &buurtCode, &tierText, &geomWKB)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", "", nil, false, nil
		}
		return "", "", "", nil, false, fmt.Errorf("coverage: resolve publication for %s: %w", zaaknummer, err)
	}
	return gbdBuurtID, buurtCode, tierFromText(tierText), geomWKB, true, nil
}

// tierFromText maps koop_publications.resolved_tier's text values to Tier.
func tierFromText(s string) Tier {
	switch s {
	case string(TierAddress):
		return TierAddress
	case string(TierPostcode):
		return TierPostcode
	default:
		return TierBuurt
	}
}

// candidateFellingsQuery is D6(1)'s buurt+time SQL: felled kapenherplant rows in the permit's buurt
// whose felling date falls in [publication, +3yr] (inWindow/windowEnd below define this window; kept
// in agreement with this BETWEEN). The join key ($1) is the GBD buurt identificatie — the same
// 14-digit value koop_publications.resolved_identificatie carries, NOT the short human-readable
// buurtcode — since kapenherplant."gbdBuurtId" is minted in that identificatie system. source_deleted_at
// IS NULL excludes soft-deleted rows. The per-candidate distance is computed only when the permit
// carries a resolved point ($2 non-NULL); otherwise every DistanceM is nil and the buurt+time filter
// alone governs recall.
const candidateFellingsQuery = `SELECT id, "boomId", "kapmaatregelDatumUitgevoerd",
       CASE WHEN $2::bytea IS NULL THEN NULL
            ELSE ST_Distance(ST_Transform("resolvedGeom", 28992), ST_GeomFromWKB($2::bytea, 28992))
       END AS dist_m
FROM %s
WHERE "gbdBuurtId" = $1
  AND "kapmaatregelDatumUitgevoerd" IS NOT NULL
  AND "kapmaatregelDatumUitgevoerd" BETWEEN $3 AND $4
  AND source_deleted_at IS NULL`

// candidateFellings runs candidateFellingsQuery for one permit's GBD buurt identificatie + publication
// date window. gbdBuurtID must be the identificatie (koop_publications.resolved_identificatie), not the
// short buurtcode. geomWKB is nil for a point-less permit, yielding a nil DistanceM for every candidate
// (buurt+time filter only, recall preserved per D6).
func candidateFellings(ctx context.Context, pool *pgxpool.Pool, schema, gbdBuurtID string, geomWKB []byte, pubDate time.Time) ([]CandidateFelling, error) {
	table := pgx.Identifier{schema, "kapenherplant"}.Sanitize()
	query := fmt.Sprintf(candidateFellingsQuery, table)

	rows, err := pool.Query(ctx, query, gbdBuurtID, geomWKB, pubDate, windowEnd(pubDate))
	if err != nil {
		return nil, fmt.Errorf("coverage: query candidate fellings for buurt identificatie %s: %w", gbdBuurtID, err)
	}
	defer rows.Close()

	var out []CandidateFelling
	for rows.Next() {
		var c CandidateFelling
		var dist *float64
		if err := rows.Scan(&c.ID, &c.BoomID, &c.FellingDate, &dist); err != nil {
			return nil, fmt.Errorf("coverage: scan candidate felling: %w", err)
		}
		c.DistanceM = dist
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("coverage: iterate candidate fellings for buurt identificatie %s: %w", gbdBuurtID, err)
	}
	return out, nil
}

// windowEnd is the candidate window's upper bound (D6): publication date + 3 years.
func windowEnd(pub time.Time) time.Time {
	return pub.AddDate(3, 0, 0)
}

// inWindow reports whether felling falls in [pub, windowEnd(pub)] — a felling strictly before pub is
// excluded (D6's "felling before publication" scenario). The SQL BETWEEN in candidateFellingsQuery
// must agree with this definition.
func inWindow(pub, felling time.Time) bool {
	end := windowEnd(pub)
	return !felling.Before(pub) && !felling.After(end)
}
