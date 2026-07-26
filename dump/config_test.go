package dump

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func envMap(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestNERCachePathFromEnv_ExplicitOverride(t *testing.T) {
	got, err := NERCachePathFromEnv(envMap(map[string]string{
		"GS_NER_CACHE_PATH": "/custom/ner-cache",
		"GS_RAW_DATA_PATH":  "/raw",
	}))
	require.NoError(t, err)
	assert.Equal(t, "/custom/ner-cache", got)
}

func TestNERCachePathFromEnv_DefaultsUnderRawDataPath(t *testing.T) {
	got, err := NERCachePathFromEnv(envMap(map[string]string{
		"GS_RAW_DATA_PATH": "/raw",
	}))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/raw", "ner-cache"), got)
}

func TestNERCachePathFromEnv_FailsLoudWithoutRawDataPath(t *testing.T) {
	_, err := NERCachePathFromEnv(envMap(nil))
	assert.Error(t, err)
}

func TestConfigFromEnv_ResolvesAllThreeStores(t *testing.T) {
	cfg, err := ConfigFromEnv(envMap(map[string]string{
		"GS_FUSEKI_URL":    "http://localhost:3030/ds",
		"GS_DATABASE_URL":  "postgres://gs:gs@localhost:5433/gemeten_stad",
		"GS_RAW_DATA_PATH": "/raw",
	}))
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:3030/ds", cfg.FusekiURL)
	assert.Equal(t, "postgres://gs:gs@localhost:5433/gemeten_stad", cfg.DatabaseURL)
	assert.Equal(t, filepath.Join("/raw", "ner-cache"), cfg.NERCachePath)
}

func TestConfigFromEnv_FailsLoudWhenFusekiURLUnset(t *testing.T) {
	_, err := ConfigFromEnv(envMap(map[string]string{
		"GS_DATABASE_URL":  "postgres://gs:gs@localhost:5433/gemeten_stad",
		"GS_RAW_DATA_PATH": "/raw",
	}))
	assert.Error(t, err)
}

func TestConfigFromEnv_FailsLoudWhenDatabaseURLUnset(t *testing.T) {
	_, err := ConfigFromEnv(envMap(map[string]string{
		"GS_FUSEKI_URL":    "http://localhost:3030/ds",
		"GS_RAW_DATA_PATH": "/raw",
	}))
	assert.Error(t, err)
}

func TestConfigFromEnv_FailsLoudWhenNERCachePathUnresolvable(t *testing.T) {
	_, err := ConfigFromEnv(envMap(map[string]string{
		"GS_FUSEKI_URL":   "http://localhost:3030/ds",
		"GS_DATABASE_URL": "postgres://gs:gs@localhost:5433/gemeten_stad",
	}))
	assert.Error(t, err)
}
