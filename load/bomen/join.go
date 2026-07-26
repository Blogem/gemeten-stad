package bomen

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// txExecer is the subset of *pgx.Tx (and *pgxpool.Pool) resolveJoin's helpers run against, so the
// same read/write code works against resolveJoin's one transaction.
type txExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// kapenherplantJoinRow is one kapenherplant row's join-relevant columns, read back for the
// resolution pass.
type kapenherplantJoinRow struct {
	id          string
	boomID      string
	boomNieuwID string
}

// resolveJoin is the DB-executing counterpart of resolvePoint (design.md D3): after both targets
// are upserted, it looks up every kapenherplant row's stamgegevens match (boomId, then
// boomNieuwId on a miss) and writes resolvedGeom/resolvedVia back onto that row, all in one
// transaction so the pass is atomic. It runs over every kapenherplant row on every load (not just
// newly-changed rows), because a stamgegevens-only refresh can newly resolve a
// previously-unresolved kapenherplant row.
func resolveJoin(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("bomen: resolve join: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	stamIDs, err := queryStamgegevensIDs(ctx, tx)
	if err != nil {
		return fmt.Errorf("bomen: resolve join: read stamgegevens ids: %w", err)
	}
	stamIDSet := make(map[string]struct{}, len(stamIDs))
	for _, id := range stamIDs {
		stamIDSet[id] = struct{}{}
	}

	kapRows, err := queryKapenherplantJoinRows(ctx, tx)
	if err != nil {
		return fmt.Errorf("bomen: resolve join: read kapenherplant rows: %w", err)
	}

	for _, row := range kapRows {
		resolvedID, via := resolvePoint(row.boomID, row.boomNieuwID, stamIDSet)
		if err := writeResolution(ctx, tx, row.id, resolvedID, via); err != nil {
			return fmt.Errorf("bomen: resolve join: write resolution for %s: %w", row.id, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("bomen: resolve join: commit: %w", err)
	}
	return nil
}

func queryStamgegevensIDs(ctx context.Context, conn txExecer) ([]string, error) {
	rows, err := conn.Query(ctx, fmt.Sprintf(`SELECT id FROM %s`, stamgegevensTable))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func queryKapenherplantJoinRows(ctx context.Context, conn txExecer) ([]kapenherplantJoinRow, error) {
	sql := fmt.Sprintf(`SELECT id, coalesce("boomId", ''), coalesce("boomNieuwId", '') FROM %s`, kapenherplantTable)
	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []kapenherplantJoinRow
	for rows.Next() {
		var r kapenherplantJoinRow
		if err := rows.Scan(&r.id, &r.boomID, &r.boomNieuwID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// writeResolution stamps resolvedGeom/resolvedVia onto one kapenherplant row. An unresolved row
// still gets its resolvedVia written (and resolvedGeom left/set NULL) — never skipped
// (specs/bomen-load/spec.md).
func writeResolution(ctx context.Context, conn txExecer, kapID, resolvedStamID, via string) error {
	if resolvedStamID == "" {
		sql := fmt.Sprintf(`UPDATE %s SET "resolvedGeom" = NULL, "resolvedVia" = $1 WHERE id = $2`, kapenherplantTable)
		_, err := conn.Exec(ctx, sql, via, kapID)
		return err
	}

	sql := fmt.Sprintf(`
UPDATE %s AS k SET "resolvedGeom" = s.geometrie, "resolvedVia" = $1
FROM %s AS s WHERE k.id = $2 AND s.id = $3`, kapenherplantTable, stamgegevensTable)
	_, err := conn.Exec(ctx, sql, via, kapID, resolvedStamID)
	return err
}
