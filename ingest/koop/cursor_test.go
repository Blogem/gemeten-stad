package koop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// cursorFile is the small JSON struct persisted at CursorArtifact
// ("koop/_cursor.json"), per the pinned surface's documented shape
// {"high_water_mark":"YYYY-MM-DD"}. It is a plain file, NOT landed via
// shared.RawStore.Land, so it carries no .prov.jsonl sidecar.
type cursorFile struct {
	HighWaterMark string `json:"high_water_mark"`
}

// readCursor reads and parses the cursor sidecar directly off disk at
// <store.BasePath>/koop/_cursor.json.
func readCursor(t *testing.T, store *shared.RawStore) cursorFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(store.BasePath, CursorArtifact))
	require.NoError(t, err, "cursor artifact must exist after at least one Ingest run")
	var cf cursorFile
	require.NoError(t, json.Unmarshal(data, &cf))
	return cf
}

// -- First run: full window, high-water mark set to the max dt.available ------
//
// Covers spec scenario "First run on a clean volume harvests the full window"
// and design D3 step 4 (persist the new high-water mark = max dt.available
// seen this run).

func TestIngest_FirstRun_LandsAllAndSetsHighWaterMark(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	fixture := readFixture(t, "first_run.xml")
	var calls []string
	httpGet := fakeSRUGetter(t, &calls, map[string][]byte{"1": fixture})

	err := Ingest(context.Background(), store, httpGet)
	require.NoError(t, err)

	for _, id := range []string{"gmb-2022-100001", "gmb-2022-100002", "gmb-2022-100003"} {
		landed, err := store.Landed("koop/" + id + ".xml")
		require.NoError(t, err)
		assert.True(t, landed, "publication %s must be landed on the first run", id)
	}

	require.NotEmpty(t, calls, "Ingest must issue at least one SRU request")
	assert.Contains(t, queryParam(t, calls[0]), `dt.available>="`+DefaultSinceDate+`"`,
		"the first run on a clean volume must query from the default 2021-01-01 lower bound")

	cursor := readCursor(t, store)
	assert.Equal(t, "2022-01-12", cursor.HighWaterMark,
		"the high-water mark must equal the MAX dt.available landed this run, not the last record processed or the query date")
}

// -- Second run: overlap re-query --------------------------------------------
//
// Covers design D3 step 2: each run queries dt.available >= (highWater -
// overlap). This proves the *sent* query's lower bound on the second run is
// exactly HWM-7d, not the raw stored HWM (which would risk missing
// back-dated same-period publications) and not the original default.

func TestIngest_SecondRun_QueriesFromHighWaterMarkMinusOverlap(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	fixture := readFixture(t, "first_run.xml")
	var calls []string
	httpGet := fakeSRUGetter(t, &calls, map[string][]byte{"1": fixture})

	require.NoError(t, Ingest(context.Background(), store, httpGet))
	require.NoError(t, Ingest(context.Background(), store, httpGet))

	require.Len(t, calls, 2, "each run over a result set smaller than one page must issue exactly one SRU request")

	firstQuery := queryParam(t, calls[0])
	secondQuery := queryParam(t, calls[1])

	assert.Contains(t, firstQuery, `dt.available>="2021-01-01"`,
		"the first run must query from the default lower bound")
	assert.Contains(t, secondQuery, `dt.available>="2022-01-05"`,
		"the second run must query from the stored high-water mark (2022-01-12) minus the 7-day overlap, i.e. 2022-01-05")
	assert.NotEqual(t, firstQuery, secondQuery,
		"the second run's query must actually advance past the first run's lower bound")
}

// -- Second run: id-dedup skip + only-new landing ----------------------------
//
// Covers spec scenario "Re-run lands only new publications": already-landed
// ids reappearing inside the overlap window are skipped (no rewrite, no new
// provenance line); only the genuinely new publication is landed and the
// cursor advances to its dt.available.

func TestIngest_SecondRun_LandsOnlyNewPublicationsAndSkipsAlreadyLanded(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	var calls []string
	httpGet := fakeSRUGetter(t, &calls, map[string][]byte{
		"1": readFixture(t, "first_run.xml"),
	})
	require.NoError(t, Ingest(context.Background(), store, httpGet))

	preExisting := []string{"koop/gmb-2022-100001.xml", "koop/gmb-2022-100002.xml", "koop/gmb-2022-100003.xml"}
	provBefore := make(map[string]int, len(preExisting))
	for _, name := range preExisting {
		provBefore[name] = provenanceLineCount(t, store, name)
		require.Equal(t, 1, provBefore[name], "sanity: each publication must have exactly one provenance line after the first run")
	}

	// Second run: same 3 already-landed ids served again (inside the overlap
	// window) PLUS one genuinely new publication.
	httpGet = fakeSRUGetter(t, &calls, map[string][]byte{
		"1": readFixture(t, "second_run_new.xml"),
	})
	require.NoError(t, Ingest(context.Background(), store, httpGet))

	for _, name := range preExisting {
		assert.Equal(t, provBefore[name], provenanceLineCount(t, store, name),
			"already-landed publication %s must be skipped on a re-run within the overlap window, not re-landed", name)
	}

	newLanded, err := store.Landed("koop/gmb-2022-100004.xml")
	require.NoError(t, err)
	assert.True(t, newLanded, "the genuinely new publication must be landed on the second run")
	assert.Equal(t, 1, provenanceLineCount(t, store, "koop/gmb-2022-100004.xml"))

	cursor := readCursor(t, store)
	assert.Equal(t, "2022-01-20", cursor.HighWaterMark,
		"the high-water mark must advance to the new max dt.available (2022-01-20) seen on the second run")
}

// -- True no-op re-run --------------------------------------------------------
//
// Covers spec scenario "Re-run against unchanged upstream is a no-op": no new
// artifact is written and no existing artifact or provenance record is
// altered. Snapshots the full file set and per-artifact provenance line
// counts before/after a re-run against the identical upstream fixture.

func TestIngest_RerunAgainstUnchangedUpstream_IsANoOp(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	fixture := readFixture(t, "first_run.xml")
	var calls []string
	httpGet := fakeSRUGetter(t, &calls, map[string][]byte{"1": fixture})

	require.NoError(t, Ingest(context.Background(), store, httpGet))

	filesBefore := listLandedFiles(t, store.BasePath)
	names := []string{"koop/gmb-2022-100001.xml", "koop/gmb-2022-100002.xml", "koop/gmb-2022-100003.xml"}
	provBefore := make(map[string]int, len(names))
	for _, name := range names {
		provBefore[name] = provenanceLineCount(t, store, name)
	}
	cursorBefore := readCursor(t, store)

	require.NoError(t, Ingest(context.Background(), store, httpGet))

	filesAfter := listLandedFiles(t, store.BasePath)
	assert.ElementsMatch(t, filesBefore, filesAfter,
		"a re-run against unchanged upstream content must not create or remove any file in the raw store")

	for _, name := range names {
		assert.Equal(t, provBefore[name], provenanceLineCount(t, store, name),
			"a re-run against unchanged upstream content must not append a provenance line for %s", name)
	}

	cursorAfter := readCursor(t, store)
	assert.Equal(t, cursorBefore, cursorAfter,
		"the high-water mark must not change when nothing new was landed")
}
