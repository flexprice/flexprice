package cmd

import (
	"bytes"
	"testing"
)

// Must hold for commands constructed directly, without PersistentPreRunE.
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

// pflag does not populate bound values until Execute parses them, so a UI built
// at construction time would capture --quiet as false regardless.
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

// Not asserting g.UI.NoInput(): stdin is never a TTY under go test, so that
// would hold even unwired. TestGatingMatrix in internal/ui covers it.
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

// A typo in bindGlobals inverting this would make every prompt refuse.
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

// Globals is per-root because pflag writes defaults into bound pointers at
// registration time, clobbering any shared instance.
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
