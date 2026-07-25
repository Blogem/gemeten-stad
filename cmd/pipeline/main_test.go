package main

import (
	"bytes"
	"testing"
)

// TestRootRegistersStages guards the CLI wiring: the pipeline must expose
// exactly the five stage subcommands, no more and no less.
func TestRootRegistersStages(t *testing.T) {
	want := map[string]bool{
		"ingest":  true,
		"extract": true,
		"load":    true,
		"derive":  true,
		"dump":    true,
	}

	got := map[string]bool{}
	for _, c := range newRootCmd().Commands() {
		got[c.Name()] = true
	}

	for name := range want {
		if !got[name] {
			t.Errorf("missing subcommand %q", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("unexpected subcommand %q", name)
		}
	}
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

			if err := root.Execute(); err != nil {
				t.Fatalf("%s returned error: %v", name, err)
			}
			if want := name + ": not yet implemented\n"; out.String() != want {
				t.Errorf("%s output = %q, want %q", name, out.String(), want)
			}
		})
	}
}
