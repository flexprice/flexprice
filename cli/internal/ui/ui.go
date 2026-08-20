// Package ui owns every human-facing write: what gets said, to which stream,
// and whether anyone is there to read it.
package ui

import (
	"io"
	"os"

	"golang.org/x/term"

	"github.com/flexprice/cli/internal/style"
)

// Gating inputs are passed in rather than probed so tests can exercise every
// combination without a real terminal. Production callers use FromEnv.
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
		// Gated on stderr, never stdout: redirecting stdout must not strip
		// colour from a stream the user is still watching.
		palette: style.NewPalette(o.Color && o.StderrTTY && o.Term != "dumb"),
		quiet:   o.Quiet,
		// No terminal means nothing can be prompted for, so it implies --no-input.
		noInput: o.NoInput || !o.StdinTTY,
		animate: o.StderrTTY && o.Term != "dumb" && !o.Quiet,
	}
}

// NO_COLOR is applied here rather than in New so Options stays a pure
// description of state, testable without touching the environment.
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

func (u *UI) NoInput() bool { return u.noInput }
func (u *UI) Quiet() bool   { return u.quiet }
