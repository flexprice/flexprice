package cmd

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"github.com/flexprice/cli/internal/config"
	"github.com/flexprice/cli/internal/keyring"
	"github.com/flexprice/cli/internal/spec"
	"github.com/flexprice/cli/internal/style"
	"github.com/flexprice/cli/internal/ui"
)

// Globals holds the values bound to the root command's persistent flags.
// Created per root and threaded into subcommands explicitly: pflag writes each
// flag's default into the bound pointer at registration time, so a shared
// instance is clobbered the moment a second root is constructed.
type Globals struct {
	Profile string
	Output  string
	APIKey  string
	BaseURL string
	Region  string
	Quiet   bool
	Debug   bool
	NoColor bool
	NoInput bool
	Limit   int
	All     bool
	Columns []string

	// Replaced in PersistentPreRunE once flags are parsed; set at construction
	// only so a directly-constructed command in a test never sees nil.
	UI *ui.UI
}

func NewRootCommand(version string) *cobra.Command {
	g := &Globals{}
	g.UI = ui.FromEnv(false, false, true)

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

	// Flags are not populated until Execute() parses them, so this hook is the
	// first point where g's fields are real.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if g.NoColor {
			style.Disable()
		}
		g.UI = ui.FromEnv(g.Quiet, g.NoInput, !g.NoColor)
		return nil
	}

	// A fresh install gets the wordmark and a pointer at init rather than a wall
	// of help for 40+ commands it cannot use yet.
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

	// Must run before any command carrying a GroupID reaches Execute: cobra
	// panics on an ID it does not know.
	root.AddGroup(commandGroups...)
	// help and completion are created during Execute, and land in "Additional
	// Commands" unless placed explicitly.
	root.SetHelpCommandGroupID(groupAdvanced)
	root.SetCompletionCommandGroupID(groupAdvanced)

	if doc, err := spec.Load(); err == nil {
		if reg, err := spec.NewRegistry(doc); err == nil {
			addResourceCommands(root, reg, g, version)
			// Derived-name warnings are diagnostics, not errors: an unmapped
			// operation still works, it just has a machine-chosen name.
			if g.Debug {
				for _, warning := range reg.Warnings() {
					g.UI.Info("warning: %s", warning)
				}
			}
		}
	}
	addRawCommands(root, g, version)

	// LAST, after every AddCommand: doing it earlier silently misses anything
	// added later, which is how raw get/post/delete once landed under
	// "Additional Commands" with every test still passing.
	for _, c := range root.Commands() {
		if id, ok := builtinGroups[c.Name()]; ok {
			c.GroupID = id
		}
	}

	registerGlobals(root, g)
	return root
}

// Lets tests reach a specific root's Globals. Keyed by command pointer so
// parallel tests constructing separate roots never observe each other's state.
var (
	rootGlobalsMu sync.Mutex
	rootGlobals   = map[*cobra.Command]*Globals{}
)

func registerGlobals(root *cobra.Command, g *Globals) {
	rootGlobalsMu.Lock()
	defer rootGlobalsMu.Unlock()
	rootGlobals[root] = g
}

// For tests only; production code receives *Globals by parameter.
func globalsFor(root *cobra.Command) *Globals {
	rootGlobalsMu.Lock()
	defer rootGlobalsMu.Unlock()
	return rootGlobals[root]
}

// The context footer under table output. Softens the gap in ADR 0003: the CLI
// cannot tell which environment a key belongs to, but it can always show which
// profile served the request.
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

// Called by main on every exit path: a spinner may have hidden the cursor, and
// an invisible cursor outlives the process. Harmless when no spinner ran.
func RestoreTerminal() {
	if term.IsTerminal(int(os.Stderr.Fd())) {
		fmt.Fprint(os.Stderr, "\x1b[?25h")
	}
}

// Decides only whether bare `flexprice` shows the banner or normal help, so
// any lookup failure is treated as "no config" rather than erroring.
func hasExistingConfig() bool {
	path, err := config.DefaultPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Every command that talks to the API starts here, so credential precedence is
// applied in exactly one place.
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
	if warn != "" {
		g.UI.Info("%s", warn)
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
	f.BoolVar(&g.NoInput, "no-input", false, "never prompt; fail instead of asking")
	f.StringSliceVar(&g.Columns, "columns", nil, "columns to show in table output")
	f.IntVar(&g.Limit, "limit", 20, "maximum records to return")
	f.BoolVar(&g.All, "all", false, "page through every record (prints the last page; use --output json with --limit for bulk export)")
}
