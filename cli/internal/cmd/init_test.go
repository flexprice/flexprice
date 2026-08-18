package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/flexprice/cli/internal/style"
)

// --quiet suppresses the decorative banner. This checks printInitBanner
// directly rather than running the whole init command, which would require a
// real terminal or --api-key; the full login flow is covered by auth_test.go.
func TestInitCommand_QuietSuppressesBanner(t *testing.T) {
	g := &Globals{Quiet: true}

	var out bytes.Buffer
	printInitBanner(&out, g)

	if strings.Contains(out.String(), "Welcome to Flexprice") {
		t.Errorf("banner printed despite --quiet: %q", out.String())
	}
}

func TestInitCommand_BannerShowsWithoutQuiet(t *testing.T) {
	g := &Globals{Quiet: false}

	var out bytes.Buffer
	printInitBanner(&out, g)

	if !strings.Contains(out.String(), "Welcome to Flexprice") {
		t.Errorf("banner missing without --quiet: %q", out.String())
	}
}

// The banner must stay readable with color disabled — the box drawing and
// wording carry the message, color is decoration on top. This is the
// NO_COLOR / --no-color / piped-output case.
func TestInitBanner_ReadableWithColorDisabled(t *testing.T) {
	style.Disable()
	defer style.EnableForTests()

	var out bytes.Buffer
	printInitBanner(&out, &Globals{})

	got := out.String()
	if strings.Contains(got, "\x1b[") {
		t.Errorf("banner contains ANSI codes with color disabled: %q", got)
	}
	if !strings.Contains(got, "Welcome to Flexprice") {
		t.Errorf("banner text missing with color disabled: %q", got)
	}
}
