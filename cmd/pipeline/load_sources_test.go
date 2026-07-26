package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadRegistryShape asserts the load registry exposes exactly the names
// geo, bomen, and graph, in that order — the value-store loaders (geo, bomen)
// then the SHACL-gated graph writer. It reads the unexported loadRegistry
// directly (fine within package main) rather than executing any loader,
// since loaders require a live database / triplestore connection.
func TestLoadRegistryShape(t *testing.T) {
	var names []string
	for _, s := range loadRegistry {
		names = append(names, s.name)
	}

	assert.Equal(t, []string{"geo", "bomen", "graph"}, names)
}

// TestLoadCmd_UnknownSourceFailsFast exercises the load subcommand
// end-to-end through cobra: an unknown positional source name must make
// root.Execute() return a non-nil error mentioning the unknown name, without
// attempting to connect to the database or run any loader. A valid source is
// deliberately never exercised here (that would require a live Postgres).
func TestLoadCmd_UnknownSourceFailsFast(t *testing.T) {
	root := newRootCmd()

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"load", "definitely-not-a-source"})

	err := root.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "definitely-not-a-source")
}
