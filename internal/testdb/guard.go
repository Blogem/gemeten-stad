// Package testdb is the integration-test isolation harness (Phase 0 · P5,
// docs/PHASE_0_PLAN.md). It creates ephemeral Postgres schemas and Fuseki
// datasets for a single test run and guards against ever targeting the
// production/dev names (docs/IMPLEMENTATION_PLAN.md §5).
package testdb

import (
	"fmt"
	"strings"
)

// reservedNames are the production/dev names an integration test must never
// target: the dev-compose Postgres database, its default schema, and the
// dev-compose Fuseki dataset (deploy/compose/compose.yaml).
var reservedNames = map[string]bool{
	"gemeten_stad": true,
	"public":       true,
	"ds":           true,
}

// AssertNotProduction errors if schemaName or datasetName resolves
// (case-insensitively) to a reserved production/dev name. Callers must check
// this before opening any connection or calling a store's admin API, so a
// misconfigured test aborts instead of mutating real data. Pass "" for
// whichever name does not apply.
func AssertNotProduction(schemaName, datasetName string) error {
	if schemaName != "" && reservedNames[strings.ToLower(schemaName)] {
		return fmt.Errorf("testdb: refusing to target schema %q — it is a reserved production/dev name", schemaName)
	}
	if datasetName != "" && reservedNames[strings.ToLower(datasetName)] {
		return fmt.Errorf("testdb: refusing to target dataset %q — it is a reserved production/dev name", datasetName)
	}
	return nil
}
