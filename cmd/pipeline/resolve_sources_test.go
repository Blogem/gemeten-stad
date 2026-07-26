package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveSources covers the generic source-name resolver shared by both
// `pipeline ingest [source ...]` and `pipeline load [source ...]`: empty args
// select every name in valid (registration order), named args select those
// names in the order given, and an unknown name fails fast with an error
// listing the valid names. This test is pure and does not depend on either
// registry — it only exercises the (args, valid) contract.
func TestResolveSources(t *testing.T) {
	valid := []string{"a", "b"}

	tests := []struct {
		name      string
		args      []string
		wantNames []string
		wantErr   bool
		errMsgAll []string // substrings that must all appear in the error message
	}{
		{
			name:      "no args selects all valid names in order",
			args:      nil,
			wantNames: []string{"a", "b"},
		},
		{
			name:      "all valid names given explicitly, in order",
			args:      []string{"a", "b"},
			wantNames: []string{"a", "b"},
		},
		{
			name:      "single named source",
			args:      []string{"b"},
			wantNames: []string{"b"},
		},
		{
			name:      "reversed order is preserved, not normalized",
			args:      []string{"b", "a"},
			wantNames: []string{"b", "a"},
		},
		{
			name:      "unknown name errors listing the valid names",
			args:      []string{"x"},
			wantErr:   true,
			errMsgAll: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names, err := resolveSources(tt.args, valid)

			if tt.wantErr {
				require.Error(t, err)
				for _, substr := range tt.errMsgAll {
					assert.Contains(t, err.Error(), substr)
				}
				assert.Nil(t, names, "names should be nil on error")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantNames, names)
		})
	}
}
