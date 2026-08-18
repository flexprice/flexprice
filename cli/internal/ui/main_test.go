package ui

import (
	"os"
	"testing"

	"github.com/flexprice/cli/internal/style"
)

// TestMain forces a true-colour renderer profile for the whole package.
//
// Without it, lipgloss's own auto-detection correctly sees no terminal under
// `go test` and suppresses colour on its own. That second suppression layer
// makes every "no escape codes were emitted" assertion vacuous: the test would
// pass even with this package's stream gates deleted outright — verified by
// mutating New to drop them, at which point the tests still passed.
//
// Forcing the profile leaves this package's own gating as the only thing that
// can suppress colour, which is what those tests are meant to be measuring.
func TestMain(m *testing.M) {
	style.EnableForTests()
	os.Exit(m.Run())
}
