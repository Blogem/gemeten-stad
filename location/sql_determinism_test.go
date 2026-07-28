package location

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// -- LIMIT-1 tie-break guard (defense-in-depth for the determinism regression; see
// determinism_integration_test.go for the real behavioral tie tests) ---------------------------
//
// Each of these resolver queries ends in an `ORDER BY ... LIMIT 1`. Ranking by a non-unique column
// alone (valid_at_date, similarity) lets a genuine tie resolve to an arbitrary row across otherwise
// identical runs; a stable result requires a final, unique tie-break column (the BAG/buurt object's
// own identificatie) in that ORDER BY. This is a cheap static check that the tie-break is present —
// it is not a substitute for the behavioral tie tests, which are the ones that actually catch a
// regression in what gets returned.

// orderByClause returns the `ORDER BY ...` clause of a `LIMIT 1` query (up to the end of the query
// string, since ORDER BY is always the last clause before LIMIT here), or "" if the query has no
// ORDER BY at all.
func orderByClause(query string) string {
	idx := strings.LastIndex(query, "ORDER BY")
	if idx == -1 {
		return ""
	}
	return query[idx:]
}

func TestAddressByPostcodeHuisnummerSQL_OrderByHasIdentificatieTieBreak(t *testing.T) {
	clause := orderByClause(addressByPostcodeHuisnummerSQL)
	assert.Contains(t, clause, "identificatie",
		"ORDER BY must include a unique identificatie tie-break so a tie between two equally-ranked "+
			"BAG objects always resolves to the same row")
}

func TestAddressByStreetExactSQL_OrderByHasIdentificatieTieBreak(t *testing.T) {
	clause := orderByClause(addressByStreetExactSQL)
	assert.Contains(t, clause, "identificatie",
		"ORDER BY must include a unique identificatie tie-break")
}

func TestAddressByStreetFuzzySQL_OrderByHasIdentificatieTieBreak(t *testing.T) {
	clause := orderByClause(addressByStreetFuzzySQL)
	assert.Contains(t, clause, "identificatie",
		"ORDER BY must include a unique identificatie tie-break after similarity/valid_at_date")
}

func TestBuurtPIPSQL_OrderByHasIdentificatieTieBreak(t *testing.T) {
	clause := orderByClause(buurtPIPSQL)
	assert.NotEmpty(t, clause,
		"buurtPIPSQL must have an ORDER BY at all to make its LIMIT 1 deterministic")
	assert.Contains(t, clause, "identificatie",
		"ORDER BY must include a unique buurt identificatie tie-break")
}
