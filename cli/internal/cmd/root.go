package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/flexprice/cli/internal/config"
	"github.com/flexprice/cli/internal/keyring"
	"github.com/flexprice/cli/internal/spec"
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

	bindGlobals(root.PersistentFlags(), g)

	root.AddCommand(
		newInitCommand(g, version),
		newLoginCommand(g, version),
		newLogoutCommand(g),
		newWhoamiCommand(g),
		newEnvCommand(g, version),
		newConfigCommand(g),
		newOpenCommand(g, version),
		newVersionCommand(g, version),
	)

	return root
}

// runtimeContext resolves credentials for the current invocation. Every command
// that talks to the API starts here, so precedence is applied in exactly one place.
func runtimeContext(g *Globals) (config.RuntimeContext, *config.Config, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return config.RuntimeContext{}, nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.RuntimeContext{}, nil, err
	}

	store, warn, err := keyring.Open()
	if err != nil {
		return config.RuntimeContext{}, nil, err
	}
	if warn != "" && !g.Quiet {
		fmt.Fprintln(os.Stderr, warn)
	}

	doc, err := spec.Load()
	if err != nil {
		return config.RuntimeContext{}, nil, err
	}
	regions := map[string]string{}
	for _, r := range spec.Regions(doc) {
		regions[r.Key] = r.BaseURL
	}

	rc, err := config.ResolveContext(cfg, store, config.Overrides{
		Profile: g.Profile,
		APIKey:  g.APIKey,
		BaseURL: g.BaseURL,
		Region:  g.Region,
		Regions: regions,
	})
	return rc, cfg, err
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
