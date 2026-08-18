package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/flexprice/cli/internal/client"
	specdata "github.com/flexprice/cli/spec"
)

func newOpenCommand(g *Globals, version string) *cobra.Command {
	open := &cobra.Command{Use: "open", Short: "Open Flexprice in your browser"}

	open.AddCommand(&cobra.Command{
		Use:   "dashboard",
		Short: "Open the Flexprice dashboard",
		RunE: func(c *cobra.Command, _ []string) error {
			return openURL("https://admin.flexprice.io")
		},
	})

	open.AddCommand(&cobra.Command{
		Use:   "webhooks",
		Short: "Open the webhook portal for this environment",
		RunE: func(c *cobra.Command, _ []string) error {
			rc, _, err := runtimeContext(g)
			if err != nil {
				return err
			}
			cl := client.New(client.Options{
				BaseURL: rc.BaseURL, APIKey: rc.APIKey, Version: version,
				Debug: g.Debug, DebugOut: os.Stderr,
			})
			// GET /webhooks/dashboard carries no OpenAPI annotations, so it is
			// unreachable through the registry (see cli/spec/commands.yaml); it is
			// called by literal path here, the same way /environments is.
			raw, err := cl.Do(c.Context(), http.MethodGet, "/webhooks/dashboard", nil, nil)
			if err != nil {
				return err
			}
			var resp struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				return fmt.Errorf("parse dashboard response: %w", err)
			}
			if resp.URL == "" {
				return fmt.Errorf("no webhook portal URL was returned")
			}
			g.UI.Info("Add your tunnel URL as an endpoint here:")
			g.UI.Data("%s", resp.URL)
			return openURL(resp.URL)
		},
	})

	return open
}

// openURL is only ever called with a URL the CLI printed itself: the fixed
// dashboard URL above, or a "url" field parsed out of a JSON API response. It
// is never called with user-supplied input. exec.Command passes url as a single
// argv element to the OS opener binary rather than through a shell, so even a
// malicious value could not inject additional shell commands.
func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		// Not fatal: the URL was already printed to stdout.
		fmt.Fprintf(os.Stderr, "Could not open a browser (%v). Open the URL above manually.\n", err)
	}
	return nil
}

// newVersionCommand reports the binary version and the spec build it embeds, so
// a 404 on a known command can be diagnosed as version skew. Design doc §12.
func newVersionCommand(g *Globals, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version and embedded spec build",
		Run: func(c *cobra.Command, _ []string) {
			g.UI.Data("flexprice %s", version)
			g.UI.Data("embedded OpenAPI spec: %d bytes", len(specdata.OpenAPI))
		},
	}
}
