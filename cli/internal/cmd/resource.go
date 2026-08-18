package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/output"
	"github.com/flexprice/cli/internal/spec"
)

// addResourceCommands builds the command tree from the registry at startup.
// There is no generated code: the tree is derived from the embedded spec.
func addResourceCommands(root *cobra.Command, reg *spec.Registry, g *Globals, version string) {
	for _, resource := range reg.Resources() {
		parent := &cobra.Command{
			Use:   resource,
			Short: fmt.Sprintf("Operations on %s", resource),
		}
		for _, action := range reg.Actions(resource) {
			cmd, _ := reg.Lookup(resource, action)
			parent.AddCommand(newOperationCommand(cmd, reg, g, version))
		}
		root.AddCommand(parent)
	}

	root.AddCommand(&cobra.Command{
		Use:   "resources",
		Short: "List every resource this CLI can act on",
		RunE: func(c *cobra.Command, _ []string) error {
			for _, r := range reg.Resources() {
				fmt.Fprintf(os.Stdout, "%-28s %s\n", r, strings.Join(reg.Actions(r), ", "))
			}
			return nil
		},
	})
}

func newOperationCommand(cmd spec.Command, reg *spec.Registry, g *Globals, version string) *cobra.Command {
	var (
		dataArg string
		edit    bool
		force   bool
	)

	fields := spec.BodyFields(cmd)
	c := &cobra.Command{
		Use:   cmd.Action,
		Short: operationSummary(cmd),
		Long:  operationHelp(cmd, fields),
		Args:  cobra.MaximumNArgs(1),
		// Body fields are not declared as typed flags: the spec has 198 operations
		// and CreateSubscriptionRequest alone has 37 top-level properties. Unknown
		// flags are collected and validated against the spec instead. Design doc §7.
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		RunE: func(cc *cobra.Command, args []string) error {
			in := spec.Input{Flags: map[string]string{}}
			if len(args) == 1 {
				in.PositionalID = args[0]
			}
			for k, v := range collectUnknownFlags(cc) {
				in.Flags[k] = v
			}

			switch {
			case edit:
				doc, err := editSkeleton(cmd)
				if err != nil {
					return err
				}
				in.Data = doc
			case dataArg != "":
				doc, err := readDataArg(dataArg)
				if err != nil {
					return err
				}
				in.Data = doc
			}

			if err := confirm(cmd, in.PositionalID, force); err != nil {
				return err
			}

			req, err := spec.BuildRequest(cmd, in)
			if err != nil {
				return err
			}

			rc, _, err := runtimeContext(g)
			if err != nil {
				return err
			}
			cl := client.New(client.Options{
				BaseURL: rc.BaseURL, APIKey: rc.APIKey, Version: version,
				Debug: g.Debug, DebugOut: os.Stderr,
			})

			pageSize := g.Limit
			if pageSize <= 0 {
				pageSize = 20
			}

			var (
				merged []byte
				page   spec.Page
				offset int
				shown  int
			)
			for {
				spec.ApplyPaging(&req, cmd, spec.Paging{Limit: pageSize, Offset: offset})

				raw, err := cl.Do(cc.Context(), req.Method, req.Path, req.Query, req.Body)
				if err != nil {
					return err
				}

				page, _ = spec.PageInfo(raw)
				shown += page.Count
				merged = raw

				if !g.All || !page.HasMore(shown) || page.Count == 0 {
					break
				}
				offset += page.Count

				// Rebuild so the next iteration starts from a clean query and body.
				req, err = spec.BuildRequest(cmd, in)
				if err != nil {
					return err
				}
				if !g.Quiet {
					fmt.Fprintf(os.Stderr, "\rfetched %d of %d\u2026", shown, page.Total)
				}
			}
			if g.All && !g.Quiet && shown > 0 {
				fmt.Fprintln(os.Stderr)
			}

			format, err := output.ParseFormat(g.Output)
			if err != nil {
				return err
			}
			w := output.Writer{Out: os.Stdout, Err: os.Stderr, Format: format}
			return w.Render(merged, output.Options{
				Columns: pickColumns(reg, g, cmd.Resource),
				Quiet:   g.Quiet,
				Shown:   shown,
				Total:   page.Total,
			})
		},
	}

	c.Flags().StringVar(&dataArg, "data", "", "request body: @file.json, - for stdin, or a JSON literal")
	c.Flags().BoolVar(&edit, "edit", false, "open $EDITOR with a pre-filled request body")
	c.Flags().BoolVar(&force, "force", false, "skip the confirmation prompt on destructive actions")
	return c
}

// destructive lists the actions that cannot be undone. Because the CLI cannot
// tell a production environment from a development one, every destructive action
// is confirmed regardless of where it is pointed — there is no environment signal
// to be selective with.
var destructive = map[string]bool{
	"delete": true, "void": true, "terminate": true, "cancel": true, "archive": true,
}

// confirm prompts before a destructive action. It returns nil when stdin is not a
// terminal, so scripts and CI are never blocked; --force skips it entirely.
func confirm(cmd spec.Command, target string, force bool) error {
	if force || !destructive[cmd.Action] {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil
	}

	subject := target
	if subject == "" {
		subject = cmd.Resource
	}
	fmt.Fprintf(os.Stderr, "This will %s %s and cannot be undone.\nContinue? [y/N]: ", cmd.Action, subject)

	var answer string
	_, _ = fmt.Fscanln(os.Stdin, &answer)
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		return fmt.Errorf("cancelled")
	}
	return nil
}

func pickColumns(reg *spec.Registry, g *Globals, resource string) []string {
	if len(g.Columns) > 0 {
		return g.Columns
	}
	return reg.Columns(resource)
}

func operationSummary(cmd spec.Command) string {
	if s := cmd.Operation.Op.Summary; s != "" {
		return s
	}
	return fmt.Sprintf("%s %s", cmd.Operation.Method, cmd.Operation.Path)
}

// operationHelp lists settable fields and states plainly when flags are not
// enough for this operation's body.
func operationHelp(cmd spec.Command, fields []spec.Field) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n%s %s\n", operationSummary(cmd), cmd.Operation.Method, cmd.Operation.Path)

	if len(fields) == 0 {
		return b.String()
	}

	nested := 0
	var flat, deep []string
	for _, f := range fields {
		label := f.Name
		if f.Required {
			label += "  (required)"
		}
		if f.Nested {
			nested++
			deep = append(deep, fmt.Sprintf("  %s  [%s]", label, f.Type))
			continue
		}
		flat = append(flat, fmt.Sprintf("  --%s  [%s]", label, f.Type))
	}
	sort.Strings(flat)
	sort.Strings(deep)

	if len(flat) > 0 {
		fmt.Fprintf(&b, "\nFields you can set with flags:\n%s\n", strings.Join(flat, "\n"))
	}
	if nested > 0 {
		fmt.Fprintf(&b, "\nNested fields — these cannot be set with flags:\n%s\n", strings.Join(deep, "\n"))
		fmt.Fprintf(&b, "\nUse --edit to fill in a pre-built request body, or --data @file.json.\n")
	}
	return b.String()
}

// collectUnknownFlags gathers --key=value pairs cobra did not recognise.
func collectUnknownFlags(c *cobra.Command) map[string]string {
	out := map[string]string{}
	for i, raw := range os.Args {
		if !strings.HasPrefix(raw, "--") {
			continue
		}
		body := strings.TrimPrefix(raw, "--")
		if key, value, found := strings.Cut(body, "="); found {
			if c.Flags().Lookup(key) == nil && c.InheritedFlags().Lookup(key) == nil {
				out[key] = value
			}
			continue
		}
		// --key value form
		if c.Flags().Lookup(body) == nil && c.InheritedFlags().Lookup(body) == nil {
			if i+1 < len(os.Args) && !strings.HasPrefix(os.Args[i+1], "--") {
				out[body] = os.Args[i+1]
			}
		}
	}
	return out
}

// readDataArg accepts @file, - for stdin, or a JSON literal.
func readDataArg(arg string) (map[string]any, error) {
	var raw []byte
	var err error

	switch {
	case arg == "-":
		raw, err = readAll(os.Stdin)
	case strings.HasPrefix(arg, "@"):
		raw, err = os.ReadFile(strings.TrimPrefix(arg, "@"))
	default:
		raw = []byte(arg)
	}
	if err != nil {
		return nil, fmt.Errorf("read --data: %w", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("--data is not valid JSON: %w", err)
	}
	return doc, nil
}

// editSkeleton writes a skeleton to a temp file, opens $EDITOR, and parses the result.
func editSkeleton(cmd spec.Command) (map[string]any, error) {
	skeleton, err := spec.Skeleton(cmd)
	if err != nil {
		return nil, err
	}

	f, err := os.CreateTemp("", "flexprice-*.json")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	path := f.Name()
	defer func() { _ = os.Remove(path) }()

	if _, err := f.WriteString(skeleton); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write skeleton: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close skeleton: %w", err)
	}

	editor, err := resolveEditor()
	if err != nil {
		return nil, err
	}

	ed := exec.Command(editor, path)
	ed.Stdin, ed.Stdout, ed.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := ed.Run(); err != nil {
		return nil, fmt.Errorf("editor %s exited with an error: %w", editor, err)
	}

	edited, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read edited file: %w", err)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(spec.StripComments(string(edited))), &doc); err != nil {
		return nil, fmt.Errorf("the edited body is not valid JSON: %w", err)
	}
	return doc, nil
}

func resolveEditor() (string, error) {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if v := os.Getenv(env); v != "" {
			return v, nil
		}
	}
	fallback := "vi"
	if runtime.GOOS == "windows" {
		fallback = "notepad"
	}
	if _, err := exec.LookPath(fallback); err != nil {
		return "", fmt.Errorf(
			"no editor found — set $EDITOR, or pass the body with --data @file.json")
	}
	return fallback, nil
}

// readAll drains stdin. os.ReadFile cannot be used here: stdin is a pipe, not a
// path, and os.Stdin.Name() is not portably openable.
func readAll(f *os.File) ([]byte, error) {
	info, err := f.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return nil, fmt.Errorf("no data on stdin — pipe JSON in, or use --data @file.json")
	}
	return io.ReadAll(f)
}
