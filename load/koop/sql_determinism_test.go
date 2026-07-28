package koop

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// -- LIMIT-1 tie-break guard (defense-in-depth for the determinism regression; see
// determinism_integration_test.go for the real behavioral tie tests) ---------------------------
//
// buurtByPointSQL and buurtByRDPointSQL each end in a bare `ST_Contains(...) LIMIT 1` with no
// ORDER BY at all — so a point inside two overlapping gebieden_buurten polygons can resolve to
// either one, non-deterministically across otherwise-identical `load koop` runs. A stable result
// requires an ORDER BY on a unique tie-break column (the buurt's own identificatie). This is a
// cheap static check that the tie-break is present — it is not a substitute for the behavioral tie
// tests, which are the ones that actually catch a regression in what gets returned.

// orderByClause returns the `ORDER BY ...` clause of a `LIMIT 1` query, or "" if the query has no
// ORDER BY at all.
func orderByClause(query string) string {
	idx := strings.LastIndex(query, "ORDER BY")
	if idx == -1 {
		return ""
	}
	return query[idx:]
}

func TestBuurtByPointSQL_OrderByHasIdentificatieTieBreak(t *testing.T) {
	clause := orderByClause(buurtByPointSQL)
	assert.NotEmpty(t, clause,
		"buurtByPointSQL must have an ORDER BY at all to make its LIMIT 1 deterministic")
	assert.Contains(t, clause, "identificatie",
		"ORDER BY must include a unique buurt identificatie tie-break")
}

func TestBuurtByRDPointSQL_OrderByHasIdentificatieTieBreak(t *testing.T) {
	clause := orderByClause(buurtByRDPointSQL)
	assert.NotEmpty(t, clause,
		"buurtByRDPointSQL must have an ORDER BY at all to make its LIMIT 1 deterministic")
	assert.Contains(t, clause, "identificatie",
		"ORDER BY must include a unique buurt identificatie tie-break")
}
