package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/flexprice/cli/internal/ui"
)

// Raw `delete` had zero confirmation: RunE went straight from argument
// parsing to cl.Do with no prompt, no --force flag, and no terminal check.
// These tests cover the fix — the --force flag exists on delete only, and
// the confirmation wiring (rawDeleteConfirm) matches the spec-driven path's
// confirmAction/promptConfirm behavior already covered in resource_test.go.

func TestRawDeleteCommand_HasForceFlag(t *testing.T) {
	root := NewRootCommand("test")

	del := findRawCommand(t, root, "delete")
	if del.Flags().Lookup("force") == nil {
		t.Error(`raw "delete" command is missing a --force flag`)
	}
}

func TestRawGetAndPostCommands_HaveNoForceFlag(t *testing.T) {
	root := NewRootCommand("test")

	for _, name := range []string{"get", "post"} {
		c := findRawCommand(t, root, name)
		if c.Flags().Lookup("force") != nil {
			t.Errorf("raw %q command should not expose --force; only delete is destructive", name)
		}
	}
}

// testGlobals builds a Globals whose UI cannot prompt, matching how the CLI
// behaves in a script or CI.
func testGlobals() *Globals {
	return &Globals{UI: ui.New(ui.Options{StderrTTY: true, StdinTTY: false, Term: "dumb"})}
}

// force=true must bypass the prompt entirely — this proves the wiring calls
// confirmAction with the raw path as the subject without ever touching stdin.
func TestRawDeleteConfirm_SkipsPromptWhenForced(t *testing.T) {
	if err := rawDeleteConfirm(testGlobals(), "/v1/customers/cust_123", true); err != nil {
		t.Errorf("rawDeleteConfirm with force=true: %v", err)
	}
}

// BEHAVIOUR CHANGE, deliberate. This test previously asserted the opposite:
// that a non-terminal stdin bypassed confirmation and proceeded. That meant a
// script piping into `flexprice delete` destroyed data with nothing asked and
// nothing logged.
//
// It now refuses and names --force. A script that relied on the old bypass
// fails until --force is added, which is the point: deleting because nobody
// could be asked is the worse default.
func TestRawDeleteConfirm_RefusesWhenStdinIsNotATerminal(t *testing.T) {
	err := rawDeleteConfirm(testGlobals(), "/v1/customers/cust_123", false)
	if err == nil {
		t.Fatal("raw delete proceeded without confirmation on a non-terminal stdin")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("refusal must name --force so a script can be fixed, got %q", err)
	}
	if !strings.Contains(err.Error(), "cust_123") {
		t.Errorf("refusal must name the target, got %q", err)
	}
}

func findRawCommand(t *testing.T, root *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("command %q not registered", name)
	return nil
}
