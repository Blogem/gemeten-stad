package bomen

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// -- 3. mergeSQL ----------------------------------------------------------------

// TestMergeSQL_FixtureSpec exercises mergeSQL against a hand-built upsertSpec, independent of
// the real upsertSpecs table, so the templating itself is pinned regardless of exactly which
// columns bomen's real specs end up with.
func TestMergeSQL_FixtureSpec(t *testing.T) {
	spec := upsertSpec{
		target: "bomen_test_target",
		keys:   []string{"id"},
		cols: []column{
			{name: "id", cast: ""},
			{name: "soortnaam", cast: ""},
			{name: "geom", cast: "geometry"},
		},
	}

	sql := mergeSQL(spec)
	normalized := strings.Join(strings.Fields(sql), " ")

	assert.Contains(t, normalized, "MERGE INTO bomen_test_target")
	assert.Contains(t, normalized, `t."id" = s."id"`, "must be keyed on id (identifiers are quoted)")
	assert.Contains(t, normalized, "WHEN MATCHED AND t.raw IS DISTINCT FROM s.raw", "changed rows must be refreshed")
	assert.Contains(t, normalized, "WHEN MATCHED AND t.source_deleted_at IS NOT NULL", "a reappearing soft-deleted row must be un-soft-deleted")
	assert.Contains(t, normalized, "WHEN NOT MATCHED THEN")
	assert.Contains(t, normalized, "WHEN NOT MATCHED BY SOURCE")
	assert.Contains(t, normalized, "source_deleted_at")
	assert.Contains(t, normalized, "s.geom::geometry", "a column with a cast must be cast on the way in from staging")
}

// TestMergeSQL_UpsertSpecs asserts every pinned upsertSpecs entry produces a MERGE statement
// satisfying the same contract: keyed on id, refreshing changed rows, un-soft-deleting rows
// that reappear, inserting new rows, and soft-deleting rows that disappear from staging via
// source_deleted_at.
func TestMergeSQL_UpsertSpecs(t *testing.T) {
	require.NotEmpty(t, upsertSpecs, "upsertSpecs must describe at least one target table")

	for _, spec := range upsertSpecs {
		t.Run(spec.target, func(t *testing.T) {
			sql := mergeSQL(spec)
			normalized := strings.Join(strings.Fields(sql), " ")

			assert.Contains(t, spec.keys, "id", "bomen upsert specs are keyed on the resolved id")
			assert.Contains(t, normalized, "MERGE")
			assert.Contains(t, normalized, spec.target)
			assert.Contains(t, normalized, `t."id" = s."id"`, "must be keyed on id (identifiers are quoted)")
			assert.Contains(t, normalized, "WHEN MATCHED AND t.raw IS DISTINCT FROM s.raw", "changed rows must be refreshed")
			assert.Contains(t, normalized, "WHEN MATCHED AND t.source_deleted_at IS NOT NULL", "a reappearing soft-deleted row must be un-soft-deleted")
			assert.Contains(t, normalized, "WHEN NOT MATCHED")
			assert.Contains(t, normalized, "WHEN NOT MATCHED BY SOURCE")
			assert.Contains(t, normalized, "source_deleted_at")
		})
	}
}

// -- 4. resolvePoint --------------------------------------------------------------

// TestResolvePoint covers the boomId -> boomNieuwId -> unresolved fallback chain
// (DATA_SOURCES.md §2a): boomId resolves ~71% of felled rows on its own; boomNieuwId recovers
// most of the rest (replanted trees whose original boomId retires from stamgegevens).
func TestResolvePoint(t *testing.T) {
	stamgegevensIDs := map[string]struct{}{
		"boom-1": {},
		"boom-2": {},
	}

	tests := []struct {
		name         string
		boomID       string
		boomNieuwID  string
		wantResolved string
		wantVia      string
	}{
		{
			name:         "boomId present in stamgegevens",
			boomID:       "boom-1",
			boomNieuwID:  "",
			wantResolved: "boom-1",
			wantVia:      ResolvedViaBoomID,
		},
		{
			name:         "boomId takes priority over boomNieuwId when both present",
			boomID:       "boom-1",
			boomNieuwID:  "boom-2",
			wantResolved: "boom-1",
			wantVia:      ResolvedViaBoomID,
		},
		{
			name:         "boomId absent, boomNieuwId present falls back",
			boomID:       "retired-id",
			boomNieuwID:  "boom-2",
			wantResolved: "boom-2",
			wantVia:      ResolvedViaBoomNieuwID,
		},
		{
			name:         "neither boomId nor boomNieuwId present",
			boomID:       "retired-id",
			boomNieuwID:  "also-missing",
			wantResolved: "",
			wantVia:      ResolvedViaUnresolved,
		},
		{
			name:         "empty inputs are unresolved",
			boomID:       "",
			boomNieuwID:  "",
			wantResolved: "",
			wantVia:      ResolvedViaUnresolved,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotVia := resolvePoint(tt.boomID, tt.boomNieuwID, stamgegevensIDs)
			assert.Equal(t, tt.wantResolved, gotID)
			assert.Equal(t, tt.wantVia, gotVia)
		})
	}
}

// -- 5. readLandedRows --------------------------------------------------------------

// Note: readLandedRows takes the artifact name as a plain string, so this test lands under a
// literal name matching ingest/bomen's pinned ArtifactStamgegevens ("bomen_stamgegevens") value
// rather than importing ingest/bomen, avoiding a needless cross-package dependency for a test
// fixture.
const testStamgegevensArtifact = "bomen_stamgegevens"

// TestReadLandedRows_AcrossPages covers the happy path: a versioned artifact landed as two
// JSONL lines (one per fetched page, each shaped like the pinned page bodies) must decode into
// every row across both pages, in order.
func TestReadLandedRows_AcrossPages(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	page1 := `{"_embedded":{"stamgegevens":[{"id":"1"},{"id":"2"}]}}`
	page2 := `{"_embedded":{"stamgegevens":[{"id":"3"}]}}`
	jsonl := page1 + "\n" + page2 + "\n"

	_, err := store.LandVersion(testStamgegevensArtifact, strings.NewReader(jsonl), "https://example.com/stamgegevens", time.Now())
	require.NoError(t, err)

	rows, err := readLandedRows(store, testStamgegevensArtifact, "stamgegevens")
	require.NoError(t, err)
	require.Len(t, rows, 3, "expected all rows across both landed pages")

	var ids []string
	for _, row := range rows {
		id, _ := row["id"].(string)
		ids = append(ids, id)
	}
	assert.Equal(t, []string{"1", "2", "3"}, ids, "rows must come back in landed page order")
}

// TestReadLandedRows_AbsentArtifact covers the not-yet-landed case: the pinned behavior is
// either an error or an empty result, but never a panic.
func TestReadLandedRows_AbsentArtifact(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	require.NotPanics(t, func() {
		rows, err := readLandedRows(store, "never-landed-artifact", "stamgegevens")
		if err == nil {
			assert.Empty(t, rows, "an absent artifact with no error must yield no rows")
		}
	})
}
