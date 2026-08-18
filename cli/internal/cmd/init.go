package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newInitCommand is the guided first run: login, then tell the user what to do next.
func newInitCommand(g *Globals, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Set up the CLI (guided)",
		RunE: func(c *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "Setting up the Flexprice CLI.")
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
