package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/flexprice/cli/internal/style"
)

// newTestUI builds a UI writing into buffers, with every gate explicit.
func newTestUI(o Options) (*UI, *bytes.Buffer, *bytes.Buffer) {
	var out, errBuf bytes.Buffer
	o.Out, o.Err = &out, &errBuf
	return New(o), &out, &errBuf
}

func TestGatingMatrix(t *testing.T) {
	cases := []struct {
		name        string
		opts        Options
		wantAnimate bool
		wantNoInput bool
	}{
		{
			name:        "interactive terminal",
			opts:        Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color"},
			wantAnimate: true,
			wantNoInput: false,
		},
		{
			name:        "CI: stderr not a terminal",
			opts:        Options{StderrTTY: false, StdinTTY: false, Term: "xterm-256color"},
			wantAnimate: false,
			wantNoInput: true,
		},
		{
			name:        "quiet suppresses animation on a real terminal",
			opts:        Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color", Quiet: true},
			wantAnimate: false,
			wantNoInput: false,
		},
		{
			name:        "TERM=dumb suppresses animation",
			opts:        Options{StderrTTY: true, StdinTTY: true, Term: "dumb"},
			wantAnimate: false,
			wantNoInput: false,
		},
		{
			name:        "--no-input on a real terminal",
			opts:        Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color", NoInput: true},
			wantAnimate: true,
			wantNoInput: true,
		},
		{
			name:        "stdout redirected but stderr still a terminal still animates",
			opts:        Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color"},
			wantAnimate: true,
			wantNoInput: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, _, _ := newTestUI(tc.opts)
			if u.animate != tc.wantAnimate {
				t.Errorf("animate = %v, want %v", u.animate, tc.wantAnimate)
			}
			if u.noInput != tc.wantNoInput {
				t.Errorf("noInput = %v, want %v", u.noInput, tc.wantNoInput)
			}
		})
	}
}

// Color is TRUE in most cases below: with Color:false this passes even with
// the stream gates deleted, which was verified directly.
func TestNonTTY_WritesZeroEscapeBytes(t *testing.T) {
	cases := []struct {
		name string
		opts Options
	}{
		{
			name: "stderr is not a terminal (CI, redirected)",
			opts: Options{Color: true, StderrTTY: false, StdinTTY: false, Term: "xterm-256color"},
		},
		{
			name: "TERM=dumb on a real terminal",
			opts: Options{Color: true, StderrTTY: true, StdinTTY: true, Term: "dumb"},
		},
		{
			name: "colour explicitly refused via --no-color",
			opts: Options{Color: false, StderrTTY: true, StdinTTY: true, Term: "xterm-256color"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, out, errBuf := newTestUI(tc.opts)

			u.Info("fetching %s", "customers")
			u.Success("created %s", "cust_01")
			u.Failure(errTest{"boom"})
			u.StatusLine("profile: default")
			u.Data("cust_01")

			for name, buf := range map[string]*bytes.Buffer{"stdout": out, "stderr": errBuf} {
				if strings.Contains(buf.String(), "\x1b") {
					t.Errorf("UI wrote an escape sequence to %s: %q", name, buf.String())
				}
			}
		})
	}
}

// Positive control: without it, every "no escapes" result above could come from
// a UI that never colours anything.
func TestTTY_DoesEmitColorWhenRequested(t *testing.T) {
	style.EnableForTests()
	u, _, errBuf := newTestUI(Options{Color: true, StderrTTY: true, StdinTTY: true, Term: "xterm-256color"})

	u.Success("created cust_01")

	if !strings.Contains(errBuf.String(), "\x1b[") {
		t.Errorf("a colour-enabled UI on a terminal emitted no ANSI codes: %q", errBuf.String())
	}
}

type errTest struct{ msg string }

func (e errTest) Error() string { return e.msg }
