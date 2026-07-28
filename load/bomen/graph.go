package bomen

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// gsNS/dataNS mirror the settled RDF namespaces (load/graph/signature.go, load/koop/graph.go,
// derive/coverage/assemble.go): gs: for the TBox, data: for every instance IRI change detection is
// keyed on (design.md D1). treeNS/fellingNS are the two instance IRI namespaces this package
// mints — treeNS MUST match ontology/shapes.ttl's gs:FellingShape sh:pattern
// ("^http://gemetenstad\\.nl/id/tree/") and fellingNS MUST match gs:ObservationShape's sh:pattern
// on gs:includesFelling ("^http://gemetenstad\\.nl/id/felling/"), since both are CROSS-LOAD
// references gated by namespace pattern rather than sh:class (model-felled-trees D5).
const (
	gsNS      = "http://gemetenstad.nl/ns#"
	dataNS    = "http://gemetenstad.nl/id/"
	treeNS    = dataNS + "tree/"
	fellingNS = dataNS + "felling/"
)

// turtlePreamble declares the prefixes every emitted candidate needs: gs: (TBox) and xsd: (the
// gs:felledOn date literal). Instance IRIs (tree/felling) are deliberately written as full
// angle-bracket IRIs, not prefixed names — boomId/kapenherplant id are registry-sourced,
// untrusted-ish input (mirrors load/koop/graph.go's rationale).
const turtlePreamble = `@prefix gs: <` + gsNS + `> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .

`

// disallowedIRIChars mirrors load/graph/signature.go's assertSafeIRI forbidden set: the characters
// a Turtle IRIREF may never contain unescaped.
const disallowedIRIChars = "<>\"{}|^`\\"

// assertSafeIRI rejects id if it contains any character not permitted raw inside a Turtle IRIREF
// (control chars, space, or one of disallowedIRIChars) — the single choke point every boomId/
// kapenherplant id passes through before it is minted into an IRI (mirrors load/koop/graph.go's
// assertSafeIRI and derive/coverage/assemble.go's assertSafeIRI).
func assertSafeIRI(id string) error {
	for _, r := range id {
		if r <= 0x20 || strings.ContainsRune(disallowedIRIChars, r) {
			return fmt.Errorf("bomen: unsafe IRI component %q: contains disallowed character %U", id, r)
		}
	}
	return nil
}

// mintTreeIRI, mintFellingIRI return the full instance IRI for a felled tree's boomId / a
// kapenherplant row's own id. Callers must validate the input via assertSafeIRI first
// (renderFelledRow does this once per row before any minting) — these are pure string
// concatenation and trust their input.
func mintTreeIRI(boomID string) string { return treeNS + boomID }
func mintFellingIRI(id string) string  { return fellingNS + id }

// FelledRow is one felled kapenherplant row: the record id (the felling act, 1:1 with the row),
// the boomId of the tree it felled, and the felling date. queryFelledRows only ever returns rows
// with a non-null kapmaatregelDatumUitgevoerd (a "felled" row, model-felled-trees D4), so FelledOn
// is expected non-zero here — buildFelledCandidate still guards it defensively (mirrors
// load/koop/graph.go's fail-loud-or-skip pattern for a caller-supplied date).
type FelledRow struct {
	ID       string
	BoomID   string
	FelledOn time.Time
}

// renderFelledRow validates row and writes its Turtle triples into b: the tree (only if not
// already emitted for this boomId — seenTrees dedups across the 17 boomIds with >1 felling row,
// design.md Open Questions) and the felling event naming it. A row with an empty boomId or an
// unparseable/absent felling date is a row a gs:Felling can never validly reference (FellingShape
// requires gs:felledTree and gs:felledOn) — this function fails loud on those so the caller's
// skip-and-record contract (mirroring load/koop/graph.go's buildCandidate) can surface it without
// aborting the whole batch.
func renderFelledRow(b *strings.Builder, row FelledRow, seenTrees map[string]bool) error {
	if row.BoomID == "" {
		return fmt.Errorf("bomen: felling %s: empty boomId", row.ID)
	}
	if err := assertSafeIRI(row.ID); err != nil {
		return fmt.Errorf("bomen: felling id: %w", err)
	}
	if err := assertSafeIRI(row.BoomID); err != nil {
		return fmt.Errorf("bomen: felling %s: boomId: %w", row.ID, err)
	}
	if row.FelledOn.IsZero() {
		return fmt.Errorf("bomen: felling %s: missing felledOn date", row.ID)
	}

	treeIRI := mintTreeIRI(row.BoomID)
	fellingIRI := mintFellingIRI(row.ID)

	if !seenTrees[row.BoomID] {
		fmt.Fprintf(b, "<%s> a gs:Tree .\n", treeIRI)
		seenTrees[row.BoomID] = true
	}
	fmt.Fprintf(b, "<%s> a gs:Felling ;\n", fellingIRI)
	fmt.Fprintf(b, "    gs:felledTree <%s> ;\n", treeIRI)
	fmt.Fprintf(b, "    gs:felledOn \"%s\"^^xsd:date .\n\n", row.FelledOn.UTC().Format("2006-01-02"))

	return nil
}

// buildFelledCandidate renders each felled row into a shared Turtle candidate, skipping (not
// failing on) any row whose IRIs/date are invalid; skipped holds the kapenherplant ids of those
// rows so the caller can log them (mirrors load/koop/graph.go's buildCandidate). Returns nil
// candidate when rows is empty OR every row was skipped.
//
// buildFelledCandidate is a PURE function (no DB, no network, no clock beyond formatting row's own
// FelledOn, no logging — the caller logs skipped) from felled rows to a Turtle candidate: one
// shared prefix preamble followed by one block per rendered row, in input order, with the gs:Tree
// triple deduplicated across rows sharing the same boomId.
func buildFelledCandidate(rows []FelledRow) (candidate []byte, skipped []string) {
	if len(rows) == 0 {
		return nil, nil
	}

	var b strings.Builder
	b.WriteString(turtlePreamble)

	seenTrees := make(map[string]bool, len(rows))
	rendered := 0
	for _, row := range rows {
		if err := renderFelledRow(&b, row, seenTrees); err != nil {
			skipped = append(skipped, row.ID)
			continue
		}
		rendered++
	}

	if rendered == 0 {
		return nil, skipped
	}
	return []byte(b.String()), skipped
}

// queryFelledRows reads every kapenherplant target row with a non-null kapmaatregelDatumUitgevoerd
// (a "felled" row, model-felled-trees D4) from the schema-qualified kapenherplant table. Trees
// never felled (a null felling date) are never returned — they must not enter the graph.
func queryFelledRows(ctx context.Context, pool *pgxpool.Pool, schema string) ([]FelledRow, error) {
	sql := fmt.Sprintf(
		`SELECT id, coalesce("boomId", ''), "kapmaatregelDatumUitgevoerd" FROM %s WHERE "kapmaatregelDatumUitgevoerd" IS NOT NULL`,
		qualify(schema, kapenherplantTable),
	)
	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("bomen: query felled rows: %w", err)
	}
	defer rows.Close()

	var out []FelledRow
	for rows.Next() {
		var r FelledRow
		if err := rows.Scan(&r.ID, &r.BoomID, &r.FelledOn); err != nil {
			return nil, fmt.Errorf("bomen: scan felled row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
