package style

import (
	"os"
	"strings"
	"testing"
)

// go test has no terminal attached, so the profile must be forced or every
// ANSI assertion depends on where it runs.
func TestMain(m *testing.M) {
	EnableForTests()
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

// Tests asserting ANSI codes are present must Enable() first: the ambient
// default auto-detects TTY-ness, and `go test` is never a TTY.
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

func TestDim_WrapsTextWithoutLosingIt(t *testing.T) {
	Enable()
	defer Disable()

	got := Dim("profile: sandbox")
	if !strings.Contains(got, "profile: sandbox") {
		t.Errorf("Dim = %q, want it to contain the original text", got)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("Dim = %q, want ANSI codes when color is enabled", got)
	}
}

func TestDim_PlainWhenDisabled(t *testing.T) {
	Disable()
	defer EnableForTests()

	if got := Dim("profile: sandbox"); got != "profile: sandbox" {
		t.Errorf("Dim with color disabled = %q, want the text unchanged", got)
	}
}
