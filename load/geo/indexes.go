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
func ensureIndexes(ctx context.Context, pool *pgxpool.Pool) error {
	const stmt = `
CREATE INDEX IF NOT EXISTS ix_vbo_geom      ON bag_verblijfsobject  USING gist (geom);
CREATE INDEX IF NOT EXISTS ix_lig_geom      ON bag_ligplaats        USING gist (geom);
CREATE INDEX IF NOT EXISTS ix_sta_geom      ON bag_standplaats      USING gist (geom);
CREATE INDEX IF NOT EXISTS ix_buurt_geom    ON gebieden_buurten     USING gist (geom);

CREATE INDEX IF NOT EXISTS ix_opr_naam_trgm ON bag_openbareruimte   USING gin  (lower(naam) gin_trgm_ops);

CREATE INDEX IF NOT EXISTS ix_num_pc_hn     ON bag_nummeraanduiding (postcode, huisnummer);
CREATE INDEX IF NOT EXISTS ix_num_ident     ON bag_nummeraanduiding (identificatie);
CREATE INDEX IF NOT EXISTS ix_num_opr_ref   ON bag_nummeraanduiding (openbareruimteref);
CREATE INDEX IF NOT EXISTS ix_vbo_ref       ON bag_verblijfsobject  (hoofdadresnummeraanduidingref);
CREATE INDEX IF NOT EXISTS ix_lig_ref       ON bag_ligplaats        (hoofdadresnummeraanduidingref);
CREATE INDEX IF NOT EXISTS ix_sta_ref       ON bag_standplaats      (hoofdadresnummeraanduidingref);
CREATE INDEX IF NOT EXISTS ix_opr_ident     ON bag_openbareruimte   (identificatie);
`
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("geo: ensure indexes: %w", err)
	}
	return nil
}
