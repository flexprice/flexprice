package ui

import (
	"os"
	"testing"

	"github.com/flexprice/cli/internal/style"
)

// Forces a true-colour profile so this package's gating is the only thing that
// can suppress colour. Without it lipgloss suppresses colour on its own under
// `go test`, which makes every "no escape codes" assertion vacuous — verified
// by deleting the gates and watching the tests still pass.
func TestMain(m *testing.M) {
	style.EnableForTests()
	os.Exit(m.Run())
}
