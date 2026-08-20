package output

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const gutter = 2

// So tests measure width the same way the padder does.
func lipglossWidth(s string) int { return lipgloss.Width(s) }

// Measures VISIBLE width, unlike text/tabwriter which counts escape bytes.
// The final column is never padded, to avoid trailing whitespace.
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
