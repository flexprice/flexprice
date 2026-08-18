// Package style is the only place in the CLI that decides what color
// something is. Every other package calls into here rather than constructing
// ANSI codes or referencing a hex color directly.
package style

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// Flexprice's brand gradient, extracted from assets/flexprice_logo_old.svg's
// gradient stops — not an invented palette.
const (
	colorMagenta = lipgloss.Color("#9F398F")
	colorPurple  = lipgloss.Color("#BB71F2")
	colorGreen   = lipgloss.Color("#4ade80")
	colorRed     = lipgloss.Color("#f87171")
	colorYellow  = lipgloss.Color("#facc15")
)

// enabled gates every ANSI color code this package emits. It does not gate
// icons (✓/✗/⚠) — those persist even in a monochrome terminal, since they
// carry information a plain-text reader still benefits from. Defaults to on
// only when NO_COLOR is unset and stdout is a real terminal; --no-color calls
// Disable() explicitly once flags are parsed (see cmd/root.go's
// PersistentPreRunE — flags are not populated yet at command construction
// time, so this default is necessarily a best-guess until that hook runs).
//
// lipgloss itself also auto-detects terminal color support independently
// (confirmed in the implementation spike), so this flag and lipgloss's own
// detection are two independent, agreeing safety nets for the auto-detect
// case. This flag remains the only mechanism for the one case lipgloss
// cannot know about on its own: our CLI's own --no-color flag.
var enabled = os.Getenv("NO_COLOR") == "" && term.IsTerminal(int(os.Stdout.Fd()))

// Disable turns off color styling for the rest of the process.
func Disable() { enabled = false }

// Enable turns color styling back on. Exists primarily for tests that need to
// restore state after calling Disable(), and for a future --color flag if one
// is ever added to force color in a piped context.
func Enable() { enabled = true }

// EnableForTests turns styling on and forces a real color profile, for use
// only from other packages' tests that need to assert on this package's
// colored output (e.g. internal/output's table tests). go test never runs
// with a terminal attached, so without forcing the profile, this package's
// own auto-detection would correctly see no terminal and silently suppress
// color — making a caller's ANSI-code assertions fail based on where the
// tests run, not on whether their code calls this package correctly. Do not
// call this from production code.
func EnableForTests() {
	enabled = true
	renderer.SetColorProfile(termenv.TrueColor)
}

// renderer is package-level, rather than calling lipgloss.NewStyle() (the
// package-level default) directly, so tests can force a specific color
// profile deterministically. go test never runs with a real terminal
// attached, so without this indirection, any test asserting on the presence
// of ANSI codes would fail based on execution environment rather than code
// correctness — lipgloss's own auto-detection would (correctly) see no
// terminal and suppress color, the same behavior confirmed in the
// implementation spike. Production code never touches this variable's color
// profile, so real terminal auto-detection is unaffected.
var renderer = lipgloss.NewRenderer(os.Stdout)

func styled(s string, c lipgloss.Color, bold bool) string {
	if !enabled {
		return s
	}
	st := renderer.NewStyle().Foreground(c)
	if bold {
		st = st.Bold(true)
	}
	return st.Render(s)
}

func Success(s string) string { return "✓ " + styled(s, colorGreen, false) }
func Error(s string) string   { return "✗ " + styled(s, colorRed, false) }
func Warning(s string) string { return "⚠ " + styled(s, colorYellow, false) }
func Header(s string) string  { return styled(s, colorMagenta, true) }
func Accent(s string) string  { return styled(s, colorPurple, false) }

var (
	goodStatus = map[string]bool{
		"active": true, "succeeded": true, "finalized": true,
		"paid": true, "completed": true, "published": true,
	}
	badStatus = map[string]bool{
		"failed": true, "archived": true, "voided": true,
		"cancelled": true, "expired": true, "deleted": true,
	}
	warnStatus = map[string]bool{
		"pending": true, "draft": true, "processing": true,
	}
)

// StatusColor returns value styled according to a small, deliberately
// generic, and deliberately incomplete word list — or value completely
// unchanged if it matches nothing. An unrecognized status is never guessed
// at: coloring something the wrong color is worse than leaving it plain.
// Matching is case-insensitive but exact-word, not substring, so
// "proactive" cannot mis-trigger on "active". Design doc §5.2 / §3.
func StatusColor(value string) string {
	lower := strings.ToLower(value)
	switch {
	case goodStatus[lower]:
		return styled(value, colorGreen, false)
	case badStatus[lower]:
		return styled(value, colorRed, false)
	case warnStatus[lower]:
		return styled(value, colorYellow, false)
	default:
		return value
	}
}
