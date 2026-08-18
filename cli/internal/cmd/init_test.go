package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/flexprice/cli/internal/style"
)

// --quiet suppresses the whole banner. This checks printInitBanner directly
// rather than running the whole init command, which would require a real
// terminal or --api-key; the full login flow is covered by auth_test.go.
func TestInitCommand_QuietSuppressesBanner(t *testing.T) {
	var out bytes.Buffer
	printInitBanner(&out, &Globals{Quiet: true})

	if out.Len() != 0 {
		t.Errorf("banner printed despite --quiet: %q", out.String())
	}
}

func TestInitCommand_BannerShowsWithoutQuiet(t *testing.T) {
	var out bytes.Buffer
	printInitBanner(&out, &Globals{})

	got := out.String()
	// Assert on the tagline plus the presence of block/box characters, not on a
	// particular row: that would break on every retouch of the art.
	if !strings.Contains(got, "Usage-based billing from your terminal") {
		t.Errorf("banner missing the tagline: %q", got)
	}
	if !strings.ContainsAny(got, "█┌─┐") {
		t.Errorf("banner missing the wordmark art: %q", got)
	}
}

// The banner must stay readable with color disabled — the wordmark's own
// characters carry it, color is decoration on top. This is the NO_COLOR /
// --no-color / piped-output case.
func TestInitBanner_ReadableWithColorDisabled(t *testing.T) {
	style.Disable()
	defer style.EnableForTests()

	var out bytes.Buffer
	printInitBanner(&out, &Globals{})

	got := out.String()
	if strings.Contains(got, "\x1b[") {
		t.Errorf("banner contains ANSI codes with color disabled: %q", got)
	}
	if !strings.Contains(got, "Usage-based billing from your terminal") {
		t.Errorf("banner text missing with color disabled: %q", got)
	}
	if !strings.ContainsAny(got, "█┌─┐") {
		t.Errorf("wordmark art missing with color disabled: %q", got)
	}
}
