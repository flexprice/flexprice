package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/flexprice/cli/internal/config"
)

func newConfigCommand(g *Globals) *cobra.Command {
	cfgCmd := &cobra.Command{Use: "config", Short: "Manage profiles"}

	cfgCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List stored profiles",
		RunE: func(c *cobra.Command, _ []string) error {
			path, err := config.DefaultPath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			if len(cfg.Profiles) == 0 {
				fmt.Fprintln(os.Stderr, "No profiles yet — run: flexprice init")
				return nil
			}

			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "PROFILE\tLABEL\tREGION\tDEFAULT")
			for name, p := range cfg.Profiles {
				marker := ""
				if name == cfg.DefaultProfile {
					marker = "*"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, p.Label, p.Region, marker)
			}
			return tw.Flush()
		},
	})

	cfgCmd.AddCommand(&cobra.Command{
		Use:   "use <profile>",
		Short: "Set the default profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			path, err := config.DefaultPath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			if _, ok := cfg.Profiles[args[0]]; !ok {
				return fmt.Errorf("profile %q not found — see: flexprice config list", args[0])
			}
			cfg.DefaultProfile = args[0]
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Default profile is now %q\n", args[0])
			return nil
		},
	})

	return cfgCmd
}
