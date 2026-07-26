package main

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
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

// TestStageIsNoOp confirms the stub stages (extract, derive, dump) run, exit
// without error, and report that they are not yet implemented. ingest and
// load are wired to real work and are covered by TestIngestAndLoadAreWired
// instead — running their RunE here would require network/DB access.
func TestStageIsNoOp(t *testing.T) {
	for name := range map[string]bool{
		"extract": true, "derive": true, "dump": true,
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

// TestIngestAndLoadAreWired asserts ingest/load are wired to real
// implementations (non-stub RunE) without invoking that RunE, which would
// require network/DB access. It also checks load registers its --reset flag
// and that both ingest and load accept an arbitrary number of positional
// source-name arguments (cobra.ArbitraryArgs), since load now takes
// "load [source ...]" just like ingest.
func TestIngestAndLoadAreWired(t *testing.T) {
	root := newRootCmd()
	commands := map[string]*cobra.Command{}
	for _, c := range root.Commands() {
		commands[c.Name()] = c
	}

	for _, name := range []string{"ingest", "load"} {
		c, ok := commands[name]
		if !ok {
			t.Fatalf("missing subcommand %q", name)
		}
		if c.RunE == nil {
			t.Errorf("%s: expected non-nil RunE", name)
		}
		if c.Args == nil {
			t.Errorf("%s: expected non-nil Args validator (ArbitraryArgs)", name)
			continue
		}
		// ArbitraryArgs accepts any number of positional args, including more
		// than one; validating that here (rather than comparing function
		// identity, which cobra.ArbitraryArgs does not support) confirms the
		// command was not left at cobra's zero-arg default.
		assert.NoError(t, c.Args(c, []string{"one", "two", "three"}),
			"%s: Args should accept an arbitrary number of positional args", name)
	}

	loadCmd := commands["load"]
	resetFlag := loadCmd.Flags().Lookup("reset")
	if resetFlag == nil {
		t.Fatal("load: expected a --reset flag")
	}
	if resetFlag.Value.Type() != "bool" {
		t.Errorf("load: --reset flag type = %q, want %q", resetFlag.Value.Type(), "bool")
	}
}
