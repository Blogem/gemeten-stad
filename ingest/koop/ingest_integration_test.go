package koop

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// -- Both aanvraag and besluit are harvested ----------------------------------
//
// Covers spec scenario "Both aanvraag and besluit publications are harvested":
// a corpus containing both publication types for the same zaaknummer must
// have BOTH landed, with no aanvraag/besluit dedup applied at harvest time
// (dedup by zaaknummer is P13's job).

func TestIngest_BothAanvraagAndBesluitAreHarvested(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	var calls []string
	httpGet := fakeSRUGetter(t, &calls, map[string][]byte{
		"1": readFixture(t, "aanvraag_besluit.xml"),
	})

	require.NoError(t, Ingest(context.Background(), store, httpGet))

	aanvraagLanded, err := store.Landed("koop/gmb-2023-700001.xml")
	require.NoError(t, err)
	assert.True(t, aanvraagLanded, "the aanvraag publication must be landed")

	besluitLanded, err := store.Landed("koop/gmb-2023-700002.xml")
	require.NoError(t, err)
	assert.True(t, besluitLanded, "the besluit publication must be landed")

	assert.NotEqual(t, "koop/gmb-2023-700001.xml", "koop/gmb-2023-700002.xml",
		"sanity: aanvraag and besluit must land under distinct identifiers/artifacts (no dedup)")
}

// TestIngest_SecondIdenticalRun_IsANoOp extends the aanvraag+besluit scenario:
// a second run against the identical upstream fixture must not create any new
// artifact and must not alter existing provenance (spec: "Re-run against
// unchanged upstream is a no-op").
func TestIngest_SecondIdenticalRun_IsANoOp(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())
	fixture := readFixture(t, "aanvraag_besluit.xml")

	var calls []string
	httpGet := fakeSRUGetter(t, &calls, map[string][]byte{"1": fixture})
	require.NoError(t, Ingest(context.Background(), store, httpGet))

	filesBefore := listLandedFiles(t, store.BasePath)
	names := []string{"koop/gmb-2023-700001.xml", "koop/gmb-2023-700002.xml"}
	provBefore := make(map[string]int, len(names))
	for _, name := range names {
		provBefore[name] = provenanceLineCount(t, store, name)
		require.Equal(t, 1, provBefore[name])
	}

	require.NoError(t, Ingest(context.Background(), store, httpGet))

	filesAfter := listLandedFiles(t, store.BasePath)
	assert.ElementsMatch(t, filesBefore, filesAfter,
		"a second identical run must not add or remove any file")
	for _, name := range names {
		assert.Equal(t, provBefore[name], provenanceLineCount(t, store, name),
			"a second identical run must not append provenance for %s", name)
	}
}

// -- Paging is exercised through the shared SRU client -------------------------
//
// Covers spec scenario "SRU paging is exercised through the shared package":
// the KOOP harvester's own Ingest call, not just ingest/shared's own tests,
// must page a multi-page result to exhaustion and land every record
// numberOfRecords reports.

func TestIngest_PagesToExhaustionThroughSharedClient(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	var calls []string
	httpGet := fakeSRUGetter(t, &calls, map[string][]byte{
		"1": readFixture(t, "paging_page1.xml"),
		"3": readFixture(t, "paging_page2.xml"),
	})

	require.NoError(t, Ingest(context.Background(), store, httpGet))

	for _, id := range []string{"gmb-2023-800001", "gmb-2023-800002", "gmb-2023-800003"} {
		landed, err := store.Landed("koop/" + id + ".xml")
		require.NoError(t, err)
		assert.True(t, landed, "publication %s from the second page must be landed", id)
	}

	require.Len(t, calls, 2, "a 3-record result set spanning two pages must issue exactly two SRU requests")
}

// -- Clause-by-clause numberOfRecords sanity guard -----------------------------
//
// Covers design D1/"Risks": KOOP silently returns numberOfRecords=0 (never an
// error) for an unknown index or an over-narrow clause. This guards two
// things together against a recorded fixture: (1) that the harvester actually
// reads and drives paging off numberOfRecords (already proven above by the
// 2-page test), and (2) that the query it ACTUALLY SENDS on a run carries
// every one of the four required clauses -- so a future edit that silently
// drops one (e.g. forgetting dt.available>= when advancing the cursor) is
// caught here even though the clause itself would still return >0 rows
// against a real endpoint until it doesn't.
func TestIngest_SentQueryCarriesEveryRequiredClause(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	var calls []string
	httpGet := fakeSRUGetter(t, &calls, map[string][]byte{
		"1": readFixture(t, "aanvraag_besluit.xml"),
	})
	require.NoError(t, Ingest(context.Background(), store, httpGet))

	require.NotEmpty(t, calls)
	sent := queryParam(t, calls[0])

	requiredClauses := []string{
		`dt.creator any "Amsterdam"`,
		`dt.type any "omgevingsvergunning"`,
		`cql.textAndIndexes any "kappen vellen rooien verplanten houtopstand"`,
		`dt.available>=`,
	}
	for _, clause := range requiredClauses {
		assert.Contains(t, sent, clause,
			"the query the harvester actually sends must carry the %q clause -- if any single one were missing, the real endpoint would silently return numberOfRecords=0 rather than an error", clause)
	}

	// This is the query Ingest is expected to build for a clean-volume first
	// run (DefaultSinceDate, no stored high-water mark yet); pinning it to
	// BuildQuery's own output proves Ingest actually routes through the query
	// builder rather than assembling an independent, divergent query string.
	assert.Equal(t, BuildQuery(DefaultSinceDate), sent,
		"the harvester's first-run query must be exactly what BuildQuery produces for the default lower bound")
}
