package style

import "strings"

// logoWide is the full block wordmark, 67 columns across.
var logoWide = []string{
	`███████╗██╗     ███████╗██╗  ██╗██████╗ ██████╗ ██╗ ██████╗███████╗`,
	`██╔════╝██║     ██╔════╝╚██╗██╔╝██╔══██╗██╔══██╗██║██╔════╝██╔════╝`,
	`█████╗  ██║     █████╗   ╚███╔╝ ██████╔╝██████╔╝██║██║     █████╗  `,
	`██╔══╝  ██║     ██╔══╝   ██╔██╗ ██╔═══╝ ██╔══██╗██║██║     ██╔══╝  `,
	`██║     ███████╗███████╗██╔╝ ██╗██║     ██║  ██║██║╚██████╗███████╗`,
	`╚═╝     ╚══════╝╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝  ╚═╝╚═╝ ╚═════╝╚══════╝`,
}

// logoCompact is the single-height wordmark, 26 columns across.
var logoCompact = []string{
	`┌─┐┬  ┌─┐─┐ ┬┌─┐┬─┐┬┌─┐┌─┐`,
	`├┤ │  ├┤ ┌┴┬┘├─┘├┬┘││  ├┤ `,
	`└  ┴─┘└─┘┴ └─┴  ┴└─┴└─┘└─┘`,
}

// logoWideMinWidth is the terminal width below which the wide wordmark wraps.
// It carries a small margin over the art's own 67 columns so the logo never
// sits flush against the right edge.
const logoWideMinWidth = 72

// Width 0 means the terminal size is unknown (piped output) and falls back to
// the compact form: a wrapped logo reads as corruption.
func Logo(width int) string {
	art := logoCompact
	if width >= logoWideMinWidth {
		art = logoWide
	}

	var b strings.Builder
	for _, line := range art {
		b.WriteString(Default().styled(line, colorMagenta, false))
		b.WriteString("\n")
	}
	return b.String()
}
