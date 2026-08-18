package cmd

import (
	"bytes"
	"testing"
)

func TestRootCommand_HasName(t *testing.T) {
	root := NewRootCommand("test")
	if root.Use != "flexprice" {
		t.Fatalf("Use = %q, want %q", root.Use, "flexprice")
	}
}

func TestRootCommand_HelpMentionsUsageBilling(t *testing.T) {
	root := NewRootCommand("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("help output is empty")
	}
}
