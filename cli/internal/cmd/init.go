package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/flexprice/cli/internal/style"
)

// Returns 0 when stdout is piped or redirected; style.Logo treats 0 as unknown
// and picks its conservative form rather than guessing wide.
func terminalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0
	}
	return w
}

// Split out from newInitCommand so it can be tested without the login flow,
// and reused by the root command's bare invocation.
func printInitBanner(w io.Writer, g *Globals) {
	if g.Quiet {
		return
	}
	fmt.Fprint(w, style.Logo(terminalWidth()))
	fmt.Fprintln(w, "Usage-based billing from your terminal")
	fmt.Fprintln(w)
}

func newInitCommand(g *Globals, version string) *cobra.Command {
	return &cobra.Command{
		Use:     "init",
		Short:   "Set up the CLI (guided)",
		GroupID: groupSetup,
		RunE: func(c *cobra.Command, args []string) error {
			printInitBanner(os.Stderr, g)
			// Warmth is confined to init and login, where the user is a
			// newcomer rather than an operator.
			g.UI.Info("Welcome to Flexprice — let's get you set up.")
			g.UI.Info("Your API key is scoped to one environment — you can add more later with `flexprice login`.")
			g.UI.Info("")

			login := newLoginCommand(g, version)
			login.SetContext(c.Context())
			if err := login.RunE(login, nil); err != nil {
				return err
			}

			g.UI.Info("\nHere's what to try first:")
			g.UI.Info("  flexprice whoami            confirm what you are pointed at")
			g.UI.Info("  flexprice resources         see everything you can act on")
			g.UI.Info("  flexprice customers list    try a read")
			g.UI.Info("  flexprice env list          see your other environments")
			return nil
		},
	}
}
