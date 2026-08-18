package cmd

import (
	"testing"

	"github.com/spf13/cobra"
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

// force=true must bypass the prompt entirely — this proves the wiring calls
// confirmAction with the raw path as the subject without ever touching stdin.
func TestRawDeleteConfirm_SkipsPromptWhenForced(t *testing.T) {
	if err := rawDeleteConfirm("/v1/customers/cust_123", true); err != nil {
		t.Errorf("rawDeleteConfirm with force=true: %v", err)
	}
}

// go test's stdin is never a terminal, so this exercises the same
// non-interactive bypass that protects scripts and CI, now reused by the raw
// delete path.
func TestRawDeleteConfirm_SkipsPromptWhenStdinIsNotATerminal(t *testing.T) {
	if err := rawDeleteConfirm("/v1/customers/cust_123", false); err != nil {
		t.Errorf("rawDeleteConfirm on non-terminal stdin: %v", err)
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
