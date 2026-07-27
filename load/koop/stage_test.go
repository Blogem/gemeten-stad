package koop

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file exercises publicationArgs against the DOCUMENTED (fixed) stage.go contract: a
// malformed Available date must fail publicationArgs with a non-nil error — the trigger
// stagePublications uses to skip (log + continue) an unstageable row rather than abort the whole
// batch. stage.go's implementation of that skip loop is written by a concurrent coder agent and is
// not visible here; publicationArgs itself is pure (no DB), so it is unit-testable directly.

// TestPublicationArgs_MalformedAvailableDateErrors covers the stage-skip trigger: a row whose
// Available is a non-empty, non-date string must make publicationArgs return a non-nil error, so
// stagePublications can skip it instead of failing the batch insert with a SQL type error.
func TestPublicationArgs_MalformedAvailableDateErrors(t *testing.T) {
	row := PublicationRow{Pub: Publication{ID: "gmb-x", Kind: KindBesluit, Available: "not-a-date"}}

	_, err := publicationArgs(row)

	require.Error(t, err, "a malformed Available date must fail publicationArgs so stagePublications can skip the row")
}

// TestPublicationArgs_WellFormedRowReturnsArgsNoError is the positive counterpart: a well-formed
// row (valid "YYYY-MM-DD" Available, no resolution) must build its 17 bind values with no error,
// and the Available value must bind as a parsed time.Time, not the raw string.
func TestPublicationArgs_WellFormedRowReturnsArgsNoError(t *testing.T) {
	row := PublicationRow{
		Pub: Publication{ID: "gmb-2022-245014", Kind: KindBesluit, Available: "2022-05-31"},
		Res: nil,
	}

	args, err := publicationArgs(row)

	require.NoError(t, err)
	require.Len(t, args, 17, "publicationArgs must return exactly the 17 bind values publicationInsertSQL expects")
	assert.Equal(t, "gmb-2022-245014", args[0], "gmb_id is the first bind value")

	available, ok := args[3].(time.Time)
	require.True(t, ok, "a well-formed Available date must bind as a parsed time.Time, not the raw string")
	assert.Equal(t, time.Date(2022, time.May, 31, 0, 0, 0, 0, time.UTC), available)
}
