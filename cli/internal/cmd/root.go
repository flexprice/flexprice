package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Globals holds the values bound to the root command's persistent flags.
//
// Fields are exported because pflag binds to their addresses directly; a getter
// cannot serve as a flag target. An instance is created per root command and
// threaded into subcommands explicitly rather than kept in a package variable:
// pflag writes each flag's default into the bound pointer at registration time,
// so a shared instance is clobbered the moment a second root is constructed,
// which would break table-driven and parallel subcommand tests.
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

func NewRootCommand(version string) *cobra.Command {
	g := &Globals{}

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
	// subcommands), so a bare invocation prints help explicitly. Later tasks set
	// root.Run = nil once real subcommands make this redundant.
	root.Run = func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	}

	bindGlobals(root.PersistentFlags(), g)

	// Later tasks add subcommands here, each taking g as a parameter:
	//   root.AddCommand(newLoginCommand(g, version), ...)

	return root
}

func bindGlobals(f *pflag.FlagSet, g *Globals) {
	f.StringVarP(&g.Profile, "profile", "p", "", "profile to use for this command")
	f.StringVar(&g.Output, "output", "table", "output format: table, json, yaml")
	f.StringVar(&g.APIKey, "api-key", "", "API key (CI use; prefer flexprice login)")
	f.StringVar(&g.BaseURL, "base-url", "", "override the API base URL")
	f.StringVar(&g.Region, "region", "", "region key, e.g. us or in")
	f.BoolVar(&g.Quiet, "quiet", false, "suppress progress output")
	f.BoolVar(&g.Debug, "debug", false, "dump requests and responses, secrets redacted")
	f.BoolVar(&g.NoColor, "no-color", false, "disable coloured output")
}
