package cmd

import (
	"testing"

	"github.com/flexprice/cli/internal/output"
	"github.com/flexprice/cli/internal/spec"
)

// Replaces TestRender_StatusFooterNotShownForJSON, which lived in
// internal/output until the footer moved to internal/ui. Someone piping json
// or yaml is scripting, not reading a status line.
func TestShouldShowFooter(t *testing.T) {
	cases := []struct {
		format output.Format
		want   bool
		name   string
	}{
		{output.FormatTable, true, "table"},
		{output.FormatJSON, false, "json"},
		{output.FormatYAML, false, "yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldShowFooter(tc.format); got != tc.want {
				t.Errorf("shouldShowFooter(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// Every mutating action needs a verb that is not the vague fallback, or the
// spinner says "Working on customers…" during a create.
func TestSpinnerVerb(t *testing.T) {
	cases := map[string]string{
		"list":      "Fetching",
		"retrieve":  "Fetching",
		"create":    "Creating",
		"update":    "Updating",
		"delete":    "Deleting",
		"void":      "Updating",
		"finalize":  "Finalizing",
		"unknown":   "Working on",
		"terminate": "Updating",
	}
	for action, want := range cases {
		got := spinnerVerb(spec.Command{Action: action})
		if got != want {
			t.Errorf("spinnerVerb(%q) = %q, want %q", action, got, want)
		}
	}
}

// The destructive actions are the ones a user most needs accurate feedback on,
// so none of them may fall through to the vague default.
func TestSpinnerVerb_DestructiveActionsAreNamed(t *testing.T) {
	for action := range destructive {
		if got := spinnerVerb(spec.Command{Action: action}); got == "Working on" {
			t.Errorf("destructive action %q has no specific spinner verb", action)
		}
	}
}
