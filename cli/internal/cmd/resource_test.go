package cmd

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/flexprice/cli/internal/spec"
)

type cobraCommand = cobra.Command

func TestResourceCommands_AreRegisteredAtTopLevel(t *testing.T) {
	root := NewRootCommand("test")

	var names []string
	for _, c := range root.Commands() {
		names = append(names, c.Name())
	}
	for _, want := range []string{"customers", "invoices", "subscriptions"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("resource %q not registered at top level; have %v", want, names)
		}
	}
}

func TestResourceCommand_ExposesItsActions(t *testing.T) {
	root := NewRootCommand("test")

	var customers *cobraCommand
	for _, c := range root.Commands() {
		if c.Name() == "customers" {
			customers = c
		}
	}
	if customers == nil {
		t.Fatal("customers command missing")
	}

	var actions []string
	for _, a := range customers.Commands() {
		actions = append(actions, a.Name())
	}
	for _, want := range []string{"list", "retrieve", "create"} {
		found := false
		for _, a := range actions {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("action %q missing from customers; have %v", want, actions)
		}
	}
}

// Webhook Events stubs are documentation, not endpoints. Design doc §5.
func TestResourceCommands_ExcludeWebhookEventStubs(t *testing.T) {
	root := NewRootCommand("test")
	for _, c := range root.Commands() {
		if strings.Contains(c.Name(), "webhook-events") {
			t.Errorf("webhook event stubs became a command: %s", c.Name())
		}
	}
}

func TestRawCommands_AreRegistered(t *testing.T) {
	root := NewRootCommand("test")

	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, want := range []string{"get", "post", "delete", "resources"} {
		if !have[want] {
			t.Errorf("command %q not registered", want)
		}
	}
}

// resolveEditor must never spawn a process to decide whether an editor is
// available: exec.LookPath only inspects PATH, so a missing editor is a clean,
// immediate error rather than a hang waiting on a program that doesn't exist.
func TestResolveEditor_ReturnsCleanErrorWhenNoEditorIsAvailable(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	t.Setenv("PATH", t.TempDir()) // a PATH with neither vi nor notepad on it

	editor, err := resolveEditor()
	if err == nil {
		t.Fatalf("want an error when no editor is configured or found on PATH, got editor=%q", editor)
	}
}

func TestResolveEditor_PrefersVISUALOverEDITOR(t *testing.T) {
	t.Setenv("VISUAL", "my-visual-editor")
	t.Setenv("EDITOR", "my-editor")

	editor, err := resolveEditor()
	if err != nil {
		t.Fatalf("resolveEditor: %v", err)
	}
	if editor != "my-visual-editor" {
		t.Errorf("editor = %q, want VISUAL to take precedence", editor)
	}
}

// collectUnknownFlags parses os.Args by hand because cobra cannot natively
// declare ~30 possible body-field flags per operation. --key=value must not
// mistake "--" inside the value for a second flag: Cut only splits on the
// first "=", so the entire remainder — including embedded "--" — becomes the value.
func TestCollectUnknownFlags_KeyValueForm_PreservesEmbeddedDashDash(t *testing.T) {
	c := newFakeOperationCommand()

	restoreArgs := setArgs(t, "flexprice", "customers", "create", `--metadata={"key":"--not-a-flag"}`)
	defer restoreArgs()

	got := collectUnknownFlags(c)
	want := `{"key":"--not-a-flag"}`
	if got["metadata"] != want {
		t.Errorf("metadata = %q, want %q", got["metadata"], want)
	}
}

// Both --key=value and --key value forms must resolve to the same result.
func TestCollectUnknownFlags_BothKeyValueAndSpaceSeparatedFormsWork(t *testing.T) {
	c := newFakeOperationCommand()

	restoreArgs := setArgs(t, "flexprice", "customers", "create", "--external_id=acme", "--name", "Acme Inc")
	defer restoreArgs()

	got := collectUnknownFlags(c)
	if got["external_id"] != "acme" {
		t.Errorf("external_id = %q, want %q", got["external_id"], "acme")
	}
	if got["name"] != "Acme Inc" {
		t.Errorf("name = %q, want %q", got["name"], "Acme Inc")
	}
}

// A space-separated value that itself looks like a flag (starts with "--") is
// silently dropped rather than captured, because the parser cannot tell a flag's
// value apart from the next flag in that form. --key=value does not have this
// limitation. This test documents the boundary rather than asserting it is
// desirable.
func TestCollectUnknownFlags_SpaceSeparatedValueLookingLikeAFlagIsDropped(t *testing.T) {
	c := newFakeOperationCommand()

	restoreArgs := setArgs(t, "flexprice", "customers", "create", "--name", "--looks-like-a-flag")
	defer restoreArgs()

	got := collectUnknownFlags(c)
	if v, ok := got["name"]; ok {
		t.Errorf("name = %q; expected the space-separated form to drop a flag-shaped value", v)
	}
}

func newFakeOperationCommand() *cobra.Command {
	c := &cobra.Command{Use: "create"}
	c.Flags().String("data", "", "")
	c.Flags().Bool("edit", false, "")
	c.Flags().Bool("force", false, "")
	return c
}

func setArgs(t *testing.T, args ...string) (restore func()) {
	t.Helper()
	orig := os.Args
	os.Args = args
	return func() { os.Args = orig }
}

// pickColumns must read live from commands.yaml's columns: entries, not a
// hardcoded default, and --columns must take precedence when the user passes it.
func TestPickColumns_ReadsLiveFromCommandsYAMLAndHonorsOverride(t *testing.T) {
	doc, err := spec.Load()
	if err != nil {
		t.Fatalf("spec.Load: %v", err)
	}
	reg, err := spec.NewRegistry(doc)
	if err != nil {
		t.Fatalf("spec.NewRegistry: %v", err)
	}

	g := &Globals{}
	want := []string{"id", "external_id", "name", "email", "created_at"}
	got := pickColumns(reg, g, "customers")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("customers columns = %v, want %v (from spec/commands.yaml)", got, want)
	}

	g.Columns = []string{"id", "status"}
	got = pickColumns(reg, g, "customers")
	if !reflect.DeepEqual(got, g.Columns) {
		t.Errorf("--columns override = %v, want %v", got, g.Columns)
	}
}
