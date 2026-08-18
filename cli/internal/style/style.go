// Package style is the only place in the CLI that decides what color something
// is. Callers never construct ANSI codes or reference a hex color directly.
package style

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// Brand gradient, taken from assets/flexprice_logo_old.svg's gradient stops.
const (
	colorMagenta = lipgloss.Color("#9F398F")
	colorPurple  = lipgloss.Color("#BB71F2")
	colorGreen   = lipgloss.Color("#4ade80")
	colorRed     = lipgloss.Color("#f87171")
	colorYellow  = lipgloss.Color("#facc15")
	colorDim     = lipgloss.Color("#6b7280")
)

// enabled gates the package-level functions, which write STDOUT content only.
// Anything written to stderr must build its own Palette from that stream's
// TTY-ness — see internal/ui. Icons (✓/✗/⚠) are never gated.
var enabled = os.Getenv("NO_COLOR") == "" &&
	os.Getenv("TERM") != "dumb" &&
	term.IsTerminal(int(os.Stdout.Fd()))

func Disable() { enabled = false }
func Enable()  { enabled = true }

// EnableForTests forces a real color profile so other packages' tests can
// assert on colored output. go test never has a terminal attached, so without
// this both this package and lipgloss suppress color and such assertions pass
// or fail on where they run. Not for production code.
func EnableForTests() {
	enabled = true
	renderer.SetColorProfile(termenv.TrueColor)
}

// Package-level rather than lipgloss's own default so EnableForTests can force
// a profile deterministically.
var renderer = lipgloss.NewRenderer(os.Stdout)

// Palette renders at a fixed color setting, so callers writing to different
// streams can each gate on their own: piping stdout to a file must not strip
// color from a stderr message the user is still watching.
type Palette struct{ enabled bool }

func NewPalette(enabled bool) Palette { return Palette{enabled: enabled} }

// Default reads the package gate at call time, not capture time, so Disable()
// still takes effect after flag parsing.
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

func Dim(s string) string     { return Default().Dim(s) }
func Success(s string) string { return Default().Success(s) }
func Error(s string) string   { return Default().Error(s) }
func Warning(s string) string { return Default().Warning(s) }
func Header(s string) string  { return Default().Header(s) }
func Accent(s string) string  { return Default().Accent(s) }

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

// StatusColor leaves an unrecognised status completely unstyled rather than
// guessing: the wrong color is worse than none. Matching is exact-word, so
// "proactive" cannot trigger on "active".
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

func StatusColor(value string) string { return Default().StatusColor(value) }
