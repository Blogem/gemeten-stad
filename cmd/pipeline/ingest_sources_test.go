package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSelectSources covers the pure source-name resolver behind `pipeline
// ingest [source ...]` (OpenSpec change per-source-ingest, tasks 3.1/3.2,
// scenarios "Ingest a single named source", "No arguments ingests all
// sources", and "Unknown source name fails fast").
func TestSelectSources(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantNames []string
		wantErr   bool
		errMsgAll []string // substrings that must all appear in the error message
	}{
		{
			name:      "no args selects all sources in registration order",
			args:      nil,
			wantNames: []string{"bag", "gebieden"},
		},
		{
			name:      "single known source: bag",
			args:      []string{"bag"},
			wantNames: []string{"bag"},
		},
		{
			name:      "single known source: gebieden",
			args:      []string{"gebieden"},
			wantNames: []string{"gebieden"},
		},
		{
			name:      "multiple known sources preserve given order",
			args:      []string{"gebieden", "bag"},
			wantNames: []string{"gebieden", "bag"},
		},
		{
			name:      "unknown source errors listing valid names",
			args:      []string{"nope"},
			wantErr:   true,
			errMsgAll: []string{"bag", "gebieden"},
		},
		{
			name:      "unknown mixed with known source still errors with no names",
			args:      []string{"bag", "nope"},
			wantErr:   true,
			errMsgAll: []string{"bag", "gebieden"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names, err := selectSources(tt.args)

			if tt.wantErr {
				require.Error(t, err)
				for _, substr := range tt.errMsgAll {
					assert.Contains(t, err.Error(), substr)
				}
				assert.Empty(t, names, "names should be nil/empty on error")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

// TestSelectSources_RegistryShape asserts the registry exposes exactly the
// names bag and gebieden, in that order, via the public selectSources(nil)
// contract rather than reaching into unexported registry internals.
func TestSelectSources_RegistryShape(t *testing.T) {
	names, err := selectSources(nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"bag", "gebieden"}, names)
}

// TestIngestCmd_UnknownSourceFailsFast exercises the ingest subcommand
// end-to-end through cobra: an unknown positional source name must make
// root.Execute() return a non-nil error mentioning the unknown name and/or
// the valid names, without attempting to ingest anything (which would hit
// the network). A valid source is deliberately never exercised here.
func TestIngestCmd_UnknownSourceFailsFast(t *testing.T) {
	root := newRootCmd()

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"ingest", "definitely-not-a-source"})

	err := root.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "definitely-not-a-source")
}
