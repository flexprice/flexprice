package style

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// Anything narrower than 67 columns plus a margin must fall back, or the
// logo wraps into unreadable garbage.
func TestLogo_WideTerminalUsesFullBlock(t *testing.T) {
	got := Logo(100)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("Logo(100) has %d lines, want 6 (the full block form)", len(lines))
	}
}

func TestLogo_NarrowTerminalFallsBackToCompact(t *testing.T) {
	got := Logo(40)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("Logo(40) has %d lines, want 3 (the compact form)", len(lines))
	}
}

// Width 0 means "unknown" (piped output); must not crash or render wide.
func TestLogo_UnknownWidthFallsBackToCompact(t *testing.T) {
	got := Logo(0)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("Logo(0) has %d lines, want 3 (compact, since width is unknown)", len(lines))
	}
}

// Whichever form is chosen must actually fit the width it was given, measured
// on visible width so the color codes don't skew the check.
func TestLogo_NeverExceedsGivenWidth(t *testing.T) {
	for _, w := range []int{0, 30, 40, 67, 72, 100, 200} {
		for _, line := range strings.Split(strings.TrimRight(Logo(w), "\n"), "\n") {
			if n := lipgloss.Width(line); w > 0 && n > w {
				t.Errorf("Logo(%d) produced a %d-column line: %q", w, n, line)
			}
		}
	}
}

func TestLogo_ContainsNoANSIWhenDisabled(t *testing.T) {
	Disable()
	defer EnableForTests()

	if got := Logo(100); strings.Contains(got, "\x1b[") {
		t.Errorf("Logo contains ANSI codes with color disabled: %q", got)
	}
}
