package geo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ensureIndexes creates the indexes the location resolver relies on (ported from
// spikes/spike-d/load.sh's index block, plus GIST on LIG/STA geom which the spike's Noord-only
// slice didn't need but the whole-city load does): a GIST index on each point-in-polygon geometry
// column, a pg_trgm GIN index on lower(openbareruimte.naam) for fuzzy street matching, and b-tree
// indexes on the NUM/VBO/LIG/STA address-join keys and (postcode, huisnummer).
func ensureIndexes(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	stmt := fmt.Sprintf(`
CREATE INDEX IF NOT EXISTS ix_vbo_geom      ON %s USING gist (geom);
CREATE INDEX IF NOT EXISTS ix_lig_geom      ON %s USING gist (geom);
CREATE INDEX IF NOT EXISTS ix_sta_geom      ON %s USING gist (geom);
CREATE INDEX IF NOT EXISTS ix_buurt_geom    ON %s USING gist (geom);

CREATE INDEX IF NOT EXISTS ix_opr_naam_trgm ON %s USING gin  (lower(naam) gin_trgm_ops);

CREATE INDEX IF NOT EXISTS ix_num_pc_hn     ON %s (postcode, huisnummer);
CREATE INDEX IF NOT EXISTS ix_num_ident     ON %s (identificatie);
CREATE INDEX IF NOT EXISTS ix_num_opr_ref   ON %s (openbareruimteref);
CREATE INDEX IF NOT EXISTS ix_vbo_ref       ON %s (hoofdadresnummeraanduidingref);
CREATE INDEX IF NOT EXISTS ix_lig_ref       ON %s (hoofdadresnummeraanduidingref);
CREATE INDEX IF NOT EXISTS ix_sta_ref       ON %s (hoofdadresnummeraanduidingref);
CREATE INDEX IF NOT EXISTS ix_opr_ident     ON %s (identificatie);
`,
		qualify(schema, "bag_verblijfsobject"),
		qualify(schema, "bag_ligplaats"),
		qualify(schema, "bag_standplaats"),
		qualify(schema, "gebieden_buurten"),
		qualify(schema, "bag_openbareruimte"),
		qualify(schema, "bag_nummeraanduiding"),
		qualify(schema, "bag_nummeraanduiding"),
		qualify(schema, "bag_nummeraanduiding"),
		qualify(schema, "bag_verblijfsobject"),
		qualify(schema, "bag_ligplaats"),
		qualify(schema, "bag_standplaats"),
		qualify(schema, "bag_openbareruimte"),
	)
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("geo: ensure indexes: %w", err)
	}
	return nil
}
