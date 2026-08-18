package output

import (
	"strings"
	"testing"

	"github.com/flexprice/cli/internal/style"
)

// text/tabwriter counts escape bytes as visible width; this is the regression
// that misaligned every column once styling was on, and the reason env list
// could not simply be given colour while it still used tabwriter.
func TestPadGrid_MeasuresVisibleWidthNotBytes(t *testing.T) {
	style.EnableForTests()

	coloured := style.Header("ID")
	if !strings.Contains(coloured, "\x1b[") {
		t.Fatal("test setup: expected a styled header to contain escape codes")
	}

	lines := PadGrid([][]string{
		{coloured, "EMAIL"},
		{"cust_01", "ada@example.com"},
	})
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}

	// Both rows' second column must start at the same VISIBLE offset.
	if got, want := visibleIndexOf(lines[0], "EMAIL"), visibleIndexOf(lines[1], "ada@"); got != want {
		t.Errorf("columns misaligned: header col2 at %d, row col2 at %d\n%q\n%q",
			got, want, lines[0], lines[1])
	}
}

func TestPadGrid_NoTrailingWhitespace(t *testing.T) {
	lines := PadGrid([][]string{
		{"a", "bbbb"},
		{"cc", "d"},
	})
	for i, l := range lines {
		if l != strings.TrimRight(l, " ") {
			t.Errorf("line %d has trailing whitespace: %q", i, l)
		}
	}
}

// Wide runes occupy two cells. Measuring them as one character misaligns
// everything after them, which matters because the API returns non-ASCII
// values (environment names, customer names).
func TestPadGrid_HandlesWideRunes(t *testing.T) {
	lines := PadGrid([][]string{
		{"名前", "a"},
		{"ab", "b"},
	})
	if got, want := visibleIndexOf(lines[0], "a"), visibleIndexOf(lines[1], "b"); got != want {
		t.Errorf("wide runes misaligned: %d vs %d\n%q\n%q", got, want, lines[0], lines[1])
	}
}

func TestPadGrid_EmptyGrid(t *testing.T) {
	if got := PadGrid(nil); got != nil {
		t.Errorf("PadGrid(nil) = %v, want nil", got)
	}
}

// visibleIndexOf returns the visible-column offset at which sub starts.
func visibleIndexOf(line, sub string) int {
	idx := strings.LastIndex(line, sub)
	if idx < 0 {
		return -1
	}
	return lipglossWidth(line[:idx])
}
