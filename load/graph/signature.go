package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	// gsNS/dataNS are the settled RDF namespaces (ontology/ontology.ttl, IMPLEMENTATION_PLAN.md
	// §3): gs: for the TBox (classes, properties, gs:validFrom/validTo/confidence/evidence) and
	// data: for every entity IRI change detection is keyed on (design.md D1).
	gsNS   = "http://gemetenstad.nl/ns#"
	dataNS = "http://gemetenstad.nl/id/"

	// stageGraphPrefix identifies a run's candidate-only staging graph: run:stage-<runID>,
	// distinct from the P8 validation-merge scratch graph (run:validate-<runID>, scratchGraphFor)
	// — design.md D2. It holds ONLY the candidate (no ontology/vocab merge), so the signature
	// extractor never has to filter out reference-model triples.
	stageGraphPrefix = gsRunNS + "stage-"

	// sigSeparator joins a subject's content rows inside GROUP_CONCAT (below). It is the ASCII
	// Unit Separator control character (U+001F) — legal inside a SPARQL string literal and
	// vanishingly unlikely to collide with real IRI/literal text, unlike a printable delimiter.
	sigSeparator = "\x1f"
)

// stageGraphFor returns the transient candidate-only staging graph IRI for runID
// (run:stage-<runID>) — design.md D2.
func stageGraphFor(runID string) string {
	return stageGraphPrefix + runID
}

// stageCandidate posts candidate (Turtle) into its own run:stage-<runID> graph (task 2.1). It is
// POSTed, not PUT, for symmetry with the rest of the package's merge-graph convention, though the
// graph is always freshly named per run so POST vs PUT makes no observable difference here.
func (c *client) stageCandidate(ctx context.Context, runID string, candidate []byte) error {
	if err := c.postGraph(ctx, stageGraphFor(runID), candidate); err != nil {
		return fmt.Errorf("graph: stage candidate into %s: %w", stageGraphFor(runID), err)
	}
	return nil
}

// signatureRowPattern is the shared graph pattern (design.md D2: "the same SELECT runs against"
// both the staging graph and the live open versions) yielding, per data: subject, one (?s ?row)
// solution for every valid-time-agnostic content row: a plain (predicate, object) triple, or an
// RDF-star annotation row on one of the subject's own edges (confidence/evidence/caveat) —
// EXCLUDING gs:validFrom/gs:validTo themselves (design.md D3). A row belonging to a CLOSED prior
// version is excluded entirely — that is exactly the "open" filter live extraction (2.3) needs, and
// a harmless no-op against the staging graph, which the writer itself never stamps validTo into
// (candidates never carry gs:validTo). "Closed" is detected per form
// (state-node-versioning design.md D1):
//   - annotation form: the row's own <<s,p,o>> already carries a gs:validTo stamp.
//   - node form: the SUBJECT itself already carries a gs:validTo stamp (a closed period node) —
//     this excludes ALL of that subject's plain-triple rows, so a closed period contributes nothing
//     to the live-open set and simply does not appear in signatureQuery's result at all.
//
// The annotation branch also excludes rdf:reifies: Jena 5.5 represents an RDF-star annotation as a
// blank-node reifier (`?ep ?eo` annotated by blank node ?ap=?r via `?r rdf:reifies <<( ?s ?ep ?eo
// )>>`), and that reifies-linkage triple itself would otherwise surface as a signature row keyed on
// plumbing (a blank node identity, or the triple-term value), not content — and one the copy
// (copyDeltaAndRecordProvenance) reproduces verbatim, so including it here only risks instability,
// never adds change-detection signal: the reified triple's actual content is already covered by row
// branch 1 above.
const signatureRowPattern = `
    { ?s ?p ?o .
      FILTER(STRSTARTS(STR(?s), "` + dataNS + `"))
      FILTER(?p != gs:validFrom && ?p != gs:validTo)
      FILTER NOT EXISTS { <<?s ?p ?o>> gs:validTo ?closed }
      FILTER NOT EXISTS { ?s gs:validTo ?closedNode }
      BIND(CONCAT(STR(?p), " ", STR(?o)) AS ?row)
    }
    UNION
    { <<?s ?ep ?eo>> ?ap ?av .
      FILTER(STRSTARTS(STR(?s), "` + dataNS + `"))
      FILTER(?ap != gs:validFrom && ?ap != gs:validTo && ?ap != rdf:reifies)
      FILTER NOT EXISTS { <<?s ?ep ?eo>> gs:validTo ?closedAnn }
      BIND(CONCAT("ann ", STR(?ep), " ", STR(?eo), " ", STR(?ap), " ", STR(?av)) AS ?row)
    }
`

// evolvingFlagPattern marks a subject "evolving" (design.md D4) iff it asserts gs:validFrom either
// directly (a node-form period node it owns, keyed to its series via gs:versionOf — see
// state-node-versioning design.md D1) or as an RDF-star annotation on one of its own edges. Both
// forms are fully versioned by this writer: node-form open/close is series-keyed on gs:versionOf
// (write.go's closeSeriesPriors + the node-form branch of verifyOpenInvariant); annotation-form
// open/close stays subject-keyed (write.go's closePriors).
const evolvingFlagPattern = `
    { ?s gs:validFrom ?vf }
    UNION
    { <<?s ?evp ?evo>> gs:validFrom ?vf2 }
`

// signatureQuery builds the shared per-entity signature SELECT (task 2.2/2.3) over the given named
// graphs (unioned): one row per subject with its order-stable, valid-time-agnostic GROUP_CONCAT
// signature and whether it is evolving. graphIRIs is a single staging graph for 2.2, or every
// current run:load-* graph for 2.3 (D2's "union of run:load-* graphs"). Each per-graph block is
// wrapped in its own `{ GRAPH <g> { ... } }` braces: a bare GraphGraphPattern is a valid standalone
// pattern but NOT a valid UNION operand (UNION requires a full GroupGraphPattern on each side), so
// without the extra braces the join below is malformed SPARQL as soon as graphIRIs has 2+ entries.
//
// The inner subquery projects (?s, ?row) ordered by (?s, ?row) before the outer GROUP_CONCAT — the
// standard order-stable-GROUP_CONCAT idiom (ORDER BY inside, aggregate outside) — so two runs over
// identical content always produce byte-identical signature strings, regardless of the store's
// internal row order. The evolving flag joins in via an OPTIONAL sub-SELECT so subjects with no
// gs:validFrom at all still appear (immutable entities), with ?evolving left unbound (-> false).
func signatureQuery(graphIRIs []string) string {
	// Each per-graph block is wrapped in its own group graph pattern ({ ... }) so the "\nUNION\n"
	// join below produces `{ GRAPH <g1> {...} } UNION { GRAPH <g2> {...} }` — SPARQL UNION combines
	// group graph patterns, so a bare `GRAPH <g1> {...} UNION GRAPH <g2> {...}` is a parse error the
	// moment a second run:load-* graph exists (i.e. any second Load).
	rowBlocks := make([]string, len(graphIRIs))
	evolvingBlocks := make([]string, len(graphIRIs))
	for i, g := range graphIRIs {
		rowBlocks[i] = "{ GRAPH <" + g + "> {" + signatureRowPattern + "} }"
		evolvingBlocks[i] = "{ GRAPH <" + g + "> {" + evolvingFlagPattern + "} }"
	}

	return fmt.Sprintf(`
PREFIX gs: <%s>
PREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#>
SELECT ?s (GROUP_CONCAT(DISTINCT ?row; separator="%s") AS ?sig) (SAMPLE(?evolvingFlag) AS ?evolving)
WHERE {
  {
    SELECT ?s ?row WHERE {
      %s
    }
    ORDER BY ?s ?row
  }
  OPTIONAL {
    SELECT DISTINCT ?s ("1" AS ?evolvingFlag) WHERE {
      %s
    }
  }
}
GROUP BY ?s
`, gsNS, sigSeparator, strings.Join(rowBlocks, "\nUNION\n"), strings.Join(evolvingBlocks, "\nUNION\n"))
}

// signatureSelectResult is the SPARQL 1.1 JSON results shape of signatureQuery's SELECT.
type signatureSelectResult struct {
	Results struct {
		Bindings []struct {
			S struct {
				Value string `json:"value"`
			} `json:"s"`
			Sig struct {
				Value string `json:"value"`
			} `json:"sig"`
			Evolving struct {
				Value string `json:"value"`
			} `json:"evolving"`
		} `json:"bindings"`
	} `json:"results"`
}

// selectSignatures runs signatureQuery(graphIRIs) and parses the result into map[iri]entitySignature
// — the shared extractor behind both stagingSignatures (2.2) and liveOpenSignatures (2.3). An empty
// graphIRIs (e.g. no run:load-... graph exists yet, the very first Load) short-circuits to an empty
// map without a round-trip.
func (c *client) selectSignatures(ctx context.Context, graphIRIs []string) (map[iri]entitySignature, error) {
	if len(graphIRIs) == 0 {
		return map[iri]entitySignature{}, nil
	}

	query := signatureQuery(graphIRIs)
	req, err := c.newRequest(ctx, http.MethodGet, "/sparql?query="+url.QueryEscape(query), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/sparql-results+json")
	body, err := c.do(req, "extract entity signatures")
	if err != nil {
		return nil, err
	}

	var result signatureSelectResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("graph: extract entity signatures: parse SPARQL JSON results: %w", err)
	}

	signatures := make(map[iri]entitySignature, len(result.Results.Bindings))
	for _, b := range result.Results.Bindings {
		signatures[iri(b.S.Value)] = entitySignature{
			evolving:  b.Evolving.Value == "1",
			signature: b.Sig.Value,
		}
	}
	return signatures, nil
}

// stagingSignatures extracts per-entity signatures from the run's own candidate-only staging graph
// (task 2.2) — run:stage-<runID>.
func (c *client) stagingSignatures(ctx context.Context, runID string) (map[iri]entitySignature, error) {
	signatures, err := c.selectSignatures(ctx, []string{stageGraphFor(runID)})
	if err != nil {
		return nil, fmt.Errorf("graph: staging signatures: %w", err)
	}
	return signatures, nil
}

// liveOpenSignatures extracts per-entity signatures from every currently-open version across the
// union of all run:load-* graphs (task 2.3) — "open" is enforced row-by-row inside
// signatureRowPattern (a row whose owning triple already carries gs:validTo is excluded). An empty
// dataset (no prior runs) returns an empty map, not an error.
func (c *client) liveOpenSignatures(ctx context.Context) (map[iri]entitySignature, error) {
	graphs, err := c.graphsWithPrefix(ctx, runGraphPrefix)
	if err != nil {
		return nil, fmt.Errorf("graph: live signatures: list run graphs: %w", err)
	}
	signatures, err := c.selectSignatures(ctx, graphs)
	if err != nil {
		return nil, fmt.Errorf("graph: live signatures: %w", err)
	}
	return signatures, nil
}

// stagingValidFrom reads, for each of the given (changed) subject IRIs, the single gs:validFrom
// value the candidate carries for it in the run's staging graph — the world-time date the writer
// stamps as the prior version's gs:validTo on close (design.md D3: the writer never invents
// validFrom, it only reads back what the caller already asserted). Its first UNION branch
// (`?s gs:validFrom ?vf`) already covers a node-form period's own stamp; that branch is exercised
// today only by the (rare) case of a node-form subject reappearing in the annotation-form `changed`
// bucket. New node-form periods — the common case, per D1's "a changed period arrives as a new
// subject" — are read instead by stagingPeriodAnchors below, which also needs the period's
// gs:versionOf anchor.
func (c *client) stagingValidFrom(ctx context.Context, runID string, changed []iri) (map[iri]string, error) {
	if len(changed) == 0 {
		return map[iri]string{}, nil
	}

	query := fmt.Sprintf(`
PREFIX gs: <%s>
SELECT ?s ?vf WHERE {
  VALUES ?s { %s }
  GRAPH <%s> {
    { ?s gs:validFrom ?vf }
    UNION
    { <<?s ?p ?o>> gs:validFrom ?vf }
  }
}
`, gsNS, iriValuesList(changed), stageGraphFor(runID))

	req, err := c.newRequest(ctx, http.MethodGet, "/sparql?query="+url.QueryEscape(query), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/sparql-results+json")
	body, err := c.do(req, "read staging gs:validFrom")
	if err != nil {
		return nil, err
	}

	var result struct {
		Results struct {
			Bindings []struct {
				S struct {
					Value string `json:"value"`
				} `json:"s"`
				Vf struct {
					Value    string `json:"value"`
					Datatype string `json:"datatype"`
				} `json:"vf"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("graph: read staging gs:validFrom: parse SPARQL JSON results: %w", err)
	}

	validFrom := make(map[iri]string, len(result.Results.Bindings))
	for _, b := range result.Results.Bindings {
		term, err := literalTerm(b.Vf.Value, b.Vf.Datatype)
		if err != nil {
			return nil, fmt.Errorf("graph: read staging gs:validFrom: %w", err)
		}
		validFrom[iri(b.S.Value)] = term
	}
	return validFrom, nil
}

// periodAnchor is a node-form period's series identity, read back from the staging graph: the
// stable gs:versionOf anchor IRI it belongs to, plus its own gs:validFrom already rendered as a
// SPARQL literal term (see literalTerm) — everything closeSeriesPriors (write.go) needs to close
// the anchor's prior open period(s) without ever inventing a validFrom itself (design.md D1/D3).
type periodAnchor struct {
	anchor    iri
	validFrom string
}

// stagingPeriodAnchors reads, for each of the given subject IRIs (in practice, upsert's newIRIs —
// D1: a changed period arrives as a NEW content-derived IRI, never a mutated one), whether the
// candidate asserts it as a node-form period node in the run's staging graph: a subject carrying
// both a `gs:versionOf <anchor>` pointer and a node-triple `gs:validFrom`. A subject in ids that is
// NOT a period node (no gs:versionOf) simply has no entry in the returned map — the caller only
// series-closes the subjects that DO appear here. The anchor IRI is read back out of the store
// (STR(?a) in the SELECT) and is therefore untrusted input exactly like any other subject IRI this
// package re-embeds into SPARQL; the caller MUST route every returned anchor through assertSafeIRI
// before building closeSeriesPriors' VALUES clause (mirrors the security gate in upsert).
func (c *client) stagingPeriodAnchors(ctx context.Context, runID string, ids []iri) (map[iri]periodAnchor, error) {
	if len(ids) == 0 {
		return map[iri]periodAnchor{}, nil
	}

	query := fmt.Sprintf(`
PREFIX gs: <%s>
SELECT ?s ?a ?vf WHERE {
  VALUES ?s { %s }
  GRAPH <%s> {
    ?s gs:versionOf ?a ; gs:validFrom ?vf .
  }
}
`, gsNS, iriValuesList(ids), stageGraphFor(runID))

	req, err := c.newRequest(ctx, http.MethodGet, "/sparql?query="+url.QueryEscape(query), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/sparql-results+json")
	body, err := c.do(req, "read staging period anchors")
	if err != nil {
		return nil, err
	}

	var result struct {
		Results struct {
			Bindings []struct {
				S struct {
					Value string `json:"value"`
				} `json:"s"`
				A struct {
					Value string `json:"value"`
				} `json:"a"`
				Vf struct {
					Value    string `json:"value"`
					Datatype string `json:"datatype"`
				} `json:"vf"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("graph: read staging period anchors: parse SPARQL JSON results: %w", err)
	}

	periods := make(map[iri]periodAnchor, len(result.Results.Bindings))
	for _, b := range result.Results.Bindings {
		term, err := literalTerm(b.Vf.Value, b.Vf.Datatype)
		if err != nil {
			return nil, fmt.Errorf("graph: read staging period anchors: %w", err)
		}
		periods[iri(b.S.Value)] = periodAnchor{anchor: iri(b.A.Value), validFrom: term}
	}
	return periods, nil
}

// literalEscaper escapes a literal's lexical value for safe embedding inside a double-quoted
// SPARQL string literal ("...") in a single left-to-right, non-overlapping pass (the
// strings.Replacer guarantee): backslash is escaped FIRST in that same pass, so the backslash
// introduced by escaping a quote is never itself re-escaped, and a value ending in a bare
// backslash can no longer swallow the literal's closing quote (the injection this replaces: a
// value ending in `\` used to produce `"...\"` — an unterminated string — from the old
// `"` -> `\"`-only escaper, which never touched `\` at all). Control characters that would
// otherwise break the term across lines (or, inside a VALUES clause, prematurely end the token)
// are escaped too.
var literalEscaper = strings.NewReplacer(
	`\`, `\\`,
	`"`, `\"`,
	"\n", `\n`,
	"\r", `\r`,
	"\t", `\t`,
)

// literalTerm reconstructs a SPARQL-embeddable typed-literal term ("value"^^<datatype>, or a plain
// "value" if datatype is empty) from a SPARQL 1.1 JSON results binding, so a value read back from
// the store can be re-embedded verbatim into a later SPARQL Update's VALUES clause (used to carry
// the candidate's gs:validFrom into the close update's gs:validTo stamp). The lexical value is
// escaped via literalEscaper; a non-empty datatype IRI is validated with assertSafeIRI (the same
// injection class as an unvalidated subject IRI — see assertSafeIRI) and surfaced as an error
// rather than silently embedded or dropped.
func literalTerm(value, datatype string) (string, error) {
	escaped := literalEscaper.Replace(value)
	if datatype == "" {
		return `"` + escaped + `"`, nil
	}
	if err := assertSafeIRI(iri(datatype)); err != nil {
		return "", fmt.Errorf("literal datatype: %w", err)
	}
	return `"` + escaped + `"^^<` + datatype + `>`, nil
}

// disallowedIRIChars are the characters a Turtle/SPARQL IRIREF may never contain unescaped
// (https://www.w3.org/TR/turtle/#grammar-production-IRIREF): <, >, ", {, }, |, ^, `, and \.
// assertSafeIRI additionally rejects every control character and space (0x00-0x20) — the IRIREF
// grammar excludes those as raw bytes too, and no legitimate data:/run: IRI this pipeline mints
// ever contains one.
const disallowedIRIChars = "<>\"{}|^`\\"

// assertSafeIRI rejects id if it contains any character not permitted raw inside a Turtle/SPARQL
// IRIREF. It is the single choke point every iri value must pass before iriValuesList (or any
// other raw `<...>` embedding, e.g. literalTerm's datatype) re-embeds it into SPARQL text: ids
// read back from the store (STR(?s) in signatureQuery) are untrusted input — Jena accepts and
// decodes UCHAR escapes in an inbound Turtle IRIREF, so a stored subject IRI can carry these
// characters even though this writer never mints one that does. This rejects loudly rather than
// sanitizing — matching the package's SHACL-gate philosophy of failing closed on a non-conformant
// candidate instead of best-effort cleanup.
func assertSafeIRI(id iri) error {
	for _, r := range string(id) {
		if r <= 0x20 || strings.ContainsRune(disallowedIRIChars, r) {
			return fmt.Errorf("graph: unsafe IRI %q: contains disallowed character %U", string(id), r)
		}
	}
	return nil
}

// iriValuesList renders ids as a SPARQL VALUES-clause token list ("<iri1> <iri2> ..."), shared by
// every batched query/update in signature.go and write.go that restricts to a specific subject set.
// Precondition: every id must already have passed assertSafeIRI — this function does not
// validate, it only renders. The single validation choke point is (*client).upsert, which checks
// every stagingSigs/liveSigs key (a superset of delta/changed) before any query or update
// referencing them is ever built.
func iriValuesList(ids []iri) string {
	tokens := make([]string, len(ids))
	for i, id := range ids {
		tokens[i] = "<" + string(id) + ">"
	}
	return strings.Join(tokens, " ")
}
