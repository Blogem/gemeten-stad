package graph

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Blogem/gemeten-stad/ontology"
)

// conformsPattern matches sh:conforms true|false, tolerant of Fuseki's own two-space Turtle
// pretty-printing ("sh:conforms  true") and a plain single space alike.
var conformsPattern = regexp.MustCompile(`sh:conforms\s+(true|false)\b`)

// resultBlockPattern splits a SHACL validation report into its `sh:result [ ... ]` blocks — each
// one sh:ValidationResult. It intentionally requires "[" right after "sh:result" so it does not
// also match sh:resultMessage/sh:resultPath/sh:resultSeverity (those are followed by a literal or
// an IRI, never "[").
var resultBlockPattern = regexp.MustCompile(`sh:result\s*\[`)

var focusNodePattern = regexp.MustCompile(`sh:focusNode\s+([^\s;.]+)`)

var resultMessagePattern = regexp.MustCompile(`sh:resultMessage\s+"([^"]*)"`)

// parseConforms parses a Turtle SHACL validation report (as returned by Fuseki's /shacl endpoint)
// and returns whether it conforms, plus — for a non-conforming report — a detail string built from
// each violation's focus node and result message, for the load gate's loud error. It is a pure
// function (no I/O), table-driven-testable against representative report payloads. A malformed or
// empty report (no parseable sh:conforms at all) is itself a returned error, never silently
// treated as conforming or non-conforming.
func parseConforms(report []byte) (conforms bool, detail string, err error) {
	if len(bytes.TrimSpace(report)) == 0 {
		return false, "", fmt.Errorf("graph: parse SHACL report: empty report")
	}

	m := conformsPattern.FindSubmatch(report)
	if m == nil {
		return false, "", fmt.Errorf("graph: parse SHACL report: no sh:conforms found in report: %s", reportSnippet(report))
	}
	if string(m[1]) == "true" {
		return true, "", nil
	}

	blocks := resultBlockPattern.Split(string(report), -1)
	var violations []string
	for _, block := range blocks[1:] { // blocks[0] is the preamble up to the first sh:result [
		focus := "?"
		if fm := focusNodePattern.FindStringSubmatch(block); fm != nil {
			focus = fm[1]
		}
		message := ""
		if mm := resultMessagePattern.FindStringSubmatch(block); mm != nil {
			message = mm[1]
		}
		violations = append(violations, fmt.Sprintf("focusNode=%s: %s", focus, message))
	}
	if len(violations) == 0 {
		return false, "", fmt.Errorf("graph: parse SHACL report: sh:conforms false but no sh:result blocks found: %s", reportSnippet(report))
	}
	return false, strings.Join(violations, "; "), nil
}

// reportSnippet bounds how much of a malformed report an error message quotes.
func reportSnippet(report []byte) string {
	const max = 512
	if len(report) > max {
		return string(report[:max]) + "..."
	}
	return string(report)
}

// validate merges the embedded reference model (ontology.Ontology + ontology.Vocab) and candidate
// into the transient scratchGraph, validates that graph against ontology.Shapes via the Fuseki
// /shacl endpoint, and always drops scratchGraph afterward (see doc.go for why the merge is
// required — cross-graph SHACL validation cannot see concepts living in a different graph).
func (c *client) validate(ctx context.Context, candidate []byte, scratchGraph string) (conforms bool, detail string, err error) {
	defer func() {
		if dropErr := c.dropGraph(ctx, scratchGraph); dropErr != nil && err == nil {
			err = fmt.Errorf("graph: validate: clean up scratch graph %s: %w", scratchGraph, dropErr)
		}
	}()

	if err := c.putGraph(ctx, scratchGraph, ontology.Ontology); err != nil {
		return false, "", fmt.Errorf("graph: validate: load ontology into scratch graph: %w", err)
	}
	if err := c.postGraph(ctx, scratchGraph, ontology.Vocab); err != nil {
		return false, "", fmt.Errorf("graph: validate: load vocab into scratch graph: %w", err)
	}
	if len(candidate) > 0 {
		if err := c.postGraph(ctx, scratchGraph, candidate); err != nil {
			return false, "", fmt.Errorf("graph: validate: load candidate into scratch graph: %w", err)
		}
	}

	report, err := c.shaclValidate(ctx, scratchGraph, ontology.Shapes)
	if err != nil {
		return false, "", fmt.Errorf("graph: validate: SHACL request: %w", err)
	}

	return parseConforms(report)
}
