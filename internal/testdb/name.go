package testdb

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// randomSuffix returns n random bytes hex-encoded, for building a name that
// cannot collide with a concurrently-running test's isolated schema/dataset.
func randomSuffix(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("testdb: generate random suffix: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// NewSchemaName returns a randomly-suffixed Postgres schema name for one
// isolated test run, e.g. "test_a1b2c3d4".
func NewSchemaName() (string, error) {
	suffix, err := randomSuffix(4)
	if err != nil {
		return "", err
	}
	return "test_" + suffix, nil
}

// NewDatasetName returns a randomly-suffixed Fuseki dataset name for one
// isolated test run, e.g. "test-a1b2c3d4".
func NewDatasetName() (string, error) {
	suffix, err := randomSuffix(4)
	if err != nil {
		return "", err
	}
	return "test-" + suffix, nil
}

// NewDatabaseName returns a randomly-suffixed Postgres database name for one isolated test run,
// e.g. "testdb_a1b2c3d4". Distinct from NewSchemaName: pg_dump/pg_restore (P9 dump/restore)
// operate at whole-database granularity, so a test exercising them needs a dedicated throwaway
// database, not just a schema within the shared one.
func NewDatabaseName() (string, error) {
	suffix, err := randomSuffix(4)
	if err != nil {
		return "", err
	}
	return "testdb_" + suffix, nil
}
