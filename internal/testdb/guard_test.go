package testdb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssertNotProduction(t *testing.T) {
	tests := []struct {
		name        string
		schemaName  string
		datasetName string
		wantErr     bool
	}{
		{name: "isolated schema and dataset", schemaName: "test_a1b2c3d4", datasetName: "test-a1b2c3d4", wantErr: false},
		{name: "empty names", schemaName: "", datasetName: "", wantErr: false},
		{name: "production database name as schema", schemaName: "gemeten_stad", datasetName: "", wantErr: true},
		{name: "default postgres schema", schemaName: "public", datasetName: "", wantErr: true},
		{name: "production dataset name", schemaName: "", datasetName: "ds", wantErr: true},
		{name: "reserved name, different case", schemaName: "PUBLIC", datasetName: "", wantErr: true},
		{name: "reserved dataset name, different case", schemaName: "", datasetName: "DS", wantErr: true},
		{name: "both reserved", schemaName: "gemeten_stad", datasetName: "ds", wantErr: true},
		{name: "isolated schema, reserved dataset", schemaName: "test_a1b2c3d4", datasetName: "ds", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AssertNotProduction(tt.schemaName, tt.datasetName)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNewSchemaAndDatasetNamesAreIsolated(t *testing.T) {
	schemaName, err := NewSchemaName()
	require.NoError(t, err)
	datasetName, err := NewDatasetName()
	require.NoError(t, err)
	require.NoError(t, AssertNotProduction(schemaName, datasetName),
		"a freshly generated name must never be rejected by the guard")
}

func TestNewDatabaseNameIsIsolated(t *testing.T) {
	dbName, err := NewDatabaseName()
	require.NoError(t, err)
	require.NoError(t, AssertNotProduction(dbName, ""),
		"a freshly generated database name must never be rejected by the guard")
}
