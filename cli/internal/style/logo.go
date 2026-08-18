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

// Logo renders the Flexprice wordmark, sized to the given terminal width.
//
// A width of 0 means the terminal size could not be determined — typically
// because output is piped rather than attached to a terminal — and falls back
// to the compact form rather than optimistically emitting 67-column art into
// something that may not be that wide. A wrapped logo reads as corruption, so
// the fallback is deliberately conservative.
func Logo(width int) string {
	art := logoCompact
	if width >= logoWideMinWidth {
		art = logoWide
	}

	var b strings.Builder
	for _, line := range art {
		b.WriteString(styled(line, colorMagenta, false))
		b.WriteString("\n")
	}
	return b.String()
}
