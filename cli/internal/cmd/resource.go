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

// addResourceCommands builds the command tree from the registry at startup.
// There is no generated code: the tree is derived from the embedded spec.
func addResourceCommands(root *cobra.Command, reg *spec.Registry, g *Globals, version string) {
	for _, resource := range reg.Resources() {
		entry, known := resourceGroups[resource]
		short := entry.Short
		if !known {
			// Unmapped resources still appear, under cobra's built-in
			// "Additional Commands" heading. GroupID stays empty deliberately:
			// an unregistered ID would panic at Execute().
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
				// Tick on each COMPLETED page rather than on a timer, so a
				// stalled page shows as a frozen count instead of an animation
				// implying progress it is not making.
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

// destructive lists the actions that cannot be undone. Because the CLI cannot
// tell a production environment from a development one, every destructive action
// is confirmed regardless of where it is pointed — there is no environment signal
// to be selective with.
//
// This is hand-maintained. Ideally it would be derived from the spec's
// x-scope: delete extension instead — finalizeInvoice, for example, is a POST
// tagged x-scope: delete in openapi.json because it is irreversible — but
// wiring that through the registry is a larger refactor than this fix.
var destructive = map[string]bool{
	"delete": true, "void": true, "terminate": true, "cancel": true, "archive": true,
	"finalize": true,
}

// confirm prompts before a destructive spec-driven action. --force skips it.
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

// confirmAction prompts before a destructive action described by action and
// subject (e.g. "delete", "/v1/customers/cust_123") — shared by the
// spec-driven commands and the raw get/post/delete escape hatch, neither of
// which always has a spec.Command to hand.
//
// The TTY check now lives in ui.Confirm, which REFUSES rather than proceeding
// when nobody can be asked. That is a behaviour change: this function
// previously returned nil on a non-terminal stdin, so a script piping into a
// destructive command deleted without confirmation. Failing until --force is
// supplied is the safer default.
func confirmAction(g *Globals, action, subject string, force bool) error {
	if force {
		return nil
	}
	return g.UI.Confirm(action, subject)
}

// receiptVerbs maps mutating actions to the past-tense verb shown in a
// receipt. Read actions are absent deliberately: "Retrieved customer X" tells
// the user nothing they cannot see in the output directly above it.
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

// responseID pulls the top-level "id" out of a response, or returns "" when
// there is not one. Returning "" makes Receipt silent, which is the intended
// behaviour when we cannot say precisely what happened.
func responseID(raw []byte) string {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	id, _ := doc["id"].(string)
	return id
}

// singular trims a trailing "s" for the receipt line, so the resource reads as
// one object. Resources whose plural is irregular are left alone: this is
// cosmetic, and a wrong singular is more jarring than an unchanged plural.
func singular(resource string) string {
	if len(resource) > 1 && strings.HasSuffix(resource, "s") &&
		!strings.HasSuffix(resource, "ss") {
		return strings.TrimSuffix(resource, "s")
	}
	return resource
}

// shouldShowFooter reports whether the status footer belongs under this
// output. Only table output gets one: someone piping json or yaml is
// scripting, not reading a status line, and CI commonly captures stderr
// alongside stdout where it would just be noise.
//
// Extracted rather than inlined so the rule stays under test after the footer
// moved out of internal/output, where it had its own test.
func shouldShowFooter(format output.Format) bool {
	return format == output.FormatTable
}

// spinnerVerb turns an action into the present participle shown while a request
// is in flight. Unknown actions fall back to "Working on", which is vague but
// never wrong — a misleading verb is worse than a general one.
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

// splitEditorCommand splits an $EDITOR/$VISUAL value into a command and its
// arguments on whitespace, so editors configured with flags (EDITOR="code
// --wait", EDITOR="subl -w") resolve to a real binary instead of one invalid
// argv[0] containing a space. This is plain whitespace splitting, not shell
// parsing — a quoted argument (e.g. a path containing spaces) is not supported.
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

// readAll drains stdin. os.ReadFile cannot be used here: stdin is a pipe, not a
// path, and os.Stdin.Name() is not portably openable.
func readAll(f *os.File) ([]byte, error) {
	info, err := f.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return nil, fmt.Errorf("no data on stdin — pipe JSON in, or use --data @file.json")
	}
	return io.ReadAll(f)
}
