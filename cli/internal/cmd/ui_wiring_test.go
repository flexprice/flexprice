package cmd

import (
	"bytes"
	"testing"
)

// The UI must never be nil, even for a command constructed directly in a test
// that never runs PersistentPreRunE.
func TestGlobals_UIIsNeverNil(t *testing.T) {
	root := NewRootCommand("test")
	g := globalsFor(root)
	if g == nil {
		t.Fatal("globalsFor returned nil for a freshly constructed root")
	}
	if g.UI == nil {
		t.Fatal("Globals.UI is nil before flag parsing; every call site would panic")
	}
}

// Regression guard: pflag does not populate bound values until Execute parses
// them, so a UI built at construction time would capture --quiet as false
// regardless of what was passed.
func TestGlobals_UIReflectsParsedFlags(t *testing.T) {
	root := NewRootCommand("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--quiet", "version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	g := globalsFor(root)
	if !g.Quiet {
		t.Fatal("--quiet did not reach Globals")
	}
	if !g.UI.Quiet() {
		t.Error("UI was built before flags were parsed: it does not see --quiet")
	}
}

// --no-input must reach Globals, which is the only half of this that can be
// asserted here.
//
// Deliberately NOT asserting g.UI.NoInput() is true: ui.New treats a non-TTY
// stdin as implying --no-input, and stdin is never a TTY under `go test`, so
// that assertion would hold even if the flag were never wired up at all —
// confirmed by mutating PersistentPreRunE to skip rebuilding the UI, at which
// point such an assertion still passed. The flag's effect on the UI is covered
// by TestGatingMatrix in internal/ui, which can set StdinTTY independently.
//
// Asserting on colour here would be vacuous for the same reason: stderr is a
// buffer under test, never a TTY. root_test.go covers --no-color against style
// directly.
func TestGlobals_NoInputReachesGlobals(t *testing.T) {
	root := NewRootCommand("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--no-input", "version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !globalsFor(root).NoInput {
		t.Fatal("--no-input did not reach Globals")
	}
}

// The flag must default to false, or every prompt would refuse. Cheap, but it
// is the half of the default that a typo in bindGlobals would silently invert.
func TestGlobals_NoInputDefaultsFalse(t *testing.T) {
	root := NewRootCommand("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if globalsFor(root).NoInput {
		t.Error("--no-input defaulted to true; prompting would never be attempted")
	}
}

// Two roots must not share state. Globals was made per-root precisely because
// pflag writes defaults into bound pointers at registration time, so a shared
// instance is clobbered the moment a second root is constructed.
func TestGlobals_RootsAreIndependent(t *testing.T) {
	a := NewRootCommand("test")
	b := NewRootCommand("test")

	if globalsFor(a) == globalsFor(b) {
		t.Fatal("two roots share one Globals; parsed flags would leak between them")
	}

	var out bytes.Buffer
	a.SetOut(&out)
	a.SetErr(&out)
	a.SetArgs([]string{"--quiet", "version"})
	if err := a.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if globalsFor(b).Quiet {
		t.Error("--quiet on one root leaked into another")
	}
}
