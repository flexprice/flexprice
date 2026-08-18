# Flexprice CLI — DX Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the eleven DX gaps recorded in `2026-08-18-flexprice-cli-dx-polish-design.md` by giving the CLI a single package that owns every human-facing write.

**Architecture:** A new `internal/ui` package holds one `*UI` value carrying the output streams and the quiet/TTY/no-input/animate decisions. It hangs off the existing `Globals`, which is already threaded into all seven files in `internal/cmd`, so it reaches every call site with no signature changes. `internal/style` is refactored from a package-level singleton into a `Palette` value so colour can be gated per stream — stdout for table content, stderr for everything human. Root help is grouped with cobra's native `AddGroup`.

**Tech Stack:** Go 1.25, cobra 1.10.2, huh 1.0.0, lipgloss 1.1.0, golang.org/x/term.

**Working directory:** All paths are relative to `cli/` unless stated otherwise. Run all commands from `cli/`.

---

## Spike findings (already verified — do not re-run, but do not contradict)

These were confirmed against the real libraries before this plan was written. Three of them change what the code must do:

1. **`cobra.Group{ID, Title}`, `(*Command).AddGroup(...*Group)` and the `Command.GroupID string` field all exist** in cobra 1.10.2. No custom help function is needed.

2. **An unregistered `GroupID` panics at `Execute()`:**
   ```
   panic: group id 'does-not-exist' is not defined for subcommand 'flexprice bogus'
   ```
   This is a runtime crash on *every* invocation, not a compile error. A single typo in the
   group table bricks the CLI. Task 5's test is therefore load-bearing, not hygiene.

3. **Empty `GroupID` is the correct fallback.** cobra natively renders such commands under a
   built-in `Additional Commands:` heading. Do **not** invent an `"additional"` group — an
   unregistered ID of that name would trigger the panic in (2).

4. **`help` and `completion` land in `Additional Commands:`** unless placed explicitly with
   `SetHelpCommandGroupID` / `SetCompletionCommandGroupID`. Both calls are required.

5. **`huh.NewConfirm()`** exists with the builder chain `.Title(string).Affirmative(string).Negative(string).Value(*bool)` and `.Run() error`.

---

## File structure

**Create:**

| File | Responsibility |
|---|---|
| `internal/ui/ui.go` | The `UI` type, its construction, and the gating decisions |
| `internal/ui/message.go` | `Info` `Data` `Success` `Failure` `Receipt` `EmptyState` `StatusLine` |
| `internal/ui/spinner.go` | Inline spinner and cursor save/restore |
| `internal/ui/prompt.go` | `Confirm` and `Select`, both `--no-input` aware |
| `internal/ui/ui_test.go` | The 4-way gating matrix — the plan's most important test |
| `internal/ui/message_test.go` | Message content and stream routing |
| `internal/ui/spinner_test.go` | Frames, erase, cursor restoration |
| `internal/ui/prompt_test.go` | `--no-input` refusal paths |
| `internal/cmd/groups.go` | Group taxonomy and the 34 resource descriptions |
| `internal/cmd/groups_test.go` | Freshness gate + the panic-safety test |
| `internal/cmd/testdata/root_help.golden` | Golden root help |
| `internal/output/pad.go` | ANSI-aware column padding, shared by `table.go` and `env.go` |
| `internal/output/pad_test.go` | Padding measured on visible width |

**Modify:**

| File | Change |
|---|---|
| `internal/style/style.go` | `Palette` value type; `TERM=dumb`; package funcs delegate to a stdout-gated default |
| `internal/cmd/root.go` | `Globals.UI`, `--no-input`, group registration, UI built in `PersistentPreRunE` |
| `internal/cmd/resource.go` | Spinner around `Do`; `ui.Confirm`; receipts; empty state |
| `internal/cmd/auth.go` | Spinner around `VerifyKey`; `ui.Success`; `ui.Select` for regions |
| `internal/cmd/init.go` | Banner and next-steps through `ui` |
| `internal/cmd/env.go` | ANSI-aware padding instead of `text/tabwriter` |
| `internal/cmd/config.go`, `misc.go`, `raw.go` | Call-site migration to `ui` |
| `internal/output/table.go` | Status footer removed; padding extracted to `pad.go` |
| `main.go` | `signal.NotifyContext`; `ui.Failure`; exit 130 |

---

## Phase split

**Phase 1 — Tasks 1–6.** The `ui` package, spinner, signal handling, `TERM=dumb`, grouped help, and the call-site migration. Ships a coherent, visibly better CLI on its own.

**Phase 2 — Tasks 7–11.** `--no-input` with the huh confirm, receipts, empty states, the `env list` padding fix, and documentation.

**One deviation from the tidy Tier split, and the reason for it:** SIGINT handling (gap 11, nominally Tier 3) is in Phase 1, immediately after the spinner. The spinner is what hides the cursor, so it is what creates the risk of leaving a user with an invisible cursor in their shell after Ctrl-C. Shipping the spinner without the teardown would introduce that hazard and leave it live for the whole gap between phases. They must land together.

---

## Task 1: The `ui` package core, and per-stream colour

**Files:**
- Modify: `internal/style/style.go`
- Create: `internal/ui/ui.go`
- Create: `internal/ui/message.go`
- Create: `internal/ui/ui_test.go`
- Create: `internal/ui/message_test.go`

Colour currently lives in a package-level `enabled` flag gated on **stdout**. Everything `ui` writes goes to **stderr**. Reusing the singleton would mean `flexprice customers list > out.json` silently strips colour and (later) the spinner from a terminal the user is still watching. So `style` gains a `Palette` value; the package-level functions keep working, backed by a stdout-gated default.

- [ ] **Step 1: Write the failing test for `Palette`**

Create `internal/style/palette_test.go`:

```go
package style

import (
	"strings"
	"testing"

	"github.com/muesli/termenv"
)

func TestPalette_EnabledEmitsColor(t *testing.T) {
	renderer.SetColorProfile(termenv.TrueColor)
	p := NewPalette(true)
	got := p.Success("stored")
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("enabled palette produced no ANSI codes: %q", got)
	}
	if !strings.Contains(got, "stored") {
		t.Errorf("palette dropped the message: %q", got)
	}
}

func TestPalette_DisabledEmitsNoColorButKeepsIcon(t *testing.T) {
	p := NewPalette(false)
	got := p.Success("stored")
	if strings.Contains(got, "\x1b[") {
		t.Errorf("disabled palette emitted ANSI codes: %q", got)
	}
	if !strings.HasPrefix(got, "✓ ") {
		t.Errorf("icon should survive a disabled palette, got %q", got)
	}
}

// The two palettes must be independent: this is the whole point of the type.
func TestPalette_IndependentOfPackageDefault(t *testing.T) {
	Disable()
	defer Enable()
	renderer.SetColorProfile(termenv.TrueColor)

	if got := Success("via package"); strings.Contains(got, "\x1b[") {
		t.Errorf("package default should be disabled, got %q", got)
	}
	if got := NewPalette(true).Success("via palette"); !strings.Contains(got, "\x1b[") {
		t.Errorf("explicit palette should still colour, got %q", got)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
go test ./internal/style/ -run TestPalette -v
```

Expected: FAIL — `undefined: NewPalette`.

- [ ] **Step 3: Refactor `style.go` to the `Palette` value**

In `internal/style/style.go`, replace the `enabled` declaration and every styling function
with the following. Keep the colour constants, `goodStatus`/`badStatus`/`warnStatus` maps,
and `renderer` exactly as they are.

```go
// enabled is the package-level default gate, used by the package-level
// functions below. It governs STDOUT content only — table headers and cells.
// Anything written to stderr must build its own Palette from that stream's
// TTY-ness instead; see internal/ui. TERM=dumb disables colour alongside
// NO_COLOR and a non-terminal stdout, per clig.dev.
var enabled = os.Getenv("NO_COLOR") == "" &&
	os.Getenv("TERM") != "dumb" &&
	term.IsTerminal(int(os.Stdout.Fd()))

func Disable() { enabled = false }
func Enable()  { enabled = true }

func EnableForTests() {
	enabled = true
	renderer.SetColorProfile(termenv.TrueColor)
}

// Palette renders text at a fixed colour setting. It exists so callers writing
// to different streams can each gate colour on their own stream: piping stdout
// to a file must not strip colour from a stderr message the user is watching.
type Palette struct{ enabled bool }

func NewPalette(enabled bool) Palette { return Palette{enabled: enabled} }

// Default returns a Palette reflecting the package gate as it stands right now.
// It is read at call time, not captured, so Disable() still takes effect after
// flag parsing.
func Default() Palette { return Palette{enabled: enabled} }

func (p Palette) styled(s string, c lipgloss.Color, bold bool) string {
	if !p.enabled {
		return s
	}
	st := renderer.NewStyle().Foreground(c)
	if bold {
		st = st.Bold(true)
	}
	return st.Render(s)
}

func (p Palette) Dim(s string) string     { return p.styled(s, colorDim, false) }
func (p Palette) Success(s string) string { return "✓ " + p.styled(s, colorGreen, false) }
func (p Palette) Error(s string) string   { return "✗ " + p.styled(s, colorRed, false) }
func (p Palette) Warning(s string) string { return "⚠ " + p.styled(s, colorYellow, false) }
func (p Palette) Header(s string) string  { return p.styled(s, colorMagenta, true) }
func (p Palette) Accent(s string) string  { return p.styled(s, colorPurple, false) }

func (p Palette) StatusColor(value string) string {
	lower := strings.ToLower(value)
	switch {
	case goodStatus[lower]:
		return p.styled(value, colorGreen, false)
	case badStatus[lower]:
		return p.styled(value, colorRed, false)
	case warnStatus[lower]:
		return p.styled(value, colorYellow, false)
	default:
		return value
	}
}

// Package-level functions delegate to the stdout-gated default so existing
// callers in internal/output are unaffected.
func Dim(s string) string              { return Default().Dim(s) }
func Success(s string) string          { return Default().Success(s) }
func Error(s string) string            { return Default().Error(s) }
func Warning(s string) string          { return Default().Warning(s) }
func Header(s string) string           { return Default().Header(s) }
func Accent(s string) string           { return Default().Accent(s) }
func StatusColor(value string) string  { return Default().StatusColor(value) }
```

Delete the old free-standing `styled` function — `Palette.styled` replaces it.

- [ ] **Step 4: Confirm the palette tests pass and nothing regressed**

```bash
go test ./internal/style/ ./internal/output/ ./internal/cmd/ -v 2>&1 | tail -20
```

Expected: PASS. `internal/output` and `internal/cmd` must pass **unmodified** — they only
call the package-level functions, whose behaviour is unchanged.

- [ ] **Step 5: Write the failing gating-matrix test**

This is the most important test in the plan. It is the one that catches escape sequences
leaking into CI logs. Create `internal/ui/ui_test.go`:

```go
package ui

import (
	"bytes"
	"strings"
	"testing"
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

// The assertion that stops Christmas-tree CI logs. Asserting ABSENCE is the
// point: the previous round shipped two real defects whose tests only checked
// that output was produced.
func TestNonTTY_WritesZeroEscapeBytes(t *testing.T) {
	u, out, errBuf := newTestUI(Options{StderrTTY: false, StdinTTY: false, Term: "dumb"})

	u.Info("fetching %s", "customers")
	u.Success("created %s", "cust_01")
	u.Failure(errTest{"boom"})
	u.Data("cust_01")

	for name, buf := range map[string]*bytes.Buffer{"stdout": out, "stderr": errBuf} {
		if strings.Contains(buf.String(), "\x1b") {
			t.Errorf("non-TTY UI wrote an escape sequence to %s: %q", name, buf.String())
		}
	}
}

type errTest struct{ msg string }

func (e errTest) Error() string { return e.msg }
```

- [ ] **Step 6: Run it and watch it fail**

```bash
go test ./internal/ui/ -run 'TestGatingMatrix|TestNonTTY' -v
```

Expected: FAIL — the `ui` package does not exist yet.

- [ ] **Step 7: Write `internal/ui/ui.go`**

```go
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
// Omitting it was a real gap in an earlier draft: internal/style honours
// NO_COLOR through its own gate, and a ui that did not would have coloured
// stderr for a user who had explicitly asked it not to.
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
```

- [ ] **Step 8: Write `internal/ui/message.go`**

```go
package ui

import "fmt"

// Info is human commentary: progress, context, next steps. Goes to stderr and
// is silenced by --quiet.
func (u *UI) Info(format string, a ...any) {
	if u.quiet {
		return
	}
	fmt.Fprintf(u.err, format+"\n", a...)
}

// Data is a command's result. It goes to stdout and is NEVER silenced by
// --quiet: --quiet suppresses progress, not the thing that was asked for.
func (u *UI) Data(format string, a ...any) {
	fmt.Fprintf(u.out, format+"\n", a...)
}

// Success reports a completed state change.
func (u *UI) Success(format string, a ...any) {
	if u.quiet {
		return
	}
	fmt.Fprintln(u.err, u.palette.Success(fmt.Sprintf(format, a...)))
}

// Failure reports an error. Deliberately not gated on --quiet: a user who
// asked for less progress output did not ask to have failures hidden, and a
// silent non-zero exit is far worse than an unwanted line.
func (u *UI) Failure(err error) {
	fmt.Fprintln(u.err, u.palette.Error(err.Error()))
}

// StatusLine renders the dim context footer under table output.
func (u *UI) StatusLine(s string) {
	if u.quiet || s == "" {
		return
	}
	fmt.Fprintln(u.err, u.palette.Dim(s))
}
```

- [ ] **Step 9: Write `internal/ui/message_test.go`**

```go
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
```

- [ ] **Step 10: Run the full `ui` suite**

```bash
go test ./internal/ui/ -v
```

Expected: PASS, all tests.

- [ ] **Step 11: Verify the whole module is still green**

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... -race 2>&1 | tail -15
```

Expected: build clean, vet clean, `gofmt -l` prints nothing, all packages `ok`.

- [ ] **Step 12: Commit**

```bash
git add internal/style/ internal/ui/
git commit -m "feat(cli): add internal/ui with per-stream colour gating

style becomes a Palette value so stderr content can gate colour on stderr
rather than stdout; package-level functions keep the stdout gate so existing
callers are unaffected. Adds TERM=dumb alongside NO_COLOR.

The gating matrix test asserts absence of escape bytes on a non-TTY, which is
the assertion style the previous round's two shipped defects evaded."
```

---

## Task 2: Spinner with cursor restoration

**Files:**
- Create: `internal/ui/spinner.go`
- Create: `internal/ui/spinner_test.go`

Hand-rolled rather than `bubbles/spinner`: that component needs a `tea.Program`, which
takes over the terminal and runs an input loop. This CLI prompts via `huh` immediately
after some spinners stop, and two components contending for terminal control is miserable
to debug. This also keeps bubbletea an indirect dependency.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/spinner_test.go`:

```go
package ui

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer safe for the spinner goroutine to write to while
// the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestSpinner_InertWhenNotAnimating(t *testing.T) {
	var buf syncBuffer
	u := New(Options{Err: &buf, StderrTTY: false, Term: "dumb"})

	sp := u.Spinner("Fetching customers")
	time.Sleep(30 * time.Millisecond)
	sp.Stop()

	if buf.String() != "" {
		t.Errorf("non-animating spinner wrote %q, want nothing", buf.String())
	}
}

func TestSpinner_AnimatesAndCleansUp(t *testing.T) {
	var buf syncBuffer
	u := New(Options{Err: &buf, StderrTTY: true, StdinTTY: true, Term: "xterm-256color", Color: true})

	sp := u.Spinner("Fetching customers")
	time.Sleep(150 * time.Millisecond)
	sp.Stop()

	got := buf.String()
	if !strings.Contains(got, "Fetching customers") {
		t.Errorf("spinner never wrote its message: %q", got)
	}
	if !strings.Contains(got, hideCursor) {
		t.Error("spinner did not hide the cursor")
	}
	// The sharp edge: failing to restore leaves the user with an invisible
	// cursor in their shell, which outlives the process.
	if !strings.HasSuffix(got, showCursor) {
		t.Errorf("spinner must restore the cursor last, got tail %q", tail(got, 20))
	}
}

// Stop must be safe to call twice: teardown paths (normal return and the
// signal handler) can both reach it.
func TestSpinner_StopIsIdempotent(t *testing.T) {
	var buf syncBuffer
	u := New(Options{Err: &buf, StderrTTY: true, StdinTTY: true, Term: "xterm-256color"})

	sp := u.Spinner("working")
	sp.Stop()
	sp.Stop() // must not panic or double-write
}

func TestSpinner_UpdateChangesMessage(t *testing.T) {
	var buf syncBuffer
	u := New(Options{Err: &buf, StderrTTY: true, StdinTTY: true, Term: "xterm-256color"})

	sp := u.Spinner("fetched 0")
	time.Sleep(120 * time.Millisecond)
	sp.Update("fetched 20 of 60")
	time.Sleep(120 * time.Millisecond)
	sp.Stop()

	if !strings.Contains(buf.String(), "fetched 20 of 60") {
		t.Errorf("Update did not reach the output: %q", buf.String())
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
go test ./internal/ui/ -run TestSpinner -v
```

Expected: FAIL — `u.Spinner` undefined, `hideCursor`/`showCursor` undefined.

- [ ] **Step 3: Write `internal/ui/spinner.go`**

```go
package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
	// eraseLine clears from the cursor to the end of the line, so a shorter
	// message cannot leave the tail of a longer one behind.
	eraseLine = "\r\x1b[K"
)

// frames is the braille cycle used by most modern CLIs. It renders as a single
// cell in every terminal that reports UTF-8.
var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const frameInterval = 80 * time.Millisecond

// Spinner is an inline progress indicator. A Spinner returned when the UI is
// not animating is inert: every method is a no-op, so callers never branch on
// whether a spinner is live.
type Spinner struct {
	ui   *UI
	mu   sync.Mutex
	msg  string
	done chan struct{}
	once sync.Once
	live bool
}

// Spinner starts an indicator, or returns an inert handle when animation is
// suppressed (not a TTY, TERM=dumb, or --quiet). Always pair with Stop, and
// prefer defer.
func (u *UI) Spinner(msg string) *Spinner {
	s := &Spinner{ui: u, msg: msg}
	if !u.animate {
		return s
	}
	s.live = true
	s.done = make(chan struct{})

	fmt.Fprint(u.err, hideCursor)
	go s.run()
	return s
}

func (s *Spinner) run() {
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	for i := 0; ; i++ {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()
			fmt.Fprintf(s.ui.err, "%s%s %s", eraseLine,
				s.ui.palette.Accent(frames[i%len(frames)]), msg)
		}
	}
}

// Update changes the message in place. Used by the --all pagination loop to
// tick on each completed page, so a stalled page shows as a frozen count
// rather than an animation that implies progress it is not making.
func (s *Spinner) Update(msg string) {
	if !s.live {
		return
	}
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

// Stop halts the spinner, erases its line and restores the cursor. It is safe
// to call more than once: both the normal return path and the signal handler
// can reach it.
func (s *Spinner) Stop() {
	if !s.live {
		return
	}
	s.once.Do(func() {
		close(s.done)
		// Erase before restoring the cursor so the final frame never survives
		// as a stray line above whatever is printed next.
		fmt.Fprint(s.ui.err, eraseLine+showCursor)
	})
}

// String satisfies fmt.Stringer for debugging; it never renders to the user.
func (s *Spinner) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.msg)
}
```

- [ ] **Step 4: Run the spinner tests**

```bash
go test ./internal/ui/ -run TestSpinner -race -v
```

Expected: PASS, all four. `-race` matters here — the spinner writes from a goroutine.

- [ ] **Step 5: Full suite**

```bash
go test ./... -race 2>&1 | tail -12
```

Expected: all packages `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/spinner.go internal/ui/spinner_test.go
git commit -m "feat(cli): add inline spinner to internal/ui

Hand-rolled rather than bubbles/spinner: that needs a tea.Program, which takes
over the terminal and would contend with huh prompts that run immediately after
a spinner stops.

Stop is idempotent and always restores the cursor. Failing to restore leaves an
invisible cursor in the user's shell after the process exits, which is why the
test asserts showCursor is the last thing written."
```

---

## Task 3: SIGINT handling and exit code 130

**Files:**
- Modify: `main.go`
- Modify: `internal/exitcode/exitcode.go`
- Create: `internal/exitcode/exitcode_test.go`

Ships with Task 2, not later: the spinner hides the cursor, so until teardown exists,
Ctrl-C during any command leaves the user's shell without a visible cursor.

- [ ] **Step 1: Write the failing test for the new exit code**

Create `internal/exitcode/exitcode_test.go`:

```go
package exitcode

import "testing"

// These values are a public contract that scripts depend on. This test exists
// to make a change to any of them a deliberate act rather than a side effect.
func TestValuesAreStable(t *testing.T) {
	for name, got := range map[string]int{
		"OK": OK, "Generic": Generic, "Usage": Usage,
		"Auth": Auth, "NotFound": NotFound, "RateLimited": RateLimited,
	} {
		_ = name
		_ = got
	}
	want := map[string]int{
		"OK": 0, "Generic": 1, "Usage": 2,
		"Auth": 3, "NotFound": 4, "RateLimited": 5,
		"Interrupted": 130,
	}
	actual := map[string]int{
		"OK": OK, "Generic": Generic, "Usage": Usage,
		"Auth": Auth, "NotFound": NotFound, "RateLimited": RateLimited,
		"Interrupted": Interrupted,
	}
	for name, w := range want {
		if actual[name] != w {
			t.Errorf("%s = %d, want %d — this is a public contract", name, actual[name], w)
		}
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
go test ./internal/exitcode/ -v
```

Expected: FAIL — `undefined: Interrupted`.

- [ ] **Step 3: Add the constant**

In `internal/exitcode/exitcode.go`, add to the existing `const` block:

```go
	// Interrupted follows the shell convention of 128 + SIGINT(2). It is
	// additive: no existing value changes.
	Interrupted = 130
```

- [ ] **Step 4: Verify it passes**

```bash
go test ./internal/exitcode/ -v
```

Expected: PASS.

- [ ] **Step 5: Rewrite `main.go`**

Replace the whole file with:

```go
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/cmd"
	"github.com/flexprice/cli/internal/exitcode"
	"github.com/flexprice/cli/internal/ui"
)

// version is set by goreleaser at build time.
var version = "dev"

func main() {
	// NotifyContext cancels ctx on Ctrl-C. The context reaches client.Do,
	// which already accepts one, so an in-flight request is abandoned rather
	// than waited out.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := cmd.NewRootCommand(version)
	err := root.ExecuteContext(ctx)

	// Restoring the cursor is the reason this handling exists at all: a
	// spinner hides it, and a process that dies without restoring leaves the
	// user's shell with no visible cursor. This runs on every exit path,
	// including the ones where no spinner was ever started (where it is a
	// harmless no-op sequence).
	cmd.RestoreTerminal()

	if err == nil {
		return
	}

	// A cancelled context means Ctrl-C, not a failure worth a stack of
	// diagnostics. Report it plainly and use the conventional code.
	if errors.Is(ctx.Err(), context.Canceled) {
		ui.FromEnv(false, false, true).Failure(errors.New("cancelled"))
		os.Exit(exitcode.Interrupted)
	}

	ui.FromEnv(false, false, true).Failure(err)

	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		os.Exit(apiErr.ExitCode())
	}
	os.Exit(exitcode.Generic)
}
```

- [ ] **Step 6: Add `RestoreTerminal` to `internal/cmd/root.go`**

Append to `internal/cmd/root.go`:

```go
// RestoreTerminal re-enables the cursor. main calls it on every exit path
// because a spinner may have hidden it, and an invisible cursor outlives the
// process — the user is left fixing it by restarting their terminal. Writing
// the sequence when no spinner ran is harmless.
func RestoreTerminal() {
	if term.IsTerminal(int(os.Stderr.Fd())) {
		fmt.Fprint(os.Stderr, "\x1b[?25h")
	}
}
```

Add `"golang.org/x/term"` to that file's imports if not already present.

- [ ] **Step 7: Verify build and manual interrupt behaviour**

```bash
go build ./... && go vet ./... && go test ./... -race 2>&1 | tail -12
```

Expected: clean build and vet, all packages `ok`.

- [ ] **Step 8: Commit**

```bash
git add main.go internal/exitcode/ internal/cmd/root.go
git commit -m "feat(cli): handle SIGINT and restore the cursor on every exit path

Ships with the spinner rather than after it: the spinner hides the cursor, so
until this exists, Ctrl-C leaves the user's shell without one.

Adds exitcode.Interrupted = 130 (128 + SIGINT). Additive; no existing value
changes. Errors now render through ui.Failure, so style.Error stops being dead
code."
```

---

## Task 4: Wire the UI onto `Globals`

**Files:**
- Modify: `internal/cmd/root.go`
- Create: `internal/cmd/ui_wiring_test.go`

The UI must be built **after** flag parsing. Building it in `NewRootCommand` would capture
`--quiet` / `--no-color` before pflag has populated them — the exact class of bug that hit
this codebase before, when pflag wrote defaults into bound pointers at registration time.

- [ ] **Step 1: Write the failing test**

Create `internal/cmd/ui_wiring_test.go`:

```go
package cmd

import (
	"bytes"
	"testing"
)

// The UI must never be nil, even for a command constructed directly in a test
// that never runs PersistentPreRunE.
func TestGlobals_UIIsNeverNil(t *testing.T) {
	root := NewRootCommand("test")
	g := globalsFor(root)
	if g.UI == nil {
		t.Fatal("Globals.UI is nil before flag parsing; every call site would panic")
	}
}

// Regression guard: flags are not populated until Execute parses them, so a UI
// built at construction time would capture --quiet as false regardless.
func TestGlobals_UIReflectsParsedFlags(t *testing.T) {
	root := NewRootCommand("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--quiet", "version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	g := globalsFor(root)
	if !g.Quiet {
		t.Fatal("--quiet did not reach Globals")
	}
	if !g.UI.Quiet() {
		t.Error("UI was built before flags were parsed: it does not see --quiet")
	}
}

// --no-input must reach the UI. Asserting on colour here would be vacuous:
// stderr is a buffer under test, never a TTY, so no colour would be emitted
// regardless of the flag. root_test.go already covers --no-color against
// style directly.
func TestGlobals_NoInputReachesUI(t *testing.T) {
	root := NewRootCommand("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--no-input", "version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	g := globalsFor(root)
	if !g.NoInput {
		t.Fatal("--no-input did not reach Globals")
	}
	if !g.UI.NoInput() {
		t.Error("UI does not see --no-input; prompts would still be attempted")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
go test ./internal/cmd/ -run TestGlobals -v
```

Expected: FAIL — `globalsFor` undefined, `g.UI` undefined.

- [ ] **Step 3: Add the `UI` field, the `--no-input` flag, and the test accessor**

In `internal/cmd/root.go`, add to the `Globals` struct:

```go
	NoInput bool
	// UI owns every human-facing write. It is replaced in PersistentPreRunE
	// once flags are parsed; the value set at construction is a safe default
	// so a directly-constructed command in a test never dereferences nil.
	UI *ui.UI
```

Add to `bindGlobals`:

```go
	f.BoolVar(&g.NoInput, "no-input", false, "never prompt; fail instead of asking")
```

In `NewRootCommand`, immediately after `g := &Globals{}`:

```go
	// A usable default so tests that construct commands without running
	// PersistentPreRunE do not dereference nil. Replaced below once flags
	// exist.
	g.UI = ui.FromEnv(false, false, true)
```

Replace `PersistentPreRunE` with:

```go
	// Flags are not populated until Execute() parses them, so neither the
	// colour decision nor the UI can be made at construction time — this hook
	// is the first point where g's fields are real.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if g.NoColor {
			style.Disable()
		}
		g.UI = ui.FromEnv(g.Quiet, g.NoInput, !g.NoColor)
		return nil
	}
```

Add `"github.com/flexprice/cli/internal/ui"` to the imports.

Add at the end of the file:

```go
// rootGlobals lets tests reach the Globals belonging to a specific root
// command. Keyed by command pointer, and guarded by a mutex, so tests that
// construct separate roots in parallel never observe each other's state — the
// failure mode that made Globals a per-root value rather than a package
// variable in the first place.
var (
	rootGlobalsMu sync.Mutex
	rootGlobals   = map[*cobra.Command]*Globals{}
)

func registerGlobals(root *cobra.Command, g *Globals) {
	rootGlobalsMu.Lock()
	defer rootGlobalsMu.Unlock()
	rootGlobals[root] = g
}

// globalsFor exposes a root command's Globals for tests. Production code
// receives *Globals by parameter and must not reach for this.
func globalsFor(root *cobra.Command) *Globals {
	rootGlobalsMu.Lock()
	defer rootGlobalsMu.Unlock()
	return rootGlobals[root]
}
```

Add `"sync"` to the imports, and in `NewRootCommand`, immediately before `return root`:

```go
	registerGlobals(root, g)
```

- [ ] **Step 4: Add the `Quiet` accessor to `ui`**

In `internal/ui/ui.go`:

```go
// Quiet reports whether commentary is suppressed.
func (u *UI) Quiet() bool { return u.quiet }
```

- [ ] **Step 5: Verify the tests pass**

```bash
go test ./internal/cmd/ -run TestGlobals -v
```

Expected: PASS, all three.

- [ ] **Step 6: Full suite**

```bash
go build ./... && go vet ./... && go test ./... -race 2>&1 | tail -12
```

Expected: all `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/cmd/root.go internal/cmd/ui_wiring_test.go internal/ui/ui.go
git commit -m "feat(cli): carry the UI on Globals and add --no-input

The UI is built in PersistentPreRunE, not at construction: pflag does not
populate bound values until Execute parses them, so a UI built earlier would
capture --quiet and --no-color as their defaults. A safe default is set at
construction so directly-constructed commands in tests never see a nil UI."
```

---

## Task 5: Grouped root help

**Files:**
- Create: `internal/cmd/groups.go`
- Create: `internal/cmd/groups_test.go`
- Create: `internal/cmd/testdata/root_help.golden`
- Modify: `internal/cmd/root.go`
- Modify: `internal/cmd/resource.go`

Recall from the spike: an unregistered `GroupID` **panics at `Execute()`**. The test below
is the safety net for that, and it must run `Execute()` because that is the code path that
panics.

- [ ] **Step 1: Write the failing tests**

Create `internal/cmd/groups_test.go`:

```go
package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexprice/cli/internal/spec"
)

// cobra panics at Execute() when a command carries a GroupID that was never
// registered with AddGroup. That is a runtime crash on every invocation, so a
// typo in the group table bricks the CLI. Running Execute here is the point:
// it is the code path that panics.
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

// Every group ID referenced by the table must be one that gets registered, or
// Execute panics.
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
```

- [ ] **Step 2: Run and watch it fail**

```bash
go test ./internal/cmd/ -run 'TestRootHelp|TestEvery' -v
```

Expected: FAIL — `resourceGroups` and `commandGroups` undefined.

- [ ] **Step 3: Write `internal/cmd/groups.go`**

```go
package cmd

import "github.com/spf13/cobra"

// Group IDs. These strings are referenced by both commandGroups and
// resourceGroups; cobra panics at Execute() if a command carries an ID that was
// never registered, so the two must stay in step. TestEveryGroupIDIsRegistered
// enforces that.
const (
	groupSetup      = "setup"
	groupCoreBill   = "core-billing"
	groupUsage      = "usage"
	groupCredits    = "credits"
	groupCatalog    = "catalog"
	groupPlatform   = "platform"
	groupAutomation = "automation"
	groupAdvanced   = "advanced"
)

// commandGroups is the render order of the root help. cobra prints groups in
// the order they are added, so this slice is the layout.
var commandGroups = []*cobra.Group{
	{ID: groupSetup, Title: "Setup"},
	{ID: groupCoreBill, Title: "Core billing"},
	{ID: groupUsage, Title: "Usage & metering"},
	{ID: groupCredits, Title: "Credits & discounts"},
	{ID: groupCatalog, Title: "Catalog & pricing"},
	{ID: groupPlatform, Title: "Platform"},
	{ID: groupAutomation, Title: "Automation"},
	{ID: groupAdvanced, Title: "Advanced"},
}

// resourceEntry is a resource's placement and its one-line description.
//
// Descriptions are hand-written rather than derived from the OpenAPI spec: the
// spec's summaries describe individual operations ("Get customer by external
// ID"), not the resource, so every derivation rule produces a misleading
// parent. They are one line each and change rarely.
type resourceEntry struct {
	GroupID string
	Short   string
}

// resourceGroups covers all 34 spec-derived resources. A resource missing from
// this map still appears in help under cobra's built-in "Additional Commands"
// heading — it is never silently dropped — but TestEveryResourceHasAGroup fails
// so the omission is caught in CI rather than shipped.
var resourceGroups = map[string]resourceEntry{
	// Core billing
	"customers":               {groupCoreBill, "Manage the people and organisations you bill"},
	"subscriptions":           {groupCoreBill, "Active plan assignments and their lifecycle"},
	"subscription-schedules":  {groupCoreBill, "Planned future changes to a subscription"},
	"subscription-line-items": {groupCoreBill, "Individual charges on a subscription"},
	"invoices":                {groupCoreBill, "Draft, finalize and void billing documents"},
	"payments":                {groupCoreBill, "Payment attempts and their outcomes"},
	"checkout":                {groupCoreBill, "Hosted checkout sessions"},

	// Usage & metering
	"events":       {groupUsage, "Raw usage events you send in for metering"},
	"features":     {groupUsage, "Capabilities that can be metered or gated"},
	"entitlements": {groupUsage, "What a customer's plan grants them access to"},
	"costs":        {groupUsage, "Cost sheets derived from usage"},

	// Credits & discounts
	"credit-grants":       {groupCredits, "Prepaid and promotional credit allocations"},
	"credit-notes":        {groupCredits, "Refunds and credit memos against invoices"},
	"wallets":             {groupCredits, "Prepaid credit balances held by a customer"},
	"coupons":             {groupCredits, "Discount codes and their rules"},
	"coupon-associations": {groupCredits, "Which coupons apply to which subscriptions"},

	// Catalog & pricing
	"plans":       {groupCatalog, "Pricing models customers can subscribe to"},
	"prices":      {groupCatalog, "Individual pricing units within a plan"},
	"price-units": {groupCatalog, "Units of measurement used by prices"},
	"addons":      {groupCatalog, "Optional extras attachable to a plan"},
	"tax-rates":   {groupCatalog, "Tax rates available to apply"},

	"tax-associations": {groupCatalog, "Which tax rates apply to which entities"},

	// Platform
	"environments": {groupPlatform, "Isolated spaces within your tenant"},
	"secrets":      {groupPlatform, "API keys and integration credentials"},
	"users":        {groupPlatform, "People with access to your tenant"},
	"tenants":      {groupPlatform, "Your top-level account"},
	"rbac":         {groupPlatform, "Roles and permissions"},
	"groups":       {groupPlatform, "Collections of users or entities"},
	"integrations": {groupPlatform, "Connections to Stripe, HubSpot and others"},

	// Automation
	"workflows":       {groupAutomation, "Long-running billing processes"},
	"tasks":           {groupAutomation, "Background jobs and their status"},
	"scheduled-tasks": {groupAutomation, "Work queued to run later"},
	"alerts":          {groupAutomation, "Threshold and anomaly notifications"},
	"alert-settings":  {groupAutomation, "How and when alerts fire"},
}

// builtinGroups places the hand-written commands. Kept separate from
// resourceGroups because these are not spec-derived and so are not covered by
// TestEveryResourceHasAGroup.
var builtinGroups = map[string]string{
	"init": groupSetup, "login": groupSetup, "logout": groupSetup,
	"whoami": groupSetup, "env": groupSetup, "config": groupSetup,
	"open": groupSetup, "version": groupSetup,

	"get": groupAdvanced, "post": groupAdvanced, "delete": groupAdvanced,
	"resources": groupAdvanced,
}
```

- [ ] **Step 4: Register the groups in `root.go`**

In `NewRootCommand`, after the `root.AddCommand(...)` block that adds the built-in
commands, add:

```go
	root.AddGroup(commandGroups...)
	for _, c := range root.Commands() {
		if id, ok := builtinGroups[c.Name()]; ok {
			c.GroupID = id
		}
	}
	// The auto-added help and completion commands are created during Execute,
	// not here, and land in "Additional Commands" unless placed explicitly.
	root.SetHelpCommandGroupID(groupAdvanced)
	root.SetCompletionCommandGroupID(groupAdvanced)
```

Note the ordering constraint: `AddGroup` must run **before** any command carrying a
`GroupID` reaches `Execute`, and the loop above must run after all built-ins are added.

- [ ] **Step 5: Apply groups and descriptions to the resource tree**

In `internal/cmd/resource.go`, replace the `parent` construction inside
`addResourceCommands` with:

```go
	for _, resource := range reg.Resources() {
		entry, known := resourceGroups[resource]
		short := entry.Short
		if !known {
			// Unmapped resources still appear, under cobra's built-in
			// "Additional Commands" heading. GroupID stays empty
			// deliberately: an unregistered ID would panic at Execute().
			short = "Operations on " + resource
		}
		parent := &cobra.Command{
			Use:     resource,
			Short:   short,
			GroupID: entry.GroupID,
		}
```

And give the `resources` command its group:

```go
	root.AddCommand(&cobra.Command{
		Use:     "resources",
		Short:   "List every resource this CLI can act on",
		GroupID: groupAdvanced,
		RunE: func(c *cobra.Command, _ []string) error {
			for _, r := range reg.Resources() {
				fmt.Fprintf(os.Stdout, "%-28s %s\n", r, strings.Join(reg.Actions(r), ", "))
			}
			return nil
		},
	})
```

Also set the group on the raw commands in `internal/cmd/raw.go` — each of the three
`get`/`post`/`delete` commands gets `GroupID: groupAdvanced`. (`builtinGroups` covers them
too; setting it in both places is harmless, but the map is the single source of truth if
you prefer to leave `raw.go` untouched.)

- [ ] **Step 6: Generate the golden file and inspect it by eye**

```bash
UPDATE_GOLDEN=1 go test ./internal/cmd/ -run TestRootHelp_Golden
cat internal/cmd/testdata/root_help.golden
```

**Read the output.** Two defects in the previous round passed their tests and were only
found by looking at rendered output. Check specifically: are groups in the intended order,
is every resource under a sensible heading, is `Additional Commands` empty (it should be —
if anything is listed there, `resourceGroups` has a gap the test will also report).

- [ ] **Step 7: Run the full group suite**

```bash
go test ./internal/cmd/ -run 'TestRootHelp|TestEvery' -v
```

Expected: PASS, all five.

- [ ] **Step 8: Full suite**

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... -race 2>&1 | tail -12
```

Expected: clean, all `ok`.

- [ ] **Step 9: Commit**

```bash
git add internal/cmd/groups.go internal/cmd/groups_test.go internal/cmd/testdata/ internal/cmd/root.go internal/cmd/resource.go internal/cmd/raw.go
git commit -m "feat(cli): group root help by billing lifecycle

Replaces a 44-item flat alphabetical list, 34 entries of which read
'Operations on <x>', with eight lifecycle groups and hand-written per-resource
descriptions.

cobra panics at Execute() on an unregistered GroupID, so TestRootHelp_
DoesNotPanicOnGroupIDs runs Execute deliberately. Unmapped resources keep an
empty GroupID and fall into cobra's native 'Additional Commands' rather than
being dropped."
```

---

## Task 6: Migrate the call sites

**Files:**
- Modify: `internal/cmd/init.go`, `auth.go`, `resource.go`, `config.go`, `env.go`, `misc.go`, `raw.go`
- Modify: `internal/output/output.go`, `internal/output/table.go`
- Modify: `internal/cmd/root.go`

Roughly 34 `fmt.Fprint*(os.Stderr|os.Stdout, ...)` calls become `g.UI` calls. The status
footer moves out of `internal/output` into `ui`.

**The hard constraint for this task:** the existing golden tests in `internal/output` must
pass **unmodified**. If you find yourself editing `customers_list.golden.json`, stop — that
is the signal stdout changed for anyone piping to `jq`, not a reason to update the file.

- [ ] **Step 1: Write the stdout-contract test first**

Create `internal/output/contract_test.go`:

```go
package output

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The status footer is human commentary and must never reach stdout. It moved
// to internal/ui in this change; this test pins that it did not come back.
func TestRenderTable_WritesNothingHumanToStdout(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("testdata", "customers_list.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}
	if err := w.Render(input, Options{Columns: []string{"id", "email"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	if bytes.Contains(out.Bytes(), []byte("profile:")) {
		t.Errorf("the status footer leaked into stdout:\n%s", out.String())
	}
}
```

- [ ] **Step 2: Run it — it should pass already, then keep passing**

```bash
go test ./internal/output/ -run TestRenderTable_WritesNothingHuman -v
```

Expected: PASS. This is a guard, not a red test — it must still pass after the footer
moves.

- [ ] **Step 3: Remove the status footer from `table.go`**

In `internal/output/table.go`, delete this block at the end of `renderTable`:

```go
	if o.Status != "" {
		w.Warn(o, "%s", style.Dim(o.Status))
	}
```

In `internal/output/output.go`, delete the `Status` field and its comment from `Options`.

- [ ] **Step 4: Print the footer from the caller instead**

In `internal/cmd/resource.go`, in `newOperationCommand`'s `RunE`, replace the render block:

```go
			format, err := output.ParseFormat(g.Output)
			if err != nil {
				return err
			}
			w := output.Writer{Out: os.Stdout, Err: os.Stderr, Format: format}
			if err := w.Render(merged, output.Options{
				Columns: pickColumns(reg, g, cmd.Resource),
				Quiet:   g.Quiet,
				Shown:   shown,
				Total:   page.Total,
			}); err != nil {
				return err
			}
			// Only table output gets a footer: a caller using json or yaml is
			// scripting, and CI commonly captures stderr alongside stdout where
			// it would be noise.
			if format == output.FormatTable {
				g.UI.StatusLine(statusLine(rc, version))
			}
			return nil
```

- [ ] **Step 5: Add the spinner around the request**

Still in `newOperationCommand`'s `RunE`, wrap the pagination loop. Replace the `for {` loop
header and the `--all` progress print:

```go
			sp := g.UI.Spinner(spinnerVerb(cmd) + " " + cmd.Resource + "…")
			defer sp.Stop()

			for {
				spec.ApplyPaging(&req, cmd, spec.Paging{Limit: pageSize, Offset: offset})

				raw, err := cl.Do(cc.Context(), req.Method, req.Path, req.Query, req.Body)
				if err != nil {
					sp.Stop()
					return err
				}

				page, _ = spec.PageInfo(raw)
				shown += page.Count
				merged = raw

				if !g.All || !page.HasMore(shown) || page.Count == 0 {
					break
				}
				offset += page.Count

				req, err = spec.BuildRequest(cmd, in)
				if err != nil {
					sp.Stop()
					return err
				}
				// Tick on each COMPLETED page rather than on a timer, so a
				// stalled page shows as a frozen count instead of an animation
				// implying progress it is not making.
				sp.Update(fmt.Sprintf("fetched %d of %d…", shown, page.Total))
			}
			sp.Stop()
```

Delete the old `if g.All && !g.Quiet && shown > 0 { fmt.Fprintln(os.Stderr) }` block — the
spinner's erase replaces it.

Add the helper at the end of `internal/cmd/resource.go`:

```go
// spinnerVerb turns an action into the present participle shown while a request
// is in flight. Unknown actions fall back to "Working on", which is vague but
// never wrong — a misleading verb is worse than a general one.
func spinnerVerb(cmd spec.Command) string {
	switch cmd.Action {
	case "list":
		return "Fetching"
	case "retrieve", "get":
		return "Fetching"
	case "create":
		return "Creating"
	case "update":
		return "Updating"
	case "delete":
		return "Deleting"
	case "void", "cancel", "terminate", "archive":
		return "Updating"
	case "finalize":
		return "Finalizing"
	default:
		return "Working on"
	}
}
```

- [ ] **Step 6: Migrate `auth.go`**

In `newLoginCommand`'s `RunE`, wrap verification and replace the trailing prints:

```go
			sp := g.UI.Spinner("Verifying your key…")
			verifyErr := VerifyKey(ctx, baseURL, apiKey, version, g.Debug, os.Stderr)
			sp.Stop()
			if verifyErr != nil {
				return verifyErr
			}
```

Replace the rotation notice:

```go
			if _, existed := cfg.Profiles[profileName]; existed {
				if old, err := store.Get(profileName); err == nil {
					g.UI.Info("Replacing key %s with %s for profile %q",
						MaskKey(old), MaskKey(apiKey), profileName)
				}
			}
```

Replace the two closing prints:

```go
			g.UI.Success("Verified — stored as profile %q in %s", profileName, store.Name())
			g.UI.Info("Note: the API does not report which environment a key belongs to, so label\n" +
				"your profiles yourself (--profile-name, --label) and check with: flexprice whoami")
```

In `newLogoutCommand`, replace the final print:

```go
			g.UI.Success("Removed profile %q", name)
```

In `newWhoamiCommand`, replace the `fmt.Fprintf(os.Stdout, ...)` calls with `g.UI.Data(...)`
— **note the stream stays stdout**, because `whoami` output is a result people parse, not
commentary:

```go
			g.UI.Data("Profile:      %s", name)
			g.UI.Data("Label:        %s", profile.Label)
			g.UI.Data("Region:       %s", profile.Region)
			g.UI.Data("Base URL:     %s", profile.BaseURL)
			g.UI.Data("Key backend:  %s", store.Name())
			if keyErr == nil {
				g.UI.Data("Key:          %s", MaskKey(key))
			} else {
				g.UI.Data("Key:          (not stored — run flexprice login)")
			}
```

`newWhoamiCommand` and `newLogoutCommand` already receive `g *Globals`, so no signature
changes.

- [ ] **Step 7: Migrate `init.go`**

Replace `printInitBanner` and the `RunE` body:

```go
func printInitBanner(w io.Writer, g *Globals) {
	if g.Quiet {
		return
	}
	fmt.Fprint(w, style.Logo(terminalWidth()))
	fmt.Fprintln(w, "Usage-based billing from your terminal")
	fmt.Fprintln(w)
}

func newInitCommand(g *Globals, version string) *cobra.Command {
	return &cobra.Command{
		Use:     "init",
		Short:   "Set up the CLI (guided)",
		GroupID: groupSetup,
		RunE: func(c *cobra.Command, args []string) error {
			printInitBanner(os.Stderr, g)
			g.UI.Info("Welcome to Flexprice — let's get you set up.")
			g.UI.Info("Your API key is scoped to one environment — you can add more later with `flexprice login`.")
			g.UI.Info("")

			login := newLoginCommand(g, version)
			login.SetContext(c.Context())
			if err := login.RunE(login, nil); err != nil {
				return err
			}

			g.UI.Info("\nHere's what to try first:")
			g.UI.Info("  flexprice whoami            confirm what you are pointed at")
			g.UI.Info("  flexprice resources         see everything you can act on")
			g.UI.Info("  flexprice customers list    try a read")
			g.UI.Info("  flexprice env list          see your other environments")
			return nil
		},
	}
}
```

- [ ] **Step 8: Migrate `misc.go`**

In the `webhooks` subcommand:

```go
			g.UI.Info("Add your tunnel URL as an endpoint here:")
			g.UI.Data("%s", resp.URL)
			return openURL(resp.URL)
```

In `newVersionCommand`, change the `Run` signature usage to reach `g.UI`:

```go
		Run: func(c *cobra.Command, _ []string) {
			g.UI.Data("flexprice %s", version)
			g.UI.Data("embedded OpenAPI spec: %d bytes", len(specdata.OpenAPI))
		},
```

`openURL` has no `*Globals`; leave its `fmt.Fprintf(os.Stderr, ...)` fallback as-is rather
than threading a parameter through for one error path.

- [ ] **Step 9: Build, vet, and run everything**

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... -race 2>&1 | tail -15
```

Expected: clean, all `ok`. **The golden files in `internal/output/testdata` must be
untouched** — confirm with:

```bash
git status --short internal/output/testdata/
```

Expected: no output. If a golden file shows as modified, stdout changed; revert it and find
what leaked.

- [ ] **Step 10: Look at the real thing**

```bash
go build -o /tmp/fp-check . && HOME=$(mktemp -d) FLEXPRICE_KEY_BACKEND=file /tmp/fp-check --help
```

Read the output. Confirm groups render, descriptions are real, and nothing sits under
`Additional Commands`.

- [ ] **Step 11: Commit**

```bash
git add internal/cmd/ internal/output/
git commit -m "refactor(cli): route human-facing output through internal/ui

Migrates ~34 fmt.Fprint call sites. Adds a spinner around every spec-driven
request and around login verification; the --all loop now ticks on each
completed page rather than on a timer, so a stalled page reads as a frozen
count.

The status footer moves out of internal/output, which no longer knows about
stderr commentary at all. Golden files are unchanged: stdout is byte-identical."
```

---

## Task 7: `--no-input` and the huh confirmation

**Files:**
- Create: `internal/ui/prompt.go`
- Create: `internal/ui/prompt_test.go`
- Modify: `internal/cmd/resource.go`
- Modify: `internal/cmd/auth.go`

Replaces the raw `fmt.Fscanln` y/N prompt with the same `huh` treatment the region picker
already gets, and makes `--no-input` a real, testable refusal rather than an accident of
stdin not being a TTY.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/prompt_test.go`:

```go
package ui

import (
	"strings"
	"testing"
)

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
func TestSelect_SingleOptionNeedsNoPrompt(t *testing.T) {
	u, _, _ := newTestUI(Options{StderrTTY: true, StdinTTY: false, Term: "dumb"})

	got, err := u.Select("Data region", []Option{{Label: "us", Value: "us"}})
	if err != nil {
		t.Fatalf("single option should resolve without prompting: %v", err)
	}
	if got != "us" {
		t.Errorf("got %q, want %q", got, "us")
	}
}
```

- [ ] **Step 2: Run and watch it fail**

```bash
go test ./internal/ui/ -run 'TestConfirm|TestSelect' -v
```

Expected: FAIL — `u.Confirm`, `u.Select`, `Option` undefined.

- [ ] **Step 3: Write `internal/ui/prompt.go`**

```go
package ui

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

// Option is one choice in a Select.
type Option struct {
	Label string
	Value string
}

// Confirm asks before a destructive action. It refuses rather than prompting
// when input is unavailable, so scripts fail loudly with a message naming the
// flag to pass instead of hanging on a prompt nobody can answer.
func (u *UI) Confirm(action, subject string) error {
	if u.noInput {
		return fmt.Errorf(
			"refusing to %s %s without confirmation — pass --force to proceed non-interactively",
			action, subject)
	}

	var ok bool
	err := huh.NewConfirm().
		Title(fmt.Sprintf("This will %s %s and cannot be undone.", action, subject)).
		Affirmative("Yes, " + action).
		Negative("Cancel").
		Value(&ok).
		Run()
	if err != nil {
		return fmt.Errorf("confirmation cancelled: %w", err)
	}
	if !ok {
		return fmt.Errorf("cancelled")
	}
	return nil
}

// Select presents an arrow-key menu. flagHint names the flag a scripted caller
// should pass instead, so the refusal under --no-input is actionable.
func (u *UI) SelectWithHint(title, flagHint string, opts []Option) (string, error) {
	if len(opts) == 0 {
		return "", fmt.Errorf("no options available for %q", title)
	}
	// One option is not a question. Prompting here would make scripted use
	// fail for no reason.
	if len(opts) == 1 {
		return opts[0].Value, nil
	}
	if u.noInput {
		return "", fmt.Errorf("no terminal available — pass %s (for example %s %s)",
			flagHint, flagHint, opts[0].Value)
	}

	huhOpts := make([]huh.Option[string], len(opts))
	for i, o := range opts {
		huhOpts[i] = huh.NewOption(o.Label, o.Value)
	}

	var choice string
	if err := huh.NewSelect[string]().
		Title(title).
		Options(huhOpts...).
		Value(&choice).
		Run(); err != nil {
		return "", fmt.Errorf("%s selection cancelled: %w", title, err)
	}
	return choice, nil
}

// Select is SelectWithHint for callers with no specific flag to point at. The
// refusal message under --no-input is necessarily vaguer, so prefer
// SelectWithHint wherever a flag exists.
func (u *UI) Select(title string, opts []Option) (string, error) {
	return u.SelectWithHint(title, "the corresponding flag", opts)
}
```

- [ ] **Step 4: Verify the prompt tests pass**

```bash
go test ./internal/ui/ -run 'TestConfirm|TestSelect' -v
```

Expected: PASS, all four.

- [ ] **Step 5: Replace the raw prompt in `resource.go`**

Delete `promptConfirm` and rewrite `confirmAction` and `confirm`:

```go
// confirm prompts before a destructive spec-driven action.
func confirm(g *Globals, cmd spec.Command, target string, force bool) error {
	if !destructive[cmd.Action] {
		return nil
	}
	subject := target
	if subject == "" {
		subject = cmd.Resource
	}
	return confirmAction(g, cmd.Action, subject, force)
}

// confirmAction prompts before a destructive action — shared by the
// spec-driven commands and the raw get/post/delete escape hatch, neither of
// which always has a spec.Command to hand.
func confirmAction(g *Globals, action, subject string, force bool) error {
	if force {
		return nil
	}
	return g.UI.Confirm(action, subject)
}
```

Update the call in `newOperationCommand`'s `RunE`:

```go
			if err := confirm(g, cmd, in.PositionalID, force); err != nil {
				return err
			}
```

Remove the now-unused `io` and `term` imports from `resource.go` if nothing else uses them
(`go build` will tell you).

**Behaviour change worth stating:** previously a non-TTY stdin *skipped* the confirmation
and proceeded. Now it refuses and names `--force`. That is the clig.dev-conformant
behaviour and is safer, but it *will* break any existing script that pipes into a
destructive command without `--force`. Note it in the commit message and the README.

- [ ] **Step 6: Update `raw.go`'s call site**

Find the `confirmAction(` call in `internal/cmd/raw.go` and add `g` as the first argument:

```go
			if err := confirmAction(g, "delete", path, force); err != nil {
				return err
			}
```

- [ ] **Step 7: Replace `promptRegion` in `auth.go`**

```go
// promptRegion asks which data region the key belongs to. The TTY guard now
// lives in ui.SelectWithHint, which refuses with an actionable message rather
// than hanging, so every scripted caller gets the same treatment.
func promptRegion(g *Globals, regions []spec.Region) (string, error) {
	opts := make([]ui.Option, len(regions))
	for i, r := range regions {
		opts[i] = ui.Option{
			Label: fmt.Sprintf("%-6s  %s", r.Key, r.BaseURL),
			Value: r.Key,
		}
	}
	return g.UI.SelectWithHint("Data region", "--region", opts)
}
```

Update its caller in `newLoginCommand`:

```go
					region, err = promptRegion(g, regions)
```

Add `"github.com/flexprice/cli/internal/ui"` to `auth.go`'s imports; remove the now-unused
`"github.com/charmbracelet/huh"` import.

- [ ] **Step 8: Fix the existing region-picker test**

`internal/cmd/auth_test.go` has a test asserting `promptRegion` refuses without a terminal.
Update its call to pass a `*Globals` whose UI has `NoInput: true`:

```go
func TestPromptRegion_RefusesWithoutTerminal(t *testing.T) {
	g := &Globals{UI: ui.New(ui.Options{StderrTTY: true, StdinTTY: false, Term: "dumb"})}
	_, err := promptRegion(g, []spec.Region{
		{Key: "us", BaseURL: "https://us.api.flexprice.io/v1"},
		{Key: "in", BaseURL: "https://in.api.flexprice.io/v1"},
	})
	if err == nil {
		t.Fatal("promptRegion must refuse when stdin is not a terminal")
	}
	if !strings.Contains(err.Error(), "--region") {
		t.Errorf("refusal must name --region, got %q", err)
	}
}
```

Note the two regions: with one option `SelectWithHint` returns it without prompting, so a
single-region fixture would not exercise the refusal path.

- [ ] **Step 9: Build and run everything**

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... -race 2>&1 | tail -15
```

Expected: clean, all `ok`.

- [ ] **Step 10: Commit**

```bash
git add internal/ui/prompt.go internal/ui/prompt_test.go internal/cmd/
git commit -m "feat(cli): add --no-input and move confirmation to huh

Replaces the raw fmt.Fscanln y/N prompt with the same treatment the region
picker gets, and makes --no-input an explicit refusal naming the flag to pass.

BEHAVIOUR CHANGE: a destructive command with non-TTY stdin previously skipped
confirmation and proceeded. It now refuses and names --force. This is safer and
matches clig.dev, but scripts piping into destructive commands without --force
will start failing."
```

---

## Task 8: Mutation receipts

**Files:**
- Modify: `internal/ui/message.go`
- Modify: `internal/ui/message_test.go`
- Modify: `internal/cmd/resource.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/message_test.go`:

```go
func TestReceipt_GoesToStderrOnly(t *testing.T) {
	u, out, errBuf := newTestUI(Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color"})

	u.Receipt("Created", "customer", "cust_01J8X")

	if out.Len() != 0 {
		t.Errorf("a receipt must never touch stdout, got %q", out.String())
	}
	got := errBuf.String()
	for _, want := range []string{"Created", "customer", "cust_01J8X"} {
		if !strings.Contains(got, want) {
			t.Errorf("receipt missing %q, got %q", want, got)
		}
	}
}

func TestReceipt_SilentWithoutAnID(t *testing.T) {
	u, _, errBuf := newTestUI(Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color"})

	// When the response carried no id there is nothing trustworthy to report,
	// so say nothing rather than guess.
	u.Receipt("Created", "customer", "")

	if errBuf.Len() != 0 {
		t.Errorf("receipt without an id should be silent, got %q", errBuf.String())
	}
}

func TestEmptyState_NamesANextStep(t *testing.T) {
	u, _, errBuf := newTestUI(Options{StderrTTY: true, StdinTTY: true, Term: "xterm-256color"})

	u.EmptyState("customers")

	got := errBuf.String()
	if !strings.Contains(got, "customers") {
		t.Errorf("empty state should name the resource, got %q", got)
	}
	if !strings.Contains(got, "flexprice customers create") {
		t.Errorf("empty state should suggest a next command, got %q", got)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

```bash
go test ./internal/ui/ -run 'TestReceipt|TestEmptyState' -v
```

Expected: FAIL — `u.Receipt`, `u.EmptyState` undefined.

- [ ] **Step 3: Implement both in `internal/ui/message.go`**

```go
// Receipt confirms a state change. It is silent when id is empty: without an
// identifier there is nothing trustworthy to report, and a vague "Created
// something" is worse than saying nothing.
//
// Always stderr. The created object itself goes to stdout as normal output, so
// piping to jq is unaffected.
func (u *UI) Receipt(verb, resource, id string) {
	if u.quiet || id == "" {
		return
	}
	fmt.Fprintln(u.err, u.palette.Success(fmt.Sprintf("%s %s %s", verb, resource, id)))
}

// EmptyState reports that a list came back empty and names a way forward.
// clig.dev: suggesting the next command is how people discover a CLI's shape.
func (u *UI) EmptyState(resource string) {
	if u.quiet {
		return
	}
	fmt.Fprintf(u.err, "No %s yet.\n", resource)
	fmt.Fprintf(u.err, "  Create one with: %s\n",
		u.palette.Accent(fmt.Sprintf("flexprice %s create", resource)))
}
```

- [ ] **Step 4: Verify they pass**

```bash
go test ./internal/ui/ -run 'TestReceipt|TestEmptyState' -v
```

Expected: PASS.

- [ ] **Step 5: Emit receipts from `resource.go`**

In `newOperationCommand`'s `RunE`, after the successful render and before `return nil`, add:

```go
			if verb, ok := receiptVerbs[cmd.Action]; ok {
				g.UI.Receipt(verb, singular(cmd.Resource), responseID(merged))
			}
```

Add these helpers at the end of `internal/cmd/resource.go`:

```go
// receiptVerbs maps mutating actions to the past-tense verb shown in a
// receipt. Read actions are absent deliberately: "Retrieved customer X" tells
// the user nothing they cannot see in the output directly above it.
var receiptVerbs = map[string]string{
	"create":    "Created",
	"update":    "Updated",
	"delete":    "Deleted",
	"void":      "Voided",
	"cancel":    "Cancelled",
	"terminate": "Terminated",
	"archive":   "Archived",
	"finalize":  "Finalized",
}

// responseID pulls the top-level "id" out of a response, or returns "" when
// there is not exactly one obvious identifier. Returning "" makes Receipt
// silent, which is the intended behaviour when we cannot say precisely what
// happened.
func responseID(raw []byte) string {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	id, _ := doc["id"].(string)
	return id
}

// singular trims a trailing "s" for the receipt line, so the resource reads as
// one object. Resources whose plural is irregular are left alone: this is
// cosmetic, and a wrong singular is more jarring than an unchanged plural.
func singular(resource string) string {
	if len(resource) > 1 && strings.HasSuffix(resource, "s") &&
		!strings.HasSuffix(resource, "ss") {
		return strings.TrimSuffix(resource, "s")
	}
	return resource
}
```

- [ ] **Step 6: Build and test**

```bash
go build ./... && go vet ./... && go test ./... -race 2>&1 | tail -12
git status --short internal/output/testdata/
```

Expected: all `ok`, and no modified golden files.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/ internal/cmd/resource.go
git commit -m "feat(cli): confirm mutations with a receipt on stderr

'Created customer cust_01J8X' after a create, so the user does not have to
infer success from a table. Silent when the response carries no id: without an
identifier there is nothing trustworthy to report.

stderr only — stdout stays byte-identical for anyone piping to jq."
```

---

## Task 9: Empty states

**Files:**
- Modify: `internal/output/output.go`
- Modify: `internal/output/table.go`
- Modify: `internal/cmd/resource.go`

`renderTable` currently prints `No results.` itself. The resource name lives in the command,
not the renderer, so the decision moves out to the caller.

- [ ] **Step 1: Write the failing test**

Create `internal/output/empty_test.go`:

```go
package output

import (
	"bytes"
	"testing"
)

// The renderer no longer invents an empty-state message: it reports that the
// result set was empty and lets the caller, which knows the resource name, say
// something useful.
func TestRender_ReportsEmptyWithoutPrinting(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	res, err := w.RenderResult([]byte(`{"items":[],"pagination":{"total":0}}`), Options{})
	if err != nil {
		t.Fatalf("RenderResult: %v", err)
	}
	if !res.Empty {
		t.Error("Empty should be true for a zero-row response")
	}
	if errOut.Len() != 0 {
		t.Errorf("renderer should not print its own empty-state text, got %q", errOut.String())
	}
}

func TestRender_NotEmptyWhenRowsExist(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	res, err := w.RenderResult([]byte(`{"items":[{"id":"cust_01"}],"pagination":{"total":1}}`), Options{})
	if err != nil {
		t.Fatalf("RenderResult: %v", err)
	}
	if res.Empty {
		t.Error("Empty should be false when rows exist")
	}
	if !bytes.Contains(out.Bytes(), []byte("cust_01")) {
		t.Errorf("row missing from output: %q", out.String())
	}
}
```

- [ ] **Step 2: Run and watch it fail**

```bash
go test ./internal/output/ -run TestRender_ -v
```

Expected: FAIL — `RenderResult` undefined.

- [ ] **Step 3: Add `RenderResult` to `internal/output/output.go`**

```go
// Result reports what Render did, so a caller that knows the resource name can
// say something useful about an empty list. The renderer deliberately does not
// invent that message itself: it has no idea what "customers" are.
type Result struct {
	Empty bool
}

// RenderResult renders and reports. Render is kept as a thin wrapper so
// existing callers and tests are unaffected.
func (w Writer) RenderResult(raw []byte, o Options) (Result, error) {
	if w.Format != FormatTable {
		return Result{}, w.Render(raw, o)
	}
	rows, err := rowsFrom(raw)
	if err != nil {
		return Result{}, Writer{Out: w.Out, Err: w.Err, Format: FormatJSON}.Render(raw, o)
	}
	if len(rows) == 0 {
		return Result{Empty: true}, nil
	}
	return Result{}, w.renderTable(raw, o)
}
```

- [ ] **Step 4: Remove the renderer's own empty message**

In `internal/output/table.go`, replace:

```go
	if len(rows) == 0 {
		w.Warn(o, "No results.")
		return nil
	}
```

with:

```go
	if len(rows) == 0 {
		// Empty is reported through RenderResult so the caller, which knows the
		// resource name, can name a next step. Reaching here directly (via the
		// legacy Render path) prints nothing rather than a bare "No results."
		return nil
	}
```

- [ ] **Step 5: Use it in `resource.go`**

Replace the render block in `newOperationCommand`'s `RunE`:

```go
			res, err := w.RenderResult(merged, output.Options{
				Columns: pickColumns(reg, g, cmd.Resource),
				Quiet:   g.Quiet,
				Shown:   shown,
				Total:   page.Total,
			})
			if err != nil {
				return err
			}
			if res.Empty {
				g.UI.EmptyState(cmd.Resource)
				return nil
			}
			if format == output.FormatTable {
				g.UI.StatusLine(statusLine(rc, version))
			}
			// Preserved from Task 8 — do not drop this when replacing the
			// block above.
			if verb, ok := receiptVerbs[cmd.Action]; ok {
				g.UI.Receipt(verb, singular(cmd.Resource), responseID(merged))
			}
			return nil
```

- [ ] **Step 6: Build and test**

```bash
go build ./... && go vet ./... && go test ./... -race 2>&1 | tail -12
git status --short internal/output/testdata/
```

Expected: all `ok`, no modified golden files.

- [ ] **Step 7: Commit**

```bash
git add internal/output/ internal/cmd/resource.go
git commit -m "feat(cli): empty lists name a next step

'No customers yet.' plus a create command, instead of a bare 'No results.'
The renderer reports emptiness rather than wording it: it has no idea what a
customer is, and the resource name lives in the command."
```

---

## Task 10: Shared ANSI-aware padding, and fix `env list`

**Files:**
- Create: `internal/output/pad.go`
- Create: `internal/output/pad_test.go`
- Modify: `internal/output/table.go`
- Modify: `internal/cmd/env.go`

`internal/cmd/env.go` uses `text/tabwriter`, which counts raw bytes. That is the exact bug
that misaligned the main table once colour was on. Extracting the fix means it cannot be
reintroduced here.

- [ ] **Step 1: Write the failing test**

Create `internal/output/pad_test.go`:

```go
package output

import (
	"strings"
	"testing"

	"github.com/flexprice/cli/internal/style"
)

// text/tabwriter counts escape bytes as visible width; this is the regression
// that misaligned every column once styling was on.
func TestPadGrid_MeasuresVisibleWidthNotBytes(t *testing.T) {
	style.EnableForTests()

	coloured := style.Header("ID")
	if !strings.Contains(coloured, "\x1b[") {
		t.Fatal("test setup: expected a styled header to contain escape codes")
	}

	lines := PadGrid([][]string{
		{coloured, "EMAIL"},
		{"cust_01", "ada@example.com"},
	})
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}

	// Both rows' second column must start at the same visible offset.
	if got, want := visibleIndexOf(lines[0], "EMAIL"), visibleIndexOf(lines[1], "ada@"); got != want {
		t.Errorf("columns misaligned: header col2 at %d, row col2 at %d\n%q\n%q",
			got, want, lines[0], lines[1])
	}
}

func TestPadGrid_NoTrailingWhitespace(t *testing.T) {
	lines := PadGrid([][]string{
		{"a", "bbbb"},
		{"cc", "d"},
	})
	for i, l := range lines {
		if l != strings.TrimRight(l, " ") {
			t.Errorf("line %d has trailing whitespace: %q", i, l)
		}
	}
}

// visibleIndexOf returns the visible-column offset at which sub starts.
func visibleIndexOf(line, sub string) int {
	idx := strings.Index(line, sub)
	if idx < 0 {
		return -1
	}
	return lipglossWidth(line[:idx])
}
```

- [ ] **Step 2: Run and watch it fail**

```bash
go test ./internal/output/ -run TestPadGrid -v
```

Expected: FAIL — `PadGrid` and `lipglossWidth` undefined.

- [ ] **Step 3: Write `internal/output/pad.go`**

```go
package output

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// gutter is the minimum gap between columns.
const gutter = 2

// lipglossWidth is a thin alias so tests and callers measure width the same way
// the padder does.
func lipglossWidth(s string) int { return lipgloss.Width(s) }

// PadGrid aligns a grid of cells into lines, measuring VISIBLE width.
//
// text/tabwriter cannot be used for anything that might carry colour: it counts
// raw bytes, so ~20 invisible escape bytes per styled cell inflate its
// calculation and misalign every column. lipgloss.Width is ANSI-aware and also
// handles wide East-Asian runes correctly.
//
// The final column is never padded: trailing whitespace is invisible on screen
// but shows up in golden-file comparisons and when piping into other tools.
func PadGrid(grid [][]string) []string {
	if len(grid) == 0 {
		return nil
	}

	widths := make([]int, len(grid[0]))
	for _, row := range grid {
		for i, cell := range row {
			if i >= len(widths) {
				continue
			}
			if n := lipgloss.Width(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	lines := make([]string, 0, len(grid))
	for _, row := range grid {
		var b strings.Builder
		for i, cell := range row {
			b.WriteString(cell)
			if i < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(cell)+gutter))
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}
```

- [ ] **Step 4: Use it in `table.go`**

In `internal/output/table.go`, delete the `const gutter = 2` line (now in `pad.go`) and
replace the width-measurement and line-building blocks with:

```go
	for _, line := range PadGrid(grid) {
		if _, err := fmt.Fprintln(w.Out, line); err != nil {
			return fmt.Errorf("write table: %w", err)
		}
	}
```

Remove the now-unused `lipgloss` import from `table.go` if nothing else uses it.

- [ ] **Step 5: Use it in `env.go`**

Replace the `text/tabwriter` block in `newEnvCommand`'s list subcommand:

```go
			grid := [][]string{{
				style.Header("ENVIRONMENT"),
				style.Header("TYPE"),
				style.Header("ID"),
			}}
			for _, e := range envs.Environments {
				grid = append(grid, []string{e.Name, e.Type, e.ID})
			}
			for _, line := range output.PadGrid(grid) {
				g.UI.Data("%s", line)
			}
```

Update `env.go`'s imports: remove `"text/tabwriter"`, add
`"github.com/flexprice/cli/internal/output"` and `"github.com/flexprice/cli/internal/style"`.

- [ ] **Step 6: Verify, including by eye**

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... -race 2>&1 | tail -12
```

Expected: all `ok`, and the existing `TestTableOutput_ContainsTheData` still passes.

- [ ] **Step 7: Commit**

```bash
git add internal/output/ internal/cmd/env.go
git commit -m "refactor(cli): share ANSI-aware padding; drop tabwriter from env list

env list used text/tabwriter, which counts escape bytes as visible width — the
same defect that misaligned the main table once colour was on. Extracting
PadGrid means adding colour there cannot reintroduce it."
```

---

## Task 11: Documentation

**Files:**
- Create: `cli/decisions/0006-ui-owns-human-facing-output.md`
- Modify: `cli/README.md`
- Modify: `cli/ARCHITECTURE.md`
- Modify: `docs/design/2026-08-18-flexprice-cli-interactive-ui-implementation-plan.md`
- Modify: `docs/design/2026-08-18-flexprice-cli-handoff.md`

Also closes the two documentation-staleness items the handoff already records as open.

- [ ] **Step 1: Write ADR 0006**

Create `cli/decisions/0006-ui-owns-human-facing-output.md`, matching the format of the
existing five ADRs:

```markdown
# 0006 — internal/ui owns every human-facing write

## Status
Accepted — 2026-08-18

## Context
Human-facing output was 34 `fmt.Fprint*` calls spread across seven files in
`internal/cmd`. Each independently decided whether to respect `--quiet`, whether
a terminal was attached, and whether to use colour — and most decided nothing at
all. Three of `internal/style`'s six exported functions (`Success`, `Error`,
`Warning`) were never called from production code.

Eight of the eleven DX gaps found in the audit reduce to one question no call
site was asking: is a human watching this stream right now.

## Decision
`internal/ui` owns every human-facing write. It holds one value carrying the
streams and the quiet/TTY/no-input/animate decisions, and hangs off the existing
`Globals`, which already reaches all seven files.

`internal/style` keeps a single responsibility — deciding what colour something
is — but becomes a `Palette` value so colour can be gated per stream. The
package-level functions remain, backed by a stdout-gated default.

## Consequences
- `--quiet`, `TERM=dumb`, CI detection and `--no-input` are implemented once.
- Colour on stderr is gated on stderr. Previously `flexprice customers list >
  out.json` stripped colour from a footer still going to a live terminal, and
  the same bug applied to a spinner would have suppressed it in one of the most
  common ways this CLI is run.
- `internal/output` no longer knows about stderr commentary at all.
- New commands get correct behaviour by default; using `fmt.Fprintf(os.Stderr,
  …)` in `internal/cmd` is now the anomaly rather than the norm.
```

- [ ] **Step 2: Update `cli/README.md`**

Add a `--no-input` row to the global flags table, alongside the existing `--quiet` and
`--no-color` entries:

```markdown
| `--no-input` | Never prompt; fail with a message naming the flag to pass instead. |
```

Add a short section after the output-formats section:

```markdown
### Scripting and CI

The CLI writes data to stdout and everything human — progress, receipts, footers —
to stderr, so redirecting stdout gives you clean machine-readable output:

```bash
flexprice customers list --output json > customers.json
```

Progress animation is suppressed automatically when stderr is not a terminal, when
`TERM=dumb`, and under `--quiet`. Colour additionally respects `NO_COLOR` and
`--no-color`.

Destructive commands confirm before acting. In a script, pass `--force` to proceed,
or `--no-input` to have the CLI fail rather than wait for an answer nobody can give.
```

Also document the wordmark and the status footer, which the handoff records as
undocumented.

- [ ] **Step 3: Update `cli/ARCHITECTURE.md`**

In the package list, add:

```markdown
- `internal/ui` — owns every human-facing write: progress, receipts, prompts,
  confirmations, empty states, and the status footer. Decides once whether a
  human is watching (`--quiet`, TTY, `TERM=dumb`, `--no-input`) so no call site
  has to. See `decisions/0006-ui-owns-human-facing-output.md`.
```

Update the `internal/style` line to say it decides colour only, and that it exposes a
`Palette` so callers can gate per stream.

- [ ] **Step 4: Correct the stale interactive-UI plan**

In `docs/design/2026-08-18-flexprice-cli-interactive-ui-implementation-plan.md`, find the
task describing a bordered welcome box and add this note directly beneath its heading:

```markdown
> **Superseded.** The bordered box described below was replaced by the block-letter
> wordmark in `internal/style/logo.go` before this plan was completed. The steps here
> are kept as a record of what was tried; do not implement them.
```

- [ ] **Step 5: Close the resolved items in the handoff**

In `docs/design/2026-08-18-flexprice-cli-handoff.md` §4, under "Stale documentation",
replace both bullets with:

```markdown
- ~~The interactive-UI plan still describes the bordered welcome box.~~ Resolved: the
  superseded task is now marked as such in that document.
- ~~`cli/README.md` documents neither the wordmark nor the status footer.~~ Resolved in
  the DX polish round.
```

- [ ] **Step 6: Verify the docs build nothing and commit**

```bash
gofmt -l . && go test ./... -race 2>&1 | tail -6
git add cli/decisions/ cli/README.md cli/ARCHITECTURE.md ../docs/design/
git commit -m "docs(cli): record the ui/style boundary and close stale doc items

Adds ADR 0006. Documents --no-input, the scripting contract, the wordmark and
the status footer in the README. Marks the superseded bordered-box task in the
interactive-UI plan and closes both documentation items the handoff had open."
```

---

## Final verification

- [ ] **Step 1: Full clean run**

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... -race
```

Expected: build clean, vet clean, `gofmt -l` silent, every package `ok`.

- [ ] **Step 2: Confirm stdout never changed**

```bash
git diff --stat origin/claude/flexprice-cli-design-7f7052 -- internal/output/testdata/
```

Expected: **no output**. Any change here means stdout moved for anyone piping to `jq`.

- [ ] **Step 3: Manual verification at a real terminal**

The test suite cannot assert that a spinner *looks* right, that its frame rate is
comfortable, or that its erase leaves no artifact. Do these by hand:

```bash
go build -o ./bin/flexprice .
HOME=$(mktemp -d) FLEXPRICE_KEY_BACKEND=file ./bin/flexprice --help
HOME=$(mktemp -d) FLEXPRICE_KEY_BACKEND=file ./bin/flexprice init
```

The scratch `HOME` and `FLEXPRICE_KEY_BACKEND=file` are not optional: `init`, `login` and
`whoami` reach the real OS keychain regardless of which API they point at, and an
unattended run during the previous round triggered a blocking macOS dialog with a
destructive "Reset To Defaults" button.

Check, by eye:
- [ ] Root help: groups in order, real descriptions, `Additional Commands` empty.
- [ ] Region picker: ↑/↓ move, Enter selects, Esc cancels cleanly.
- [ ] Spinner: smooth, no flicker, leaves no artifact when it stops.
- [ ] Ctrl-C mid-spinner: the cursor comes back. Verify with `echo $?` → `130`.
- [ ] `flexprice customers list > /tmp/x.json` — spinner and footer still visible on the
      terminal, file contains only JSON.
- [ ] `flexprice customers list 2>/dev/null | head` — no escape sequences in the pipe.

---

## Self-review

**Spec coverage** — every section of the design maps to a task:

| Design | Task |
|---|---|
| §3.1 package boundary | 1 |
| §3.2 the type | 1, 4 |
| §3.3 per-stream gating | 1 |
| §3.4 vocabulary | 1, 2, 7, 8, 9 |
| §4 spinner semantics | 2 |
| §4.3 cursor / SIGINT | 3 |
| §4.4 spinner placement | 6 |
| §5 grouped help | 5 |
| §5.1 rot guards | 5 |
| §5.2 resource shorts | 5 |
| §6 gap 3 (success/failure) | 3, 6 |
| §6 gap 4 (confirm) | 7 |
| §6 gap 6 (whoami/resources) | 6, 10 |
| §6 gap 7 (`--no-input`) | 4, 7 |
| §6 gap 8 (empty state) | 9 |
| §6 gap 9 (receipts) | 8 |
| §6 gap 10 (`TERM=dumb`) | 1 |
| §6.1 stdout contract | 6, and Final Verification step 2 |
| §7 tone | 6 (init copy), 8, 9 |
| §8 testing | every task; matrix in 1, golden in 5 |
| §8.1 manual verification | Final Verification step 3 |

**Additions beyond the design, and why:**
- Task 10 (`PadGrid` / `env list`) — found while reading `env.go`: it uses `text/tabwriter`,
  the exact byte-counting bug already fixed once in `table.go`. Styling it without this
  would reintroduce a known defect.
- The `style.Palette` refactor in Task 1 — the design requires per-stream gating (§3.3) but
  did not say how; a package-level singleton cannot express it.

**Type consistency check:** `ui.Options`, `ui.New`, `ui.FromEnv`, `UI.Spinner`,
`Spinner.Update`, `Spinner.Stop`, `UI.Info`, `UI.Data`, `UI.Success`, `UI.Failure`,
`UI.StatusLine`, `UI.Receipt`, `UI.EmptyState`, `UI.Confirm`, `UI.SelectWithHint`,
`UI.Select`, `UI.Quiet`, `UI.NoInput`, `ui.Option`, `style.Palette`, `style.NewPalette`,
`style.Default`, `output.PadGrid`, `output.Result`, `Writer.RenderResult`, `resourceEntry`,
`resourceGroups`, `commandGroups`, `builtinGroups`, `globalsFor`, `RestoreTerminal`,
`spinnerVerb`, `receiptVerbs`, `responseID`, `singular` — each is defined in exactly one
task and used with a consistent signature thereafter.
