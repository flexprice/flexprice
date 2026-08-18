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

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/output"
	"github.com/flexprice/cli/internal/spec"
)

// Builds the command tree from the embedded spec at startup; no generated code.
func addResourceCommands(root *cobra.Command, reg *spec.Registry, g *Globals, version string) {
	for _, resource := range reg.Resources() {
		entry, known := resourceGroups[resource]
		short := entry.Short
		if !known {
			// Unmapped resources still appear under cobra's "Additional
			// Commands". GroupID stays empty: an unregistered ID panics at
			// Execute().
			short = fmt.Sprintf("Operations on %s", resource)
		}
		parent := &cobra.Command{
			Use:     resource,
			Short:   short,
			GroupID: entry.GroupID,
		}
		for _, action := range reg.Actions(resource) {
			cmd, _ := reg.Lookup(resource, action)
			parent.AddCommand(newOperationCommand(cmd, reg, g, version))
		}
		root.AddCommand(parent)
	}

	root.AddCommand(&cobra.Command{
		Use:     "resources",
		Short:   "List every resource this CLI can act on",
		GroupID: groupAdvanced,
		RunE: func(c *cobra.Command, _ []string) error {
			for _, r := range reg.Resources() {
				g.UI.Data("%-28s %s", r, strings.Join(reg.Actions(r), ", "))
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
		// Body fields are not typed flags: 198 operations, and
		// CreateSubscriptionRequest alone has 37 top-level properties. Unknown
		// flags are collected and validated against the spec instead.
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

			if err := confirm(g, cmd, in.PositionalID, force); err != nil {
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
			sp := g.UI.Spinner(spinnerVerb(cmd) + " " + cmd.Resource + "\u2026")
			defer sp.Stop()

			for {
				spec.ApplyPaging(&req, cmd, spec.Paging{Limit: pageSize, Offset: offset})

				raw, err := cl.Do(cc.Context(), req.Method, req.Path, req.Query, req.Body)
				if err != nil {
					sp.Stop()
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
					sp.Stop()
					return err
				}
				// On each completed page, not on a timer, so a stall reads as
				// a frozen count.
				sp.Update(fmt.Sprintf("fetched %d of %d\u2026", shown, page.Total))
			}
			sp.Stop()

			format, err := output.ParseFormat(g.Output)
			if err != nil {
				return err
			}
			w := output.Writer{Out: os.Stdout, Err: os.Stderr, Format: format}
			res, err := w.RenderResult(merged, output.Options{
				Columns: pickColumns(reg, g, cmd.Resource),
				Quiet:   g.Quiet,
				Shown:   shown,
				Total:   page.Total,
			})
			if err != nil {
				return err
			}
			if res.Empty {
				g.UI.EmptyState(cmd.Resource)
				return nil
			}
			if shouldShowFooter(format) {
				g.UI.StatusLine(statusLine(rc, version))
			}
			if verb, ok := receiptVerbs[cmd.Action]; ok {
				g.UI.Receipt(verb, singular(cmd.Resource), responseID(merged))
			}
			return nil
		},
	}

	c.Flags().StringVar(&dataArg, "data", "", "request body: @file.json, - for stdin, or a JSON literal")
	c.Flags().BoolVar(&edit, "edit", false, "open $EDITOR with a pre-filled request body")
	c.Flags().BoolVar(&force, "force", false, "skip the confirmation prompt on destructive actions")
	return c
}

// Actions that cannot be undone. Confirmed regardless of environment: the CLI
// has no signal for which one it is pointed at (ADR 0003). Hand-maintained —
// deriving it from the spec's x-scope: delete would need registry changes.
var destructive = map[string]bool{
	"delete": true, "void": true, "terminate": true, "cancel": true, "archive": true,
	"finalize": true,
}

func confirm(g *Globals, cmd spec.Command, target string, force bool) error {
	if !destructive[cmd.Action] {
		return nil
	}
	subject := target
	if subject == "" {
		subject = cmd.Resource
	}
	return confirmAction(g, cmd.Action, subject, force)
}

// Shared by the spec-driven commands and the raw escape hatch, neither of which
// always has a spec.Command to hand. ui.Confirm refuses rather than proceeding
// when nobody can be asked.
func confirmAction(g *Globals, action, subject string, force bool) error {
	if force {
		return nil
	}
	return g.UI.Confirm(action, subject)
}

// Read actions are absent deliberately: "Retrieved customer X" adds nothing to
// the output directly above it.
var receiptVerbs = map[string]string{
	"create":    "Created",
	"update":    "Updated",
	"delete":    "Deleted",
	"void":      "Voided",
	"cancel":    "Cancelled",
	"terminate": "Terminated",
	"archive":   "Archived",
	"finalize":  "Finalized",
}

// Returns "" when there is no top-level id, which makes Receipt stay silent.
func responseID(raw []byte) string {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	id, _ := doc["id"].(string)
	return id
}

// Cosmetic, for the receipt line. Irregular plurals are left alone: a wrong
// singular is more jarring than an unchanged plural.
func singular(resource string) string {
	if len(resource) > 1 && strings.HasSuffix(resource, "s") &&
		!strings.HasSuffix(resource, "ss") {
		return strings.TrimSuffix(resource, "s")
	}
	return resource
}

// Only table output gets a footer: someone piping json or yaml is scripting,
// not reading a status line. Extracted so the rule stays under test.
func shouldShowFooter(format output.Format) bool {
	return format == output.FormatTable
}

// Unknown actions fall back to "Working on": vague but never wrong.
func spinnerVerb(cmd spec.Command) string {
	switch cmd.Action {
	case "list", "retrieve", "get":
		return "Fetching"
	case "create":
		return "Creating"
	case "update":
		return "Updating"
	case "delete":
		return "Deleting"
	case "void", "cancel", "terminate", "archive":
		return "Updating"
	case "finalize":
		return "Finalizing"
	default:
		return "Working on"
	}
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

// Lists settable fields, and says plainly when flags cannot express the body.
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

// Gathers --key=value pairs cobra did not recognise.
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

// Accepts @file, - for stdin, or a JSON literal.
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

	editorCmd, editorArgs := splitEditorCommand(editor)
	if editorCmd == "" {
		return nil, fmt.Errorf("no editor found — set $EDITOR, or pass the body with --data @file.json")
	}

	ed := exec.Command(editorCmd, append(editorArgs, path)...)
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

// Splits $EDITOR on whitespace so values with flags (EDITOR="code --wait")
// resolve to a real binary. Not shell parsing: quoted arguments are unsupported.
func splitEditorCommand(raw string) (cmd string, args []string) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
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

// os.ReadFile cannot be used: stdin is a pipe, and os.Stdin.Name() is not
// portably openable.
func readAll(f *os.File) ([]byte, error) {
	info, err := f.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return nil, fmt.Errorf("no data on stdin — pipe JSON in, or use --data @file.json")
	}
	return io.ReadAll(f)
}
