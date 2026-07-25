package shared

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DatabaseURL resolves GS_DATABASE_URL from env; error (fail loud) if unset/empty.
func DatabaseURL(env func(string) string) (string, error) {
	url := env("GS_DATABASE_URL")
	if url == "" {
		return "", fmt.Errorf("shared: GS_DATABASE_URL is not set")
	}
	return url, nil
}

// ConnectPostgres opens a pgxpool connection pool to dsn and pings it.
func ConnectPostgres(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("shared: connect postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("shared: ping postgres: %w", err)
	}

	return pool, nil
}
