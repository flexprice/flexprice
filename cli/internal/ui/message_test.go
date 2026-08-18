package ui

import (
	"errors"
	"strings"
	"testing"
)

func TestStreamRouting(t *testing.T) {
	u, out, errBuf := newTestUI(Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color"})

	u.Info("progress")
	u.Success("done")
	u.Failure(errors.New("broken"))
	u.StatusLine("profile: default")
	u.Data("cust_01")

	if got := out.String(); strings.TrimSpace(got) != "cust_01" {
		t.Errorf("stdout must carry only Data, got %q", got)
	}
	for _, want := range []string{"progress", "done", "broken", "profile: default"} {
		if !strings.Contains(errBuf.String(), want) {
			t.Errorf("stderr missing %q, got %q", want, errBuf.String())
		}
	}
}

// Replaces TestRenderTable_StatusFooterGoesToStderr, which lived in
// internal/output until the footer moved here. stdout carries data, and
// `--output json > file.json` must stay clean.
func TestStatusLine_GoesToStderrOnly(t *testing.T) {
	u, out, errBuf := newTestUI(Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color"})

	u.StatusLine("profile: sandbox · region: in")

	if strings.Contains(out.String(), "profile: sandbox") {
		t.Errorf("status footer leaked into stdout: %q", out.String())
	}
	if !strings.Contains(errBuf.String(), "profile: sandbox") {
		t.Errorf("status footer missing from stderr: %q", errBuf.String())
	}
}

// Replaces TestRenderTable_NoStatusFooterWhenEmpty.
func TestStatusLine_SilentWhenEmpty(t *testing.T) {
	u, _, errBuf := newTestUI(Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color"})

	u.StatusLine("")

	if errBuf.Len() != 0 {
		t.Errorf("an empty status line should print nothing, got %q", errBuf.String())
	}
}

// --quiet suppresses commentary but must never suppress the result or a failure.
func TestQuiet_SuppressesCommentaryOnly(t *testing.T) {
	u, out, errBuf := newTestUI(Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color", Quiet: true})

	u.Info("progress")
	u.Success("done")
	u.StatusLine("profile: default")
	u.Data("cust_01")
	u.Failure(errors.New("broken"))

	if strings.Contains(errBuf.String(), "progress") ||
		strings.Contains(errBuf.String(), "done") ||
		strings.Contains(errBuf.String(), "profile: default") {
		t.Errorf("--quiet should suppress commentary, got %q", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "broken") {
		t.Error("--quiet must NOT suppress failures")
	}
	if !strings.Contains(out.String(), "cust_01") {
		t.Error("--quiet must NOT suppress the result")
	}
}
