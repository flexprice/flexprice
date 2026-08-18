// Package ui owns every human-facing write the CLI makes. It exists because
// the same question — is a human watching this stream right now — was
// previously answered independently at 34 call sites, and mostly not asked at
// all. Centralising it means --quiet, TERM=dumb, CI detection and --no-input
// are each implemented once.
//
// The split with internal/style: style decides what colour something is; ui
// decides what gets said, to which stream, and whether anyone is there to read
// it.
package ui

import (
	"io"
	"os"

	"golang.org/x/term"

	"github.com/flexprice/cli/internal/style"
)

// Options carries the gating inputs explicitly rather than probing the process,
// so tests can exercise every combination without a real terminal. Use FromEnv
// in production.
type Options struct {
	Out, Err  io.Writer
	Quiet     bool
	NoInput   bool
	Color     bool
	StderrTTY bool
	StdinTTY  bool
	Term      string
}

type UI struct {
	out, err io.Writer
	palette  style.Palette
	quiet    bool
	noInput  bool
	animate  bool
}

func New(o Options) *UI {
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Err == nil {
		o.Err = io.Discard
	}
	return &UI{
		out: o.Out,
		err: o.Err,
		// Colour on stderr is gated on stderr, never on stdout: redirecting
		// stdout is a common way to run this CLI and must not strip colour
		// from a stream the user is still watching.
		palette: style.NewPalette(o.Color && o.StderrTTY && o.Term != "dumb"),
		quiet:   o.Quiet,
		// Not a terminal means nothing can be prompted for, so it implies
		// --no-input rather than being a separate condition every prompt has
		// to remember to check.
		noInput: o.NoInput || !o.StdinTTY,
		animate: o.StderrTTY && o.Term != "dumb" && !o.Quiet,
	}
}

// FromEnv builds a UI against the real process streams.
//
// NO_COLOR is applied here rather than in New so that Options stays a pure
// description of the desired state, testable without touching the environment.
// internal/style honours NO_COLOR through its own gate; a ui that did not would
// colour stderr for a user who had explicitly asked it not to.
func FromEnv(quiet, noInput, color bool) *UI {
	return New(Options{
		Out:       os.Stdout,
		Err:       os.Stderr,
		Quiet:     quiet,
		NoInput:   noInput,
		Color:     color && os.Getenv("NO_COLOR") == "",
		StderrTTY: term.IsTerminal(int(os.Stderr.Fd())),
		StdinTTY:  term.IsTerminal(int(os.Stdin.Fd())),
		Term:      os.Getenv("TERM"),
	})
}

// NoInput reports whether prompting is forbidden, for callers that want to pick
// a non-interactive path rather than surface the error from Confirm/Select.
func (u *UI) NoInput() bool { return u.noInput }

// Quiet reports whether commentary is suppressed.
func (u *UI) Quiet() bool { return u.quiet }
