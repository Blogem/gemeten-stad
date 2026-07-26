package koop

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -- BuildQuery ---------------------------------------------------------------
//
// Covers spec scenario "The query builder assembles the scoped Amsterdam
// kap/verplant query" and design D1: the query must carry exactly the four
// scoped clauses (dt.creator, dt.type, the kap/verplant full-text term set,
// dt.available>=) and must NOT carry any stadsdeel/Noord, geometry, or
// postcode filter -- the harvest is deliberately Amsterdam-wide; Noord
// selection is deferred to P13 (proposal's settled decision).

func TestBuildQuery_IncludesScopedClauses(t *testing.T) {
	got := BuildQuery("2021-01-01")

	require.NotEmpty(t, got, "BuildQuery must not return an empty query")

	assert.Contains(t, got, `dt.creator any "Amsterdam"`,
		"the query must scope to the publishing authority Amsterdam via an `any` relation, not the sparse w.gemeentenaam field")
	assert.Contains(t, got, `dt.type any "omgevingsvergunning"`,
		"the query must scope to the omgevingsvergunning document type via an `any` relation")
	assert.Contains(t, got, `cql.textAndIndexes any "kappen vellen rooien verplanten houtopstand"`,
		"the query must carry the full kap/verplant synonym term set (kappen/vellen/rooien/verplanten/houtopstand) as a single full-text clause")
	assert.Contains(t, got, `dt.available>="2021-01-01"`,
		"the query must carry the publication-date lower bound")
}

func TestBuildQuery_ExcludesOutOfScopeFilters(t *testing.T) {
	got := BuildQuery("2021-01-01")

	lower := strings.ToLower(got)
	for _, forbidden := range []string{"noord", "stadsdeel", "point", "geometrie", "geometry", "locatie", "postcode", "w.postcode"} {
		assert.NotContains(t, lower, strings.ToLower(forbidden),
			"the harvest is Amsterdam-wide: the query must not carry a %q filter (Noord/geometry/postcode selection is deferred to P13)", forbidden)
	}
}

// TestBuildQuery_ParameterizesLowerBound guards against a hardcoded
// "2021-01-01" literal: the incremental cursor (D3) re-queries with an
// advancing lower bound on every run, so BuildQuery must actually use its
// sinceDate argument rather than always emitting the default.
func TestBuildQuery_ParameterizesLowerBound(t *testing.T) {
	tests := []struct {
		name      string
		sinceDate string
	}{
		{name: "the default 2021-01-01 lower bound", sinceDate: "2021-01-01"},
		{name: "an advanced cursor lower bound", sinceDate: "2022-06-01"},
		{name: "a lower bound crossing a year boundary", sinceDate: "2021-12-27"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildQuery(tt.sinceDate)
			assert.Contains(t, got, `dt.available>="`+tt.sinceDate+`"`,
				"BuildQuery(%q) must carry that exact lower bound, not a fixed literal", tt.sinceDate)
		})
	}

	// Two different sinceDates must produce two different queries (proves the
	// argument is actually threaded through, not ignored).
	assert.NotEqual(t, BuildQuery("2021-01-01"), BuildQuery("2022-06-01"),
		"BuildQuery must produce a different query string for a different sinceDate")
}

// -- Pinned constants -----------------------------------------------------------

func TestPinnedConstants(t *testing.T) {
	assert.Equal(t, "https://repository.overheid.nl/sru", SRUEndpoint)
	assert.Equal(t, "2021-01-01", DefaultSinceDate)
	assert.Equal(t, 7, OverlapDays)
	assert.Equal(t, "koop/_cursor.json", CursorArtifact)
}

// -- queryLowerBound ------------------------------------------------------------
//
// Covers design D3 (overlap window): queryLowerBound(hwm) must subtract
// exactly OverlapDays (7) calendar days from the stored high-water mark,
// including across month/year boundaries.

func TestQueryLowerBound_SubtractsOverlapDays(t *testing.T) {
	tests := []struct {
		name string
		hwm  string
		want string
	}{
		{name: "plain same-month subtraction", hwm: "2022-01-12", want: "2022-01-05"},
		{name: "month boundary", hwm: "2022-03-03", want: "2022-02-24"},
		{name: "year boundary", hwm: "2022-01-03", want: "2021-12-27"},
		{name: "leap-year February boundary", hwm: "2024-03-04", want: "2024-02-26"},
		{name: "the fixture-driven high-water mark used in the cursor tests", hwm: "2022-01-20", want: "2022-01-13"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := queryLowerBound(tt.hwm)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestQueryLowerBound_RejectsMalformedDate(t *testing.T) {
	tests := []string{"", "not-a-date", "2022/01/12", "2022-13-01"}
	for _, hwm := range tests {
		t.Run(hwm, func(t *testing.T) {
			_, err := queryLowerBound(hwm)
			assert.Error(t, err, "queryLowerBound(%q) must reject a malformed date rather than silently miscomputing", hwm)
		})
	}
}
