package testdb

import "testing"

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
			if (err != nil) != tt.wantErr {
				t.Fatalf("AssertNotProduction(%q, %q) error = %v, wantErr %v", tt.schemaName, tt.datasetName, err, tt.wantErr)
			}
		})
	}
}

func TestNewSchemaAndDatasetNamesAreIsolated(t *testing.T) {
	schemaName, err := NewSchemaName()
	if err != nil {
		t.Fatalf("NewSchemaName: %v", err)
	}
	datasetName, err := NewDatasetName()
	if err != nil {
		t.Fatalf("NewDatasetName: %v", err)
	}
	if err := AssertNotProduction(schemaName, datasetName); err != nil {
		t.Fatalf("a freshly generated name must never be rejected by the guard: %v", err)
	}
}
