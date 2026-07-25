package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRootRegistersStages guards the CLI wiring: the pipeline must expose
// exactly the five stage subcommands, no more and no less.
func TestRootRegistersStages(t *testing.T) {
	var got []string
	for _, c := range newRootCmd().Commands() {
		got = append(got, c.Name())
	}

	assert.ElementsMatch(t, []string{"ingest", "extract", "load", "derive", "dump"}, got)
}

// TestStageIsNoOp confirms each stage runs, exits without error, and reports
// that it is not yet implemented.
func TestStageIsNoOp(t *testing.T) {
	for name := range map[string]bool{
		"ingest": true, "extract": true, "load": true, "derive": true, "dump": true,
	} {
		t.Run(name, func(t *testing.T) {
			root := newRootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs([]string{name})

			require.NoError(t, root.Execute())
			assert.Equal(t, name+": not yet implemented\n", out.String())
		})
	}
}
