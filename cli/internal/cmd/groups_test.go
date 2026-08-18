package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexprice/cli/internal/spec"
)

// cobra panics at Execute() on an unregistered GroupID — a crash on every
// invocation. Running Execute is the point: it is the path that panics.
func TestRootHelp_DoesNotPanicOnGroupIDs(t *testing.T) {
	root := NewRootCommand("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("help produced no output")
	}
}

// Every resource the registry knows about must have a group, or it silently
// drifts into "Additional Commands" and the taxonomy rots.
func TestEveryResourceHasAGroup(t *testing.T) {
	doc, err := spec.Load()
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	reg, err := spec.NewRegistry(doc)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	var missing []string
	for _, r := range reg.Resources() {
		if _, ok := resourceGroups[r]; !ok {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		t.Errorf("resources with no group in groups.go: %s\n"+
			"Add each to resourceGroups with a group ID and a one-line description.",
			strings.Join(missing, ", "))
	}
}

// The reverse of the above: a stale entry naming a resource that no longer
// exists is dead weight that makes the table harder to trust.
func TestNoStaleResourceGroups(t *testing.T) {
	doc, err := spec.Load()
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	reg, err := spec.NewRegistry(doc)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	known := map[string]bool{}
	for _, r := range reg.Resources() {
		known[r] = true
	}
	for resource := range resourceGroups {
		if !known[resource] {
			t.Errorf("groups.go lists %q, which the registry does not know about", resource)
		}
	}
}

// Every group ID referenced by either table must be one that gets registered,
// or Execute panics.
func TestEveryGroupIDIsRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, g := range commandGroups {
		registered[g.ID] = true
	}
	for resource, entry := range resourceGroups {
		if !registered[entry.GroupID] {
			t.Errorf("resource %q references unregistered group %q — this panics at Execute()",
				resource, entry.GroupID)
		}
	}
	for name, id := range builtinGroups {
		if !registered[id] {
			t.Errorf("builtin %q references unregistered group %q — this panics at Execute()",
				name, id)
		}
	}
}

// Every resource needs a real description; "Operations on x" was the old
// zero-information default and must not come back.
func TestEveryResourceHasADescription(t *testing.T) {
	for resource, entry := range resourceGroups {
		if strings.TrimSpace(entry.Short) == "" {
			t.Errorf("resource %q has an empty description", resource)
		}
		if strings.HasPrefix(entry.Short, "Operations on") {
			t.Errorf("resource %q still uses the placeholder description %q", resource, entry.Short)
		}
	}
}

// The fallback stays for resources added later, but anything we ship reaching
// it is a wiring bug — raw get/post/delete once did.
func TestRootHelp_NothingFallsIntoAdditionalCommands(t *testing.T) {
	root := NewRootCommand("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(buf.String(), "Additional Commands") {
		return
	}

	// Name the offenders rather than just failing: the fix is one line in
	// groups.go, but only if you know which command needs it.
	var ungrouped []string
	for _, c := range root.Commands() {
		if c.GroupID == "" && !c.Hidden {
			ungrouped = append(ungrouped, c.Name())
		}
	}
	t.Errorf("these commands have no group and fell into \"Additional Commands\": %s",
		strings.Join(ungrouped, ", "))
}

// The first thing a new user sees, pinned so a regression is visible in review.
func TestRootHelp_Golden(t *testing.T) {
	root := NewRootCommand("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	golden := filepath.Join("testdata", "root_help.golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (regenerate with UPDATE_GOLDEN=1 go test ./internal/cmd/ -run Golden): %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(buf.Bytes()), bytes.TrimSpace(want)) {
		t.Errorf("root help changed.\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
}
