package bomen

import (
	"encoding/json"
	"regexp"
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

	sql := mergeSQL("s", spec)
	normalized := strings.Join(strings.Fields(sql), " ")

	assert.Contains(t, normalized, `MERGE INTO "s"."bomen_test_target"`, "target table must be schema-qualified")
	assert.Contains(t, normalized, `t."id" = s."id"`, "must be keyed on id (identifiers are quoted)")
	assert.Contains(t, normalized, "WHEN MATCHED AND t.raw IS DISTINCT FROM s.raw", "changed rows must be refreshed")
	assert.Contains(t, normalized, "WHEN MATCHED AND t.source_deleted_at IS NOT NULL", "a reappearing soft-deleted row must be un-soft-deleted")
	assert.Contains(t, normalized, "WHEN NOT MATCHED THEN")
	assert.Contains(t, normalized, "WHEN NOT MATCHED BY SOURCE")
	assert.Contains(t, normalized, "source_deleted_at")
	assert.Contains(t, normalized, `s."geom"::geometry`, "a column with a cast must be cast on the way in from staging")
}

// TestMergeSQL_UpsertSpecs asserts every pinned upsertSpecs entry produces a MERGE statement
// satisfying the same contract: keyed on id, refreshing changed rows, un-soft-deleting rows
// that reappear, inserting new rows, and soft-deleting rows that disappear from staging via
// source_deleted_at.
func TestMergeSQL_UpsertSpecs(t *testing.T) {
	require.NotEmpty(t, upsertSpecs, "upsertSpecs must describe at least one target table")

	for _, spec := range upsertSpecs {
		t.Run(spec.target, func(t *testing.T) {
			sql := mergeSQL("s", spec)
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
// fixture (contract_test.go is the one test file that imports ingest/bomen, specifically to guard
// this literal against drifting from the real value).
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

// TestReadLandedRows_FeatureCollection covers the geojson auto-detect path: a versioned artifact
// landed as a single GeoJSON FeatureCollection JSONL line (stamgegevens' new DSO geojson-export
// ingest shape) must decode into one row per feature, each carrying its `properties` fields plus
// a `geometrie` key set to the feature's own GeoJSON `geometry` object — proving readLandedRows
// auto-detects this shape independently of the `_embedded` page shape covered above.
func TestReadLandedRows_FeatureCollection(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	fc := `{"type":"FeatureCollection","features":[` +
		`{"type":"Feature","id":"stam-1","geometry":{"type":"Point","coordinates":[4.895,52.370]},"properties":{"id":"stam-1","gbdBuurtId":"A01","soortnaam":"Tilia"}},` +
		`{"type":"Feature","id":"stam-2","geometry":{"type":"Point","coordinates":[4.900,52.375]},"properties":{"id":"stam-2","gbdBuurtId":"A02","soortnaam":"Quercus"}}` +
		`]}` + "\n"

	_, err := store.LandVersion(testStamgegevensArtifact, strings.NewReader(fc), "https://example.com/stamgegevens?_format=geojson", time.Now())
	require.NoError(t, err)

	rows, err := readLandedRows(store, testStamgegevensArtifact, "stamgegevens")
	require.NoError(t, err)
	require.Len(t, rows, 2, "one row per feature")

	assert.Equal(t, "stam-1", rows[0]["id"])
	assert.Equal(t, "A01", rows[0]["gbdBuurtId"])
	assert.Equal(t, "Tilia", rows[0]["soortnaam"])
	geom, ok := rows[0]["geometrie"].(map[string]any)
	require.True(t, ok, "geometrie must be the feature's geometry object")
	assert.Equal(t, "Point", geom["type"])
	coords, ok := geom["coordinates"].([]any)
	require.True(t, ok)
	require.Len(t, coords, 2)
	assert.InDelta(t, 4.895, coords[0], 1e-9)
	assert.InDelta(t, 52.370, coords[1], 1e-9)

	assert.Equal(t, "stam-2", rows[1]["id"])
	assert.Equal(t, "A02", rows[1]["gbdBuurtId"])
	assert.Equal(t, "Quercus", rows[1]["soortnaam"])
}

// -- 6. geoJSONText null handling -----------------------------------------------

// TestGeoJSONText_NullGeometry guards the fix for the live full-corpus failure: ~24 of the ~324k
// stamgegevens features have a null geometry, which decodes into a typed-nil map[string]any. Stored
// in an any-typed row value it is a NON-nil interface, so a bare `v == nil` check misses it and
// json.Marshal produces "null" — which ST_GeomFromGeoJSON rejects ("invalid GeoJSON
// representation"). geoJSONText must map null/empty geometry to SQL NULL, not the string "null".
func TestGeoJSONText_NullGeometry(t *testing.T) {
	valid := map[string]any{"type": "Point", "coordinates": []any{4.9, 52.3}}

	tests := []struct {
		name    string
		in      any
		wantNil bool
	}{
		{"valid point marshals to json text", valid, false},
		{"untyped nil is NULL", nil, true},
		{"typed-nil map is NULL (the live bug)", map[string]any(nil), true},
		{"empty object is NULL", map[string]any{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := geoJSONText(tt.in)
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, got, "null/empty geometry must bind SQL NULL, never the string \"null\"/\"{}\"")
			} else {
				assert.Equal(t, `{"coordinates":[4.9,52.3],"type":"Point"}`, got)
			}
		})
	}
}

// -- 7. rowString numeric-id regression -----------------------------------------
//
// Regression: a landed source `id`/`boomId` field is a JSON *number*
// (docs/DATA_SOURCES.md §2a — the Datapunt bomen DSO API emits these unquoted), so
// readLandedRows's json.Decoder must decode it as json.Number (exact decimal text), never the
// default float64 — a large tree id like 4301189 loses precision-formatting through float64 and
// fmt.Sprintf("%v", ...) renders it as "4.301428e+06" instead of the exact integer string
// "4301189". rowString is the last step before that value binds to a text staging column, so it
// is the sharpest place to pin the fix.

// TestRowString covers rowString's full contract per field-type: a json.Number renders as its
// exact decimal text (never scientific notation), an empty json.Number is SQL NULL (same as an
// empty string), a plain string passes through verbatim, and a missing/nil key is SQL NULL.
func TestRowString(t *testing.T) {
	tests := []struct {
		name string
		row  map[string]any
		key  string
		want any
	}{
		{
			name: "json.Number large integer renders exact, never scientific notation",
			row:  map[string]any{"id": json.Number("4301189")},
			key:  "id",
			want: "4301189",
		},
		{
			name: "json.Number another large integer (boomId) renders exact",
			row:  map[string]any{"boomId": json.Number("1014499")},
			key:  "boomId",
			want: "1014499",
		},
		{
			name: "json.Number small integer renders exact",
			row:  map[string]any{"boomNieuwId": json.Number("2041042")},
			key:  "boomNieuwId",
			want: "2041042",
		},
		{
			name: "empty json.Number is SQL NULL, same as an empty string",
			row:  map[string]any{"id": json.Number("")},
			key:  "id",
			want: nil,
		},
		{
			name: "plain string field passes through verbatim",
			row:  map[string]any{"soortnaam": "Tilia"},
			key:  "soortnaam",
			want: "Tilia",
		},
		{
			name: "empty string field is SQL NULL",
			row:  map[string]any{"soortnaam": ""},
			key:  "soortnaam",
			want: nil,
		},
		{
			name: "missing key is SQL NULL",
			row:  map[string]any{"other": "x"},
			key:  "id",
			want: nil,
		},
		{
			name: "nil value is SQL NULL",
			row:  map[string]any{"id": nil},
			key:  "id",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rowString(tt.row, tt.key)
			assert.Equal(t, tt.want, got)
			if s, ok := got.(string); ok {
				assert.NotContains(t, s, "e+", "a staged text value must never render in scientific notation")
			}
		})
	}
}

// TestRowString_NeverScientificNotation is a dedicated guard, pinned directly to the reported
// live bug (a landed id rendered as "4.301428e+06"): feed rowString several json.Number values in
// the magnitude range that triggers Go's default float64 %v scientific-notation formatting, and
// assert every result is a plain, all-digit string.
func TestRowString_NeverScientificNotation(t *testing.T) {
	plainInteger := regexp.MustCompile(`^[0-9]+$`)
	ids := []json.Number{"4301189", "2041042", "1014499", "35202", "323728", "10000000"}

	for _, id := range ids {
		row := map[string]any{"id": id}
		got := rowString(row, "id")
		s, ok := got.(string)
		require.Truef(t, ok, "rowString must return a string for non-empty json.Number %v, got %T", id, got)
		assert.Regexpf(t, plainInteger, s, "id %v must stage as a plain integer string, got %q", id, s)
		assert.NotContains(t, s, "e+", "id %v must never render in scientific notation", id)
		assert.Equal(t, string(id), s, "rowString must preserve the source's exact decimal text")
	}
}

// -- 8. readLandedRows numeric-id decode regression -----------------------------
//
// numericString asserts v is either a json.Number or a string carrying the exact decimal text
// want, and fails loudly if it decoded as a float64 instead — the live bug this whole regression
// guards against (a JSON number id silently coerced to float64 during landed-row decode, which
// then loses exact-integer formatting downstream in rowString).
func numericString(t *testing.T, v any, want string) {
	t.Helper()
	switch n := v.(type) {
	case json.Number:
		assert.Equal(t, want, n.String(), "json.Number must carry the exact decimal text")
	case string:
		assert.Equal(t, want, n, "string-decoded id must carry the exact decimal text")
	case float64:
		t.Fatalf("landed id/boomId decoded as float64 (%v) instead of json.Number/string — this is exactly the scientific-notation bug (want %q)", n, want)
	default:
		t.Fatalf("landed id/boomId has unexpected type %T: %v (want %q)", v, v, want)
	}
}

// TestReadLandedRows_KapenherplantNumericIDsDecodeExact covers the HAL _embedded page path
// (kapenherplant): a landed page whose id/boomId/boomNieuwId are JSON *numbers* (not quoted
// strings) — exactly the Datapunt bomen DSO API's own shape — must decode into rows whose
// id/boomId/boomNieuwId carry the exact decimal text, never a float64-coerced value that would
// later render as scientific notation.
func TestReadLandedRows_KapenherplantNumericIDsDecodeExact(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	body := `{"_embedded":{"kapenherplant":[{"id":4301189,"boomId":1014499,"boomNieuwId":2041042}]}}` + "\n"
	_, err := store.LandVersion(artifactKapenherplant, strings.NewReader(body), "https://example.com/kapenherplant", time.Now())
	require.NoError(t, err)

	rows, err := readLandedRows(store, artifactKapenherplant, embedKeyKapenherplant)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	numericString(t, rows[0]["id"], "4301189")
	numericString(t, rows[0]["boomId"], "1014499")
	numericString(t, rows[0]["boomNieuwId"], "2041042")
}

// TestReadLandedRows_StamgegevensGeoJSONNumericID mirrors the kapenherplant case for the GeoJSON
// FeatureCollection path (stamgegevens): a feature whose properties.id is a JSON number must
// decode into a row whose "id" carries the exact decimal text.
func TestReadLandedRows_StamgegevensGeoJSONNumericID(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	fc := `{"type":"FeatureCollection","features":[` +
		`{"type":"Feature","id":4301189,"geometry":{"type":"Point","coordinates":[4.895,52.370]},"properties":{"id":4301189,"gbdBuurtId":"A01","soortnaam":"Tilia"}}` +
		`]}` + "\n"
	_, err := store.LandVersion(testStamgegevensArtifact, strings.NewReader(fc), "https://example.com/stamgegevens?_format=geojson", time.Now())
	require.NoError(t, err)

	rows, err := readLandedRows(store, testStamgegevensArtifact, "stamgegevens")
	require.NoError(t, err)
	require.Len(t, rows, 1)

	numericString(t, rows[0]["id"], "4301189")
}
