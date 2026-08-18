package style

import (
	"os"
	"strings"
	"testing"

	"github.com/muesli/termenv"
)

// TestMain forces the package's renderer to a real color profile for the
// whole test binary. go test never runs with a terminal attached, so without
// this, lipgloss's own auto-detection would correctly see "no terminal" and
// silently suppress color — which would make every test asserting on ANSI
// codes fail based on execution environment, not code correctness. Production
// code never calls SetColorProfile, so real auto-detection is unaffected;
// this is test-only, matching the technique verified in the implementation
// spike (docs/design/2026-08-18-cli-interactive-ui-spike-findings.md).
func TestMain(m *testing.M) {
	renderer.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

func TestSuccess_IncludesCheckmarkAndText(t *testing.T) {
	got := Success("Verified")
	if !strings.Contains(got, "✓") || !strings.Contains(got, "Verified") {
		t.Errorf("Success(%q) = %q, want it to contain a checkmark and the text", "Verified", got)
	}
}

func TestError_IncludesCrossAndText(t *testing.T) {
	got := Error("failed")
	if !strings.Contains(got, "✗") || !strings.Contains(got, "failed") {
		t.Errorf("Error(%q) = %q, want it to contain a cross and the text", "failed", got)
	}
}

func TestWarning_IncludesWarningSymbolAndText(t *testing.T) {
	got := Warning("check your input")
	if !strings.Contains(got, "⚠") || !strings.Contains(got, "check your input") {
		t.Errorf("Warning(...) = %q, want it to contain a warning symbol and the text", got)
	}
}

// Icons must survive Disable(): a monochrome terminal still benefits from ✓/✗/⚠
// as information, even with no color applied. Color is the only thing gated.
func TestDisable_KeepsIconsRemovesColorCodes(t *testing.T) {
	Disable()
	defer Enable()

	got := Success("Verified")
	if !strings.Contains(got, "✓") || !strings.Contains(got, "Verified") {
		t.Errorf("Success after Disable() = %q, want icon and text preserved", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("Success after Disable() = %q, want no ANSI escape codes", got)
	}
}

func TestEnable_RestoresColorCodes(t *testing.T) {
	Disable()
	Enable()
	got := Header("test")
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("Header after Enable() = %q, want ANSI escape codes present", got)
	}
}

// Tests asserting ANSI codes ARE present must force Enable() first: the
// default `enabled` state auto-detects the real environment's TTY-ness, and
// `go test` itself is never a TTY, so relying on the ambient default would
// make these tests fail depending on where they're run rather than on
// whether the code is correct.
func TestStatusColor_KnownGoodValue(t *testing.T) {
	Enable()
	defer Disable()

	got := StatusColor("active")
	if !strings.Contains(got, "active") {
		t.Errorf("StatusColor(active) = %q, want it to contain the original text", got)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("StatusColor(active) = %q, want ANSI color codes for a known-good status", got)
	}
}

func TestStatusColor_KnownBadValue(t *testing.T) {
	Enable()
	defer Disable()

	got := StatusColor("archived")
	if !strings.Contains(got, "archived") || !strings.Contains(got, "\x1b[") {
		t.Errorf("StatusColor(archived) = %q, want colored text containing \"archived\"", got)
	}
}

// The unrecognized case is the load-bearing one: an unmatched value must never
// be colored, since a wrong guess is worse than no color at all. Design doc §5.2.
func TestStatusColor_UnrecognizedValueIsUnchanged(t *testing.T) {
	got := StatusColor("some-domain-specific-state")
	if got != "some-domain-specific-state" {
		t.Errorf("StatusColor(unrecognized) = %q, want the value returned completely unchanged", got)
	}
}

// Exact-word match, not substring: "proactive" contains "active" as a substring
// and must not mis-trigger the good-status color.
func TestStatusColor_DoesNotSubstringMatch(t *testing.T) {
	got := StatusColor("proactive")
	if got != "proactive" {
		t.Errorf("StatusColor(proactive) = %q, want it unchanged (not a substring match on \"active\")", got)
	}
}

func TestStatusColor_CaseInsensitive(t *testing.T) {
	Enable()
	defer Disable()

	got := StatusColor("ACTIVE")
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("StatusColor(ACTIVE) = %q, want the match to be case-insensitive", got)
	}
}
