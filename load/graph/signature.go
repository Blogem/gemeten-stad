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
// version — its own <<s,p,o>> already carries a gs:validTo stamp — is excluded entirely: that is
// exactly the "open" filter live extraction (2.3) needs, and a harmless no-op against the staging
// graph, which the writer itself never stamps validTo into (candidates never carry gs:validTo).
const signatureRowPattern = `
    { ?s ?p ?o .
      FILTER(STRSTARTS(STR(?s), "` + dataNS + `"))
      FILTER(?p != gs:validFrom && ?p != gs:validTo)
      FILTER NOT EXISTS { <<?s ?p ?o>> gs:validTo ?closed }
      BIND(CONCAT(STR(?p), " ", STR(?o)) AS ?row)
    }
    UNION
    { <<?s ?ep ?eo>> ?ap ?av .
      FILTER(STRSTARTS(STR(?s), "` + dataNS + `"))
      FILTER(?ap != gs:validFrom && ?ap != gs:validTo)
      FILTER NOT EXISTS { <<?s ?ep ?eo>> gs:validTo ?closedAnn }
      BIND(CONCAT("ann ", STR(?ep), " ", STR(?eo), " ", STR(?ap), " ", STR(?av)) AS ?row)
    }
`

// evolvingFlagPattern marks a subject "evolving" (design.md D4) iff it asserts gs:validFrom either
// directly (a state/period node it owns) or as an RDF-star annotation on one of its own edges (the
// only mechanism this change builds — the node-form Assessment close is a Phase 2 follow-up, per
// design.md's Open Questions).
const evolvingFlagPattern = `
    { ?s gs:validFrom ?vf }
    UNION
    { <<?s ?evp ?evo>> gs:validFrom ?vf2 }
`

// signatureQuery builds the shared per-entity signature SELECT (task 2.2/2.3) over the given named
// graphs (unioned): one row per subject with its order-stable, valid-time-agnostic GROUP_CONCAT
// signature and whether it is evolving. graphIRIs is a single staging graph for 2.2, or every
// current run:load-* graph for 2.3 (D2's "union of run:load-* graphs").
//
// The inner subquery projects (?s, ?row) ordered by (?s, ?row) before the outer GROUP_CONCAT — the
// standard order-stable-GROUP_CONCAT idiom (ORDER BY inside, aggregate outside) — so two runs over
// identical content always produce byte-identical signature strings, regardless of the store's
// internal row order. The evolving flag joins in via an OPTIONAL sub-SELECT so subjects with no
// gs:validFrom at all still appear (immutable entities), with ?evolving left unbound (-> false).
func signatureQuery(graphIRIs []string) string {
	rowBlocks := make([]string, len(graphIRIs))
	evolvingBlocks := make([]string, len(graphIRIs))
	for i, g := range graphIRIs {
		rowBlocks[i] = "GRAPH <" + g + "> {" + signatureRowPattern + "}"
		evolvingBlocks[i] = "GRAPH <" + g + "> {" + evolvingFlagPattern + "}"
	}

	return fmt.Sprintf(`
PREFIX gs: <%s>
SELECT ?s (GROUP_CONCAT(?row; separator="%s") AS ?sig) (SAMPLE(?evolvingFlag) AS ?evolving)
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
// validFrom, it only reads back what the caller already asserted).
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
		validFrom[iri(b.S.Value)] = literalTerm(b.Vf.Value, b.Vf.Datatype)
	}
	return validFrom, nil
}

// literalTerm reconstructs a SPARQL-embeddable typed-literal term ("value"^^<datatype>, or a plain
// "value" if datatype is empty) from a SPARQL 1.1 JSON results binding, so a value read back from
// the store can be re-embedded verbatim into a later SPARQL Update's VALUES clause (used to carry
// the candidate's gs:validFrom into the close update's gs:validTo stamp).
func literalTerm(value, datatype string) string {
	escaped := strings.ReplaceAll(value, `"`, `\"`)
	if datatype == "" {
		return `"` + escaped + `"`
	}
	return `"` + escaped + `"^^<` + datatype + `>`
}

// iriValuesList renders ids as a SPARQL VALUES-clause token list ("<iri1> <iri2> ..."), shared by
// every batched query/update in signature.go and write.go that restricts to a specific subject set.
func iriValuesList(ids []iri) string {
	tokens := make([]string, len(ids))
	for i, id := range ids {
		tokens[i] = "<" + string(id) + ">"
	}
	return strings.Join(tokens, " ")
}
