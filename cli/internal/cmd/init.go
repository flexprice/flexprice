package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/flexprice/cli/internal/style"
)

// printInitBanner writes the wordmark and tagline. Split out from
// newInitCommand's RunE so it can be tested without exercising the full login
// flow, which needs a real terminal or --api-key. Also reused by the root
// command's bare invocation.
//
// The wordmark is drawn with box/block characters, so it carries the message
// on its own; color is decoration layered on top and this stays readable under
// --no-color, NO_COLOR and piped output.
// terminalWidth reports the width of the terminal attached to stdout, or 0
// when there is none (output piped or redirected). style.Logo treats 0 as
// "unknown" and picks its conservative form rather than guessing wide.
func terminalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0
	}
	return w
}

func printInitBanner(w io.Writer, g *Globals) {
	if g.Quiet {
		return
	}
	fmt.Fprint(w, style.Logo(terminalWidth()))
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
