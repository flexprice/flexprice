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
