package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeriveRegistryShape asserts the derive registry exposes exactly the
// name coverage. It reads the unexported deriveRegistry directly (fine
// within package main) rather than executing any derivation, since
// derivations require a live database / triplestore connection.
func TestDeriveRegistryShape(t *testing.T) {
	var names []string
	for _, s := range deriveRegistry {
		names = append(names, s.name)
	}

	assert.Equal(t, []string{"coverage"}, names)
}

// TestDeriveCmd_UnknownSourceFailsFast exercises the derive subcommand
// end-to-end through cobra: an unknown positional source name must make
// root.Execute() return a non-nil error mentioning the unknown name, without
// attempting to connect to the database or run any derivation. A valid
// source is deliberately never exercised here (that would require a live
// Postgres).
func TestDeriveCmd_UnknownSourceFailsFast(t *testing.T) {
	root := newRootCmd()

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"derive", "definitely-not-a-source"})

	err := root.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "definitely-not-a-source")
}
