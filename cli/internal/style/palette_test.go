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
