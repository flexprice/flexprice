package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/flexprice/cli/internal/config"
	"github.com/flexprice/cli/internal/keyring"
	"github.com/flexprice/cli/internal/spec"
	"github.com/flexprice/cli/internal/style"
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

	// Flags are not populated until Execute() parses them, so --no-color
	// cannot be applied at construction time — this hook is the first point
	// where g.NoColor is real.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if g.NoColor {
			style.Disable()
		}
		return nil
	}

	// A fresh install gets the wordmark and a pointer at init rather than a
	// wall of help for 40+ commands it cannot use yet. Once a config exists,
	// this reverts to normal help — the banner is a first-run affordance, not
	// something to sit through on every bare invocation.
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if hasExistingConfig() {
			return cmd.Help()
		}
		out := cmd.ErrOrStderr()
		printInitBanner(out, g)
		fmt.Fprintf(out, "  Get started   %s\n", style.Accent("flexprice init"))
		fmt.Fprintf(out, "  Docs          %s\n", "https://docs.flexprice.io/cli")
		return nil
	}

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

	if doc, err := spec.Load(); err == nil {
		if reg, err := spec.NewRegistry(doc); err == nil {
			addResourceCommands(root, reg, g, version)
			// Derived-name warnings are diagnostics, not errors: an unmapped
			// operation still works, it just has a machine-chosen name.
			if g.Debug {
				for _, warning := range reg.Warnings() {
					fmt.Fprintln(os.Stderr, "warning:", warning)
				}
			}
		}
	}
	addRawCommands(root, g, version)

	return root
}

// statusLine formats the context footer shown under table output: which
// profile and region a command actually ran against.
//
// This exists partly to soften the gap recorded in ADR 0003 — the CLI cannot
// tell which environment a key belongs to, since no endpoint reports it, but
// it can always show which profile was used, which is the next best signal for
// "am I pointed where I think I am".
func statusLine(rc config.RuntimeContext, version string) string {
	parts := []string{"profile: " + rc.ProfileName}
	if rc.Profile.Region != "" {
		parts = append(parts, "region: "+rc.Profile.Region)
	}
	if rc.Profile.Label != "" {
		parts = append(parts, rc.Profile.Label)
	}
	return strings.Join(parts, " · ") + " · v" + version
}

// hasExistingConfig reports whether a config file already exists, without
// resolving credentials — used only to decide whether bare `flexprice` shows
// the welcome banner or normal help. A missing home directory or any other
// lookup failure is treated the same as "no config": show the banner, rather
// than erroring on a decision this lightweight.
func hasExistingConfig() bool {
	path, err := config.DefaultPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
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
	f.StringSliceVar(&g.Columns, "columns", nil, "columns to show in table output")
	f.IntVar(&g.Limit, "limit", 20, "maximum records to return")
	f.BoolVar(&g.All, "all", false, "page through every record (prints the last page; use --output json with --limit for bulk export)")
}
