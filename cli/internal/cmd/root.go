package cmd

import (
	"github.com/spf13/cobra"
)

// Globals are bound on the root command and read by every subcommand.
type Globals struct {
	Profile string
	Output  string
	APIKey  string
	BaseURL string
	Region  string
	Quiet   bool
	Debug   bool
	NoColor bool
	Limit   int
	All     bool
	Columns []string
}

var globals Globals

func NewRootCommand(version string) *cobra.Command {
	root := &cobra.Command{
		Use:     "flexprice",
		Short:   "Flexprice CLI — usage-based billing from your terminal",
		Version: version,
		Long: "Send events, inspect how they metered, and drive the Flexprice API " +
			"from your terminal.\n\nStart with: flexprice init",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Without a Run func, cobra's default help template skips the Usage/Flags
	// section entirely (it only renders when the command is Runnable or has
	// subcommands), so a bare invocation prints help explicitly.
	root.Run = func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	}

	f := root.PersistentFlags()
	f.StringVarP(&globals.Profile, "profile", "p", "", "profile to use for this command")
	f.StringVar(&globals.Output, "output", "table", "output format: table, json, yaml")
	f.StringVar(&globals.APIKey, "api-key", "", "API key (CI use; prefer flexprice login)")
	f.StringVar(&globals.BaseURL, "base-url", "", "override the API base URL")
	f.StringVar(&globals.Region, "region", "", "region key, e.g. us or in")
	f.BoolVar(&globals.Quiet, "quiet", false, "suppress progress output")
	f.BoolVar(&globals.Debug, "debug", false, "dump requests and responses, secrets redacted")
	f.BoolVar(&globals.NoColor, "no-color", false, "disable coloured output")

	return root
}
