package cmd

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/flexprice/cli/internal/spec"
	"github.com/flexprice/cli/internal/ui"
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

// exec.LookPath only inspects PATH, so a missing editor is a clean, immediate
// error rather than a hang.
func TestResolveEditor_ReturnsCleanErrorWhenNoEditorIsAvailable(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	t.Setenv("PATH", t.TempDir()) // a PATH with neither vi nor notepad on it

	editor, err := resolveEditor()
	if err == nil {
		t.Fatalf("want an error when no editor is configured or found on PATH, got editor=%q", editor)
	}
}

// EDITOR commonly carries flags (EDITOR="code --wait"), and exec.Command
// needs the binary and arguments split apart.
func TestSplitEditorCommand_SplitsCommandAndArgs(t *testing.T) {
	tests := []struct {
		raw      string
		wantCmd  string
		wantArgs []string
	}{
		{"code --wait", "code", []string{"--wait"}},
		{"subl -w", "subl", []string{"-w"}},
		{"vim -u NONE", "vim", []string{"-u", "NONE"}},
		{"vi", "vi", nil},
		{"", "", nil},
	}
	for _, tt := range tests {
		gotCmd, gotArgs := splitEditorCommand(tt.raw)
		if gotCmd != tt.wantCmd {
			t.Errorf("splitEditorCommand(%q) cmd = %q, want %q", tt.raw, gotCmd, tt.wantCmd)
		}
		if !reflect.DeepEqual(gotArgs, tt.wantArgs) && !(len(gotArgs) == 0 && len(tt.wantArgs) == 0) {
			t.Errorf("splitEditorCommand(%q) args = %v, want %v", tt.raw, gotArgs, tt.wantArgs)
		}
	}
}

func TestResolveEditor_ThenSplit_HandlesEditorConfiguredWithArgs(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "code --wait")

	editor, err := resolveEditor()
	if err != nil {
		t.Fatalf("resolveEditor: %v", err)
	}

	cmd, args := splitEditorCommand(editor)
	if cmd != "code" || !reflect.DeepEqual(args, []string{"--wait"}) {
		t.Errorf("cmd=%q args=%v, want cmd=%q args=[--wait]", cmd, args, "code")
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

// Cut splits on the first "=" only, so an embedded "--" stays part of the value
// rather than being read as a second flag.
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

// Documents a boundary rather than a desirable behaviour: in `--key value` form
// a value starting with "--" is dropped, since it is indistinguishable from the
// next flag. --key=value has no such limit.
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

// finalizeInvoice is tagged x-scope: delete in the real spec, so it must
// require confirmation like delete/void/terminate.
func TestDestructiveActions_IncludeFinalize(t *testing.T) {
	if !destructive["finalize"] {
		t.Error(`destructive["finalize"] = false, want true`)
	}
}

// The old TestPromptConfirm_* cases tested the fmt.Fscanln reader huh replaced;
// their surviving concern is now TestConfirmTitle_NamesTheActionAndSubject.

func TestConfirmAction_SkipsPromptWhenForced(t *testing.T) {
	// force=true must return immediately without prompting at all.
	g := &Globals{UI: ui.New(ui.Options{StderrTTY: true, StdinTTY: false, Term: "dumb"})}
	if err := confirmAction(g, "delete", "cust_123", true); err != nil {
		t.Errorf("confirmAction with force=true: %v", err)
	}
}

// BEHAVIOUR CHANGE: this previously asserted a non-terminal stdin bypassed
// confirmation and proceeded. It now refuses and names --force.
func TestConfirmAction_RefusesWhenStdinIsNotATerminal(t *testing.T) {
	g := &Globals{UI: ui.New(ui.Options{StderrTTY: true, StdinTTY: false, Term: "dumb"})}
	err := confirmAction(g, "delete", "cust_123", false)
	if err == nil {
		t.Fatal("confirmAction proceeded without confirmation on a non-terminal stdin")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("refusal must name --force, got %q", err)
	}
}

// A non-destructive action must never prompt, regardless of stdin.
func TestConfirm_NonDestructiveActionNeverPrompts(t *testing.T) {
	g := &Globals{UI: ui.New(ui.Options{StderrTTY: true, StdinTTY: false, Term: "dumb"})}
	if err := confirm(g, spec.Command{Action: "list", Resource: "customers"}, "", false); err != nil {
		t.Errorf("list should never require confirmation, got %v", err)
	}
}

// Every destructive action must actually reach the confirmation path.
func TestConfirm_EveryDestructiveActionPrompts(t *testing.T) {
	g := &Globals{UI: ui.New(ui.Options{StderrTTY: true, StdinTTY: false, Term: "dumb"})}
	for action := range destructive {
		err := confirm(g, spec.Command{Action: action, Resource: "invoices"}, "inv_1", false)
		if err == nil {
			t.Errorf("destructive action %q was not confirmed", action)
		}
	}
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
