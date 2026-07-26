package dump

import (
	"fmt"
	"path/filepath"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// Config resolves the three store connections a snapshot export/restore run
// needs, from the same GS_* env contract the rest of the pipeline uses.
type Config struct {
	// FusekiURL is the RDF graph dataset endpoint (GS_FUSEKI_URL), e.g. "http://localhost:3030/ds".
	FusekiURL string
	// DatabaseURL is the pgx/pg_dump PostGIS connection string (GS_DATABASE_URL).
	DatabaseURL string
	// NERCachePath is the directory holding the NER cache (GS_NER_CACHE_PATH, or its default).
	NERCachePath string
}

// ConfigFromEnv resolves Config from env (pass os.Getenv at the call site). GS_FUSEKI_URL and
// GS_DATABASE_URL are required, matching the rest of the pipeline's env contract; NERCachePath
// falls back to NERCachePathFromEnv's default when GS_NER_CACHE_PATH is unset.
func ConfigFromEnv(env func(string) string) (Config, error) {
	fusekiURL := env("GS_FUSEKI_URL")
	if fusekiURL == "" {
		return Config{}, fmt.Errorf("dump: GS_FUSEKI_URL is not set")
	}

	databaseURL, err := shared.DatabaseURL(env)
	if err != nil {
		return Config{}, err
	}

	nerCachePath, err := NERCachePathFromEnv(env)
	if err != nil {
		return Config{}, err
	}

	return Config{
		FusekiURL:    fusekiURL,
		DatabaseURL:  databaseURL,
		NERCachePath: nerCachePath,
	}, nil
}

// NERCachePathFromEnv resolves GS_NER_CACHE_PATH from env, defaulting to
// "<GS_RAW_DATA_PATH>/ner-cache" when unset (GS_RAW_DATA_PATH is required with no code default —
// see shared.RawDataPath — so this fails loud the same way if neither is set).
func NERCachePathFromEnv(env func(string) string) (string, error) {
	if path := env("GS_NER_CACHE_PATH"); path != "" {
		return path, nil
	}

	rawPath, err := shared.RawDataPath(env)
	if err != nil {
		return "", err
	}
	return filepath.Join(rawPath, "ner-cache"), nil
}
