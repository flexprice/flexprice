package ui

import (
	"strings"
	"testing"
)

// Replaces the three TestPromptConfirm_* cases that lived in internal/cmd
// against the old fmt.Fscanln reader. huh now owns reading the answer; the
// wording is still ours, and it is the part that can silently regress.
func TestConfirmTitle_NamesTheActionAndSubject(t *testing.T) {
	got := ConfirmTitle("delete", "/v1/customers/cust_123")
	for _, want := range []string{"delete", "/v1/customers/cust_123", "cannot be undone"} {
		if !strings.Contains(got, want) {
			t.Errorf("confirm title missing %q: %q", want, got)
		}
	}
}

func TestConfirm_RefusesWhenNoInput(t *testing.T) {
	u, _, _ := newTestUI(Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color", NoInput: true})

	err := u.Confirm("delete", "/v1/customers/cust_01")
	if err == nil {
		t.Fatal("Confirm must refuse under --no-input rather than prompting")
	}
	// The message has to name the way out, or the user is stuck.
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("refusal must name --force, got %q", err)
	}
}

func TestConfirm_RefusesWhenStdinIsNotATerminal(t *testing.T) {
	u, _, _ := newTestUI(Options{StderrTTY: true, StdinTTY: false, Term: "xterm-256color"})

	if err := u.Confirm("delete", "/v1/customers/cust_01"); err == nil {
		t.Fatal("Confirm must refuse when stdin is not a terminal")
	}
}

// The refusal must name what would have been destroyed. "refusing to proceed"
// with no subject leaves the operator guessing which command in their script
// stopped.
func TestConfirm_RefusalNamesTheSubject(t *testing.T) {
	u, _, _ := newTestUI(Options{StderrTTY: true, StdinTTY: false, Term: "xterm-256color"})

	err := u.Confirm("void", "/v1/invoices/inv_042")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, want := range []string{"void", "inv_042"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal should name %q, got %q", want, err)
		}
	}
}

func TestSelectWithHint_RefusesWhenNoInputAndNamesTheFlag(t *testing.T) {
	u, _, _ := newTestUI(Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color", NoInput: true})

	// Two options: with one, SelectWithHint returns it without prompting, so a
	// single-option fixture would never reach the refusal path.
	_, err := u.SelectWithHint("Data region", "--region", []Option{
		{Label: "us", Value: "us"},
		{Label: "in", Value: "in"},
	})
	if err == nil {
		t.Fatal("SelectWithHint must refuse under --no-input")
	}
	if !strings.Contains(err.Error(), "--region") {
		t.Errorf("refusal must name the flag to pass instead, got %q", err)
	}
}

// A single option needs no prompt: asking a question with one answer is noise,
// and it makes scripted use fail for no reason.
func TestSelectWithHint_SingleOptionNeedsNoPrompt(t *testing.T) {
	u, _, _ := newTestUI(Options{StderrTTY: true, StdinTTY: false, Term: "dumb"})

	got, err := u.SelectWithHint("Data region", "--region", []Option{{Label: "us", Value: "us"}})
	if err != nil {
		t.Fatalf("single option should resolve without prompting: %v", err)
	}
	if got != "us" {
		t.Errorf("got %q, want %q", got, "us")
	}
}

func TestSelectWithHint_ErrorsWithNoOptions(t *testing.T) {
	u, _, _ := newTestUI(Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color"})

	if _, err := u.SelectWithHint("Data region", "--region", nil); err == nil {
		t.Fatal("expected an error when there is nothing to choose from")
	}
}
