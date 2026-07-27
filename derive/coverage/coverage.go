package coverage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Blogem/gemeten-stad/load/graph"
)

// Config configures one coverage.Run invocation. Reset drops only the
// audit_metrics PostGIS table before ensuring it fresh (design.md D9) — it
// never resets the graph writer's shared run graphs, which stay under the
// P12 load path's own (unrelated) reset semantics.
type Config struct {
	Reset bool
}

// Run implements the P14 coverage-audit derive step end to end (design.md
// D5-D7, D9, D10): generate candidate permit<->felling pairs, score every
// pair, exclusively assign each felling to its single best permit, score each
// permit's assigned set into a final Outcome, assemble the anchor + period
// turtle, write it through the SHACL-gated graph loader, and persist the
// derived numbers to the slim audit_metrics table.
func Run(ctx context.Context, pool *pgxpool.Pool, fusekiURL string, cfg Config) error {
	var schema string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		return fmt.Errorf("coverage: resolve current schema: %w", err)
	}

	if cfg.Reset {
		if err := DropAuditMetrics(ctx, pool, schema); err != nil {
			return err
		}
	}
	if err := EnsureAuditMetrics(ctx, pool, schema); err != nil {
		return err
	}

	permits, err := GenerateCandidates(ctx, pool, schema, fusekiURL)
	if err != nil {
		return fmt.Errorf("coverage: generate candidates: %w", err)
	}

	runID := time.Now().UTC().Format("20060102T150405Z")
	validFrom := time.Now().UTC()

	pairs := scorePairs(permits)
	assignment := Assign(pairs)
	contendersByFelling := contention(permits)

	assembleds := make([]Assembled, 0, len(permits))
	metrics := make([]Metric, 0, len(permits))
	matchedCount := 0

	for _, p := range permits {
		assigned := assignment[p.Zaaknummer]

		var outcome Outcome
		var evidence string
		var nearest *float64

		if len(assigned) == 0 {
			outcome = Outcome{Matched: false}
			evidence = fmt.Sprintf("searched buurt %s, window [%s,+3yr]: 0 fellings",
				p.BuurtCode, p.PublicationDate.Format("2006-01-02"))
			nearest = nearestCandidateDistance(p.Candidates)
		} else {
			matchedCount++

			byID := make(map[string]CandidateFelling, len(p.Candidates))
			for _, c := range p.Candidates {
				byID[c.ID] = c
			}

			minLag := -1
			var nearestAssigned *float64
			contenders := 1
			for _, fellingID := range assigned {
				felling, ok := byID[fellingID]
				if !ok {
					continue
				}
				lag := daysBetween(p.PublicationDate, felling.FellingDate)
				if minLag == -1 || lag < minLag {
					minLag = lag
				}
				if felling.DistanceM != nil && (nearestAssigned == nil || *felling.DistanceM < *nearestAssigned) {
					nearestAssigned = felling.DistanceM
				}
				if c := contendersByFelling[fellingID]; c > contenders {
					contenders = c
				}
			}
			if minLag == -1 {
				minLag = 0
			}

			res := Score(ScoreInput{
				Tier:          p.Tier,
				NearestDistM:  nearestAssigned,
				LagDays:       minLag,
				RegistryCount: len(assigned),
				PermitCount:   nil,
				Contenders:    contenders,
			})

			observationIRI := "http://gemetenstad.nl/id/observation/" + p.Zaaknummer
			outcome = Outcome{
				Matched:        true,
				ObservationIRI: observationIRI,
				Confidence:     res.Confidence,
				Granularity:    res.Granularity,
				Caveats:        res.Caveats,
			}

			if res.Confidence < Tau {
				evidence = fmt.Sprintf("matched %d felling(s) in buurt %s, score %.2f (weak link)",
					len(assigned), p.BuurtCode, res.Confidence)
			} else {
				evidence = fmt.Sprintf("matched %d felling(s) in buurt %s, score %.2f",
					len(assigned), p.BuurtCode, res.Confidence)
			}

			nearest = nearestAssigned
		}

		assembleds = append(assembleds, Assembled{
			Zaaknummer:      p.Zaaknummer,
			InterventionIRI: p.InterventionIRI,
			Outcome:         outcome,
			ValidFrom:       validFrom,
			Evidence:        evidence,
			DerivedFrom:     []string{p.InterventionIRI},
		})

		metrics = append(metrics, Metric{
			Zaaknummer:           p.Zaaknummer,
			Matched:              len(assigned) > 0,
			AssignedFellingIDs:   assigned,
			AssignedFellingCount: len(assigned),
			CandidateCount:       len(p.Candidates),
			NearestDistM:         nearest,
			RunID:                runID,
		})
	}

	turtle, err := Assemble(assembleds)
	if err != nil {
		return fmt.Errorf("coverage: assemble: %w", err)
	}

	if err := graph.Load(ctx, fusekiURL, turtle, graph.Config{Reset: false}); err != nil {
		return fmt.Errorf("coverage: load graph: %w", err)
	}

	if err := UpsertAuditMetrics(ctx, pool, schema, metrics); err != nil {
		return fmt.Errorf("coverage: upsert audit_metrics: %w", err)
	}

	log.Printf("coverage: run %s: %d permits, %d matched, %d no-source",
		runID, len(permits), matchedCount, len(permits)-matchedCount)
	return nil
}

// scorePairs scores every permit<->candidate-felling pair for the exclusive
// assignment step (design.md D6(2)): RegistryCount=1/Contenders=1 per pair —
// the assignment scoring is per-pair, not per-assigned-set (that happens
// afterward, once per permit, over its actual assigned set).
func scorePairs(permits []Permit) []Pair {
	var pairs []Pair
	for _, p := range permits {
		for _, f := range p.Candidates {
			s := Score(ScoreInput{
				Tier:          p.Tier,
				NearestDistM:  f.DistanceM,
				LagDays:       daysBetween(p.PublicationDate, f.FellingDate),
				RegistryCount: 1,
				PermitCount:   nil,
				Contenders:    1,
			}).Confidence
			pairs = append(pairs, Pair{
				Zaaknummer: p.Zaaknummer,
				FellingID:  f.ID,
				Score:      s,
				DistanceM:  f.DistanceM,
			})
		}
	}
	return pairs
}

// contention builds fellingID -> number of distinct permits that had it as a
// candidate (design.md D6(2)'s ambiguity term): 1 means the felling was a
// sole/clean candidate for exactly one permit.
func contention(permits []Permit) map[string]int {
	permitsByFelling := make(map[string]map[string]bool)
	for _, p := range permits {
		for _, f := range p.Candidates {
			set, ok := permitsByFelling[f.ID]
			if !ok {
				set = make(map[string]bool)
				permitsByFelling[f.ID] = set
			}
			set[p.Zaaknummer] = true
		}
	}
	out := make(map[string]int, len(permitsByFelling))
	for fellingID, set := range permitsByFelling {
		out[fellingID] = len(set)
	}
	return out
}

// nearestCandidateDistance returns the minimum non-nil DistanceM across
// candidates, or nil when every candidate is point-less (or there are none).
func nearestCandidateDistance(candidates []CandidateFelling) *float64 {
	var nearest *float64
	for _, c := range candidates {
		if c.DistanceM != nil && (nearest == nil || *c.DistanceM < *nearest) {
			nearest = c.DistanceM
		}
	}
	return nearest
}

// daysBetween returns the felling->publication gap in days: b (felling) is
// expected >= a (publication) — candidates are windowed upstream to exclude
// an earlier felling.
func daysBetween(a, b time.Time) int {
	return int(b.Sub(a).Hours() / 24)
}
