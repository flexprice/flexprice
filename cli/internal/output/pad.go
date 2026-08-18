package output

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// gutter is the minimum gap between columns.
const gutter = 2

// lipglossWidth is a thin alias so tests and callers measure width the same way
// the padder does.
func lipglossWidth(s string) int { return lipgloss.Width(s) }

// PadGrid aligns a grid of cells into lines, measuring VISIBLE width.
//
// text/tabwriter cannot be used for anything that might carry colour: it counts
// raw bytes, so ~20 invisible escape bytes per styled cell inflate its
// calculation and misalign every column. lipgloss.Width is ANSI-aware and also
// handles wide East-Asian runes correctly.
//
// Shared by the table renderer and `env list`. Extracting it is what stops the
// tabwriter defect being reintroduced the next time a plain listing is given
// colour — env list carried exactly that bug in waiting.
//
// The final column is never padded: trailing whitespace is invisible on screen
// but shows up in golden-file comparisons and when piping into other tools.
func PadGrid(grid [][]string) []string {
	if len(grid) == 0 {
		return nil
	}

	widest := 0
	for _, row := range grid {
		if len(row) > widest {
			widest = len(row)
		}
	}

	widths := make([]int, widest)
	for _, row := range grid {
		for i, cell := range row {
			if n := lipgloss.Width(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	lines := make([]string, 0, len(grid))
	for _, row := range grid {
		var b strings.Builder
		for i, cell := range row {
			b.WriteString(cell)
			if i < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(cell)+gutter))
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}
