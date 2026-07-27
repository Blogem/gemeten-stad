package koop

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// publicationKey is koop_publications' identity: the SRU record's own gmb_id.
const publicationKey = "gmb_id"

// publicationColumns lists every koop_publications column in the fixed order shared by
// stagePublications' INSERT (stage.go) and mergeSQL's INSERT/UPDATE lists below, key first.
var publicationColumns = []string{
	publicationKey,
	"zaaknummer",
	"kind",
	"available",
	"geom",
	"postcode",
	"huisnummer",
	"resolved_identificatie",
	"resolved_buurt_code",
	"resolved_confidence",
	"resolved_geom",
	"resolved_tier",
	"caveats",
	"in_noord",
	"unresolved",
	"raw",
}

// publicationsMergeSQL builds the MERGE statement that reconciles koop_publications against its
// staging snapshot for schema.
//
// Unlike load/bomen (whose `id` is a mutable key compared only via its `raw` column), a koop
// publication row's mutable content spans two independent things that can each change on their own
// re-run: the publication itself (kind/available/geom/... — captured in raw) and its resolution
// (resolved_*/in_noord/unresolved, recomputed by resolve.go without the publication changing). The
// WHEN MATCHED guard below therefore compares every non-key column, not just raw, so either kind of
// change — and only an actual change — triggers an update; an unchanged re-run touches nothing.
//
// koop keeps no source_deleted_at / WHEN NOT MATCHED BY SOURCE clause, unlike load/bomen: the
// publication corpus only grows (a KOOP gmb-id, once published, is never retracted from the SRU
// feed), so there is nothing to soft-delete.
func publicationsMergeSQL(schema string) string {
	names := make([]string, len(publicationColumns))
	values := make([]string, len(publicationColumns))
	var changeChecks []string
	var refreshSets []string
	for i, col := range publicationColumns {
		names[i] = col
		values[i] = "s." + col
		if col == publicationKey {
			continue
		}
		changeChecks = append(changeChecks, fmt.Sprintf("t.%s IS DISTINCT FROM s.%s", col, col))
		refreshSets = append(refreshSets, fmt.Sprintf("%s = s.%s", col, col))
	}

	target := qualify(schema, publicationsTable)
	source := qualify(schema, publicationsStagingTable)

	return fmt.Sprintf(`
MERGE INTO %s AS t
USING %s AS s
ON t.%s = s.%s
WHEN MATCHED AND (%s) THEN
  UPDATE SET %s
WHEN NOT MATCHED THEN
  INSERT (%s) VALUES (%s);
`,
		target, source,
		publicationKey, publicationKey,
		strings.Join(changeChecks, " OR "),
		strings.Join(refreshSets, ", "),
		strings.Join(names, ", "), strings.Join(values, ", "),
	)
}

// upsertPublications reconciles koop_publications against koop_publications_staging (see
// publicationsMergeSQL). loadTS is accepted to mirror load/bomen's upsertAll signature and leave
// room for a future soft-delete pass, but is currently unused: koop has no source_deleted_at column
// to stamp (see publicationsMergeSQL's doc comment).
func upsertPublications(ctx context.Context, pool *pgxpool.Pool, schema string, loadTS time.Time) error {
	_ = loadTS
	if _, err := pool.Exec(ctx, publicationsMergeSQL(schema)); err != nil {
		return fmt.Errorf("koop: upsert %s: %w", publicationsTable, err)
	}
	return nil
}
