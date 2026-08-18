package cmd

import (
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/output"
)

// rawDeleteConfirm gates a raw DELETE behind the same confirmation prompt the
// spec-driven destructive commands use. There is no spec.Command / resource
// name available on the raw path, so the request path itself is the subject.
func rawDeleteConfirm(path string, force bool) error {
	return confirmAction("delete", path, force)
}

// addRawCommands registers get/post/delete — the escape hatch for anything the
// resource tree does not cover, mirroring `stripe get /v1/...`.
func addRawCommands(root *cobra.Command, g *Globals, version string) {
	for _, m := range []struct {
		name, method, short string
		takesBody           bool
	}{
		{"get", http.MethodGet, "Issue a raw GET against the API", false},
		{"post", http.MethodPost, "Issue a raw POST against the API", true},
		{"delete", http.MethodDelete, "Issue a raw DELETE against the API", false},
	} {
		name, method, takesBody := m.name, m.method, m.takesBody
		var dataArg string
		var force bool

		c := &cobra.Command{
			Use:   m.name + " <path>",
			Short: m.short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cc *cobra.Command, args []string) error {
				var body any
				if takesBody && dataArg != "" {
					doc, err := readDataArg(dataArg)
					if err != nil {
						return err
					}
					body = doc
				}

				if name == "delete" {
					if err := rawDeleteConfirm(args[0], force); err != nil {
						return err
					}
				}

				rc, _, err := runtimeContext(g)
				if err != nil {
					return err
				}
				cl := client.New(client.Options{
					BaseURL: rc.BaseURL, APIKey: rc.APIKey, Version: version,
					Debug: g.Debug, DebugOut: os.Stderr,
				})

				raw, err := cl.Do(cc.Context(), method, args[0], nil, body)
				if err != nil {
					return err
				}

				format, err := output.ParseFormat(g.Output)
				if err != nil {
					return err
				}
				w := output.Writer{Out: os.Stdout, Err: os.Stderr, Format: format}
				return w.Render(raw, output.Options{Quiet: g.Quiet})
			},
		}
		if takesBody {
			c.Flags().StringVar(&dataArg, "data", "", "request body: @file.json, - for stdin, or a JSON literal")
		}
		if name == "delete" {
			c.Flags().BoolVar(&force, "force", false, "skip the confirmation prompt")
		}
		root.AddCommand(c)
	}
}
