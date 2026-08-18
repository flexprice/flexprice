package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/flexprice/cli/internal/style"
)

// printInitBanner writes the bordered welcome box. Split out from
// newInitCommand's RunE so it can be tested without exercising the full login
// flow, which needs a real terminal or --api-key. Also reused by the root
// command's bare invocation.
//
// The box drawing and wording carry the message on their own; color is
// decoration layered on top, so this stays readable under --no-color, NO_COLOR
// and piped output.
func printInitBanner(w io.Writer, g *Globals) {
	if g.Quiet {
		return
	}

	// Size the border to the rendered content rather than hardcoding a width:
	// a literal run of ─ drifts out of alignment the moment the wording
	// changes, and lipgloss.Width measures visible width so the styling
	// applied below does not inflate the count.
	const pad = 2
	title := style.Header("Welcome to") + " " + style.Accent("Flexprice")
	inner := lipgloss.Width(title) + pad*2
	bar := strings.Repeat("─", inner)
	spaces := strings.Repeat(" ", pad)

	fmt.Fprintln(w, style.Accent("┌"+bar+"┐"))
	fmt.Fprintf(w, "%s%s%s%s%s\n", style.Accent("│"), spaces, title, spaces, style.Accent("│"))
	fmt.Fprintln(w, style.Accent("└"+bar+"┘"))
	fmt.Fprintln(w, "Usage-based billing from your terminal")
	fmt.Fprintln(w)
}

// newInitCommand is the guided first run: login, then tell the user what to do next.
func newInitCommand(g *Globals, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Set up the CLI (guided)",
		RunE: func(c *cobra.Command, args []string) error {
			printInitBanner(os.Stderr, g)
			fmt.Fprintln(os.Stderr, "Your API key is scoped to one environment — you can add more later with `flexprice login`.")
			fmt.Fprintln(os.Stderr)

			login := newLoginCommand(g, version)
			login.SetContext(c.Context())
			if err := login.RunE(login, nil); err != nil {
				return err
			}

			fmt.Fprintln(os.Stderr, "\nNext steps:")
			fmt.Fprintln(os.Stderr, "  flexprice whoami            confirm what you are pointed at")
			fmt.Fprintln(os.Stderr, "  flexprice resources         see everything you can act on")
			fmt.Fprintln(os.Stderr, "  flexprice customers list    try a read")
			fmt.Fprintln(os.Stderr, "  flexprice env list          see your other environments")
			return nil
		},
	}
}
