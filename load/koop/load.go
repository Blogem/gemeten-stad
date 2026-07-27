package koop

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Blogem/gemeten-stad/ingest/shared"
	loadgraph "github.com/Blogem/gemeten-stad/load/graph"
)

// Config configures a koop Load run.
type Config struct {
	// Reset drops and rebuilds koop's own PostGIS target (koop_publications) from the landed
	// data before loading. It never reaches the graph store — see Load's doc comment on why
	// Reset is never forwarded to loadgraph.Load.
	Reset bool
}

// Load orchestrates the full koop backbone load: read the landed SRU corpus, dedup each zaak down
// to the besluit (or ontwerpbesluit stand-in) worth auditing, resolve that besluit's location and
// scope it to stadsdeel Noord, assemble the audited besluiten into a Turtle candidate and write it
// through the SHACL-gated graph loader (load/graph.Load), and persist the full publication trail —
// every publication, not just besluiten — plus its resolution to PostGIS.
//
// Scoping policy (task 5.1/7.1 — resolves the spec's loose "in scope" definition):
//
//   - Keyless publications (no landed metadata sidecar, Zaaknummer == "") can never be dedup'd into
//     a zaak or audited: each is persisted to PostGIS as its own row, flagged Resolved{Unresolved:
//     true}, and never graphed.
//   - A zaak with no besluit and no ontwerpbesluit yet ("pending", selectBesluit's ok == false) is
//     persisted as a trail with no resolution attempted (every row's Res is nil) — it becomes
//     auditable once its besluit lands in a later run.
//   - A zaak whose besluit (or ontwerpbesluit stand-in) cannot be placed anywhere
//     (res.Unresolved == true) is persisted as a trail: the besluit's own row carries the
//     Unresolved resolution, every other publication in the trail carries Res: nil. Never graphed.
//   - A zaak whose besluit resolves outside Noord (res.InNoord == false) is excluded entirely — no
//     PostGIS rows for any publication in the zaak, no graph write. Vertical 1 audits stadsdeel
//     Noord only (docs/IMPLEMENTATION_PLAN.md), and the resolved buurt is the sole scoping signal:
//     a zaak's zaaknummer prefix is never used to decide exclusion (a defensive cross-check for the
//     opposite case — a mismatch between the resolved scope and the zaaknummer prefix — already
//     lives in resolve.go's warnIfNoordMismatch, log-only).
//   - Every other zaak (a besluit resolved and in Noord) is persisted as a trail — the besluit's row
//     carries its resolution, every other publication in the trail carries Res: nil — and is what
//     gets graphed as an audited Intervention.
//
// Reset governs koop's OWN PostGIS target (koop_publications) only. It is never forwarded to the
// graph writer: loadgraph.Config.Reset clears EVERY prior run graph in Fuseki (bomen's and places'
// contributions included, not just koop's), so koop always calls loadgraph.Load with
// Reset: false regardless of cfg.Reset — a koop-only --reset must never destroy graph data that
// belongs to another load stage.
func Load(ctx context.Context, pool *pgxpool.Pool, store *shared.RawStore, fusekiURL string, cfg Config) error {
	pubs, err := readPublications(store)
	if err != nil {
		return fmt.Errorf("koop: load: %w", err)
	}
	groups := groupByZaak(pubs)

	var rows []PublicationRow
	var audited []AuditedBesluit
	var besluitenLoaded, excludedNonNoord, pending, unresolvable, keyless, resolveFailed int

	if keylessPubs, ok := groups[""]; ok {
		for _, p := range keylessPubs {
			rows = append(rows, PublicationRow{Pub: p, Res: &Resolved{Unresolved: true}})
			keyless++
		}
		delete(groups, "")
	}

	for zaaknummer, group := range groups {
		besluit, ok := selectBesluit(group)
		if !ok {
			// Pending: no besluit or ontwerpbesluit has landed yet. Keep the whole trail, no
			// resolution attempted, no graph write.
			for _, p := range group {
				rows = append(rows, PublicationRow{Pub: p, Res: nil})
			}
			pending++
			continue
		}

		res, err := resolveBesluit(ctx, pool, besluit)
		if err != nil {
			slog.Warn("koop: skipping zaak (resolve failed)", "zaak", zaaknummer, "err", err)
			resolveFailed++
			continue
		}

		switch {
		case res.Unresolved:
			appendTrail(&rows, group, besluit, res)
			unresolvable++

		case !res.InNoord:
			// Excluded: outside stadsdeel Noord. No rows, no graph — the whole zaak is dropped.
			excludedNonNoord++

		default:
			appendTrail(&rows, group, besluit, res)
			audited = append(audited, AuditedBesluit{Pub: besluit, Res: res})
			besluitenLoaded++
		}
	}

	if err := ensureExtensions(ctx, pool); err != nil {
		return fmt.Errorf("koop: load: %w", err)
	}
	schema, err := loadSchema(ctx, pool)
	if err != nil {
		return fmt.Errorf("koop: load: %w", err)
	}
	if cfg.Reset {
		if err := dropTargets(ctx, pool, schema); err != nil {
			return fmt.Errorf("koop: load: %w", err)
		}
	}
	if err := ensureSchema(ctx, pool, schema); err != nil {
		return fmt.Errorf("koop: load: %w", err)
	}
	if err := stagePublications(ctx, pool, schema, rows); err != nil {
		return fmt.Errorf("koop: load: %w", err)
	}
	if err := upsertPublications(ctx, pool, schema, time.Now().UTC()); err != nil {
		return fmt.Errorf("koop: load: %w", err)
	}

	candidate, graphSkipped := buildCandidate(audited)
	for _, id := range graphSkipped {
		slog.Warn("koop: besluit skipped from graph (unsafe IRI)", "id", id)
	}
	if len(candidate) > 0 {
		// Reset is always false: koop's own cfg.Reset governs only koop_publications above.
		// Forwarding it would let a koop-only --reset clear every run graph in Fuseki, destroying
		// bomen/places data unrelated to this load (see Load's doc comment). A rejected candidate
		// here is still a batch-level, fatal failure — the SHACL gate is never weakened.
		if err := loadgraph.Load(ctx, fusekiURL, candidate, loadgraph.Config{Reset: false}); err != nil {
			return fmt.Errorf("koop: load: %w", err)
		}
	}

	slog.Info("koop: load complete",
		"besluitenLoaded", besluitenLoaded,
		"excludedNonNoord", excludedNonNoord,
		"pending", pending,
		"unresolvable", unresolvable,
		"keyless", keyless,
		"resolveFailed", resolveFailed,
		"graphSkipped", len(graphSkipped),
	)

	return nil
}

// appendTrail appends every publication in group as a PublicationRow: besluit's own row carries
// res (matched by Publication.ID, which selectBesluit picked group from), every other publication
// in the trail carries a nil Res.
func appendTrail(rows *[]PublicationRow, group []Publication, besluit Publication, res Resolved) {
	for _, p := range group {
		if p.ID == besluit.ID {
			*rows = append(*rows, PublicationRow{Pub: p, Res: &res})
		} else {
			*rows = append(*rows, PublicationRow{Pub: p, Res: nil})
		}
	}
}
