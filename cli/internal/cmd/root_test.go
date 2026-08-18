package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommand_HasName(t *testing.T) {
	root := NewRootCommand("test")
	if root.Use != "flexprice" {
		t.Fatalf("Use = %q, want %q", root.Use, "flexprice")
	}
}

func TestRootCommand_HelpShowsDescriptionAndFlags(t *testing.T) {
	root := NewRootCommand("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Flexprice API") {
		t.Errorf("help output missing the product description:\n%s", out)
	}
	// Guards the Run stub: cobra omits the Flags section for a root command that
	// is neither Runnable nor has subcommands, so removing the stub prematurely
	// would silently stop help from listing any flags.
	if !strings.Contains(out, "--profile") {
		t.Errorf("help output missing the Flags section:\n%s", out)
	}
}

// Two roots in one process must not share flag state. pflag writes defaults into
// the bound pointer at registration time, so a package-level Globals would have
// the second construction clobber the first's parsed values.
func TestNewRootCommand_InstancesDoNotShareState(t *testing.T) {
	rootA := NewRootCommand("a")
	rootA.SetOut(&bytes.Buffer{})
	rootA.SetArgs([]string{"--api-key", "secret-from-A", "--profile", "profileA"})
	if err := rootA.Execute(); err != nil {
		t.Fatalf("rootA.Execute: %v", err)
	}

	gotKey, err := rootA.PersistentFlags().GetString("api-key")
	if err != nil {
		t.Fatalf("GetString: %v", err)
	}
	if gotKey != "secret-from-A" {
		t.Fatalf("rootA api-key = %q before second construction, want secret-from-A", gotKey)
	}

	_ = NewRootCommand("b")

	gotKey, err = rootA.PersistentFlags().GetString("api-key")
	if err != nil {
		t.Fatalf("GetString: %v", err)
	}
	if gotKey != "secret-from-A" {
		t.Errorf("rootA api-key = %q after constructing a second root; state leaked", gotKey)
	}
}
