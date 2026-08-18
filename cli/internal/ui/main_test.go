package ui

import (
	"os"
	"testing"

	"github.com/flexprice/cli/internal/style"
)

// Forces true-colour so this package's gating is the only thing that can
// suppress colour; otherwise lipgloss suppresses it under go test regardless.
func TestMain(m *testing.M) {
	style.EnableForTests()
	os.Exit(m.Run())
}
