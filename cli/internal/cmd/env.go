package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/output"
	"github.com/flexprice/cli/internal/style"
)

// newEnvCommand lists the tenant's environments and which have a local profile.
// Because keys are environment-scoped, switching environments means logging in
// again — so the command prints the exact next step. Design doc §6.
func newEnvCommand(g *Globals, version string) *cobra.Command {
	env := &cobra.Command{Use: "env", Short: "Inspect environments"}

	env.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List environments and which have a local profile",
		RunE: func(c *cobra.Command, _ []string) error {
			rc, _, err := runtimeContext(g)
			if err != nil {
				return err
			}
			cl := client.New(client.Options{
				BaseURL: rc.BaseURL, APIKey: rc.APIKey, Version: version,
				Debug: g.Debug, DebugOut: os.Stderr,
			})
			raw, err := cl.Do(c.Context(), http.MethodGet, "/environments", nil, nil)
			if err != nil {
				return err
			}

			var envs environmentsResponse
			if err := json.Unmarshal(raw, &envs); err != nil {
				return fmt.Errorf("parse environments: %w", err)
			}

			// Profiles cannot be correlated to environments: the API does not
			// report which environment the active key belongs to, so this is a
			// plain listing of what exists in the tenant.
			//
			// output.PadGrid rather than text/tabwriter: tabwriter counts raw
			// bytes, so the styled header below would have inflated its width
			// calculation by ~20 invisible escape bytes per cell and misaligned
			// every column — the same defect already fixed once in the table
			// renderer.
			grid := [][]string{{
				style.Header("ENVIRONMENT"),
				style.Header("TYPE"),
				style.Header("ID"),
			}}
			for _, e := range envs.Environments {
				grid = append(grid, []string{e.Name, e.Type, e.ID})
			}
			for _, line := range output.PadGrid(grid) {
				g.UI.Data("%s", line)
			}
			g.UI.Info("\nYour key is scoped to one of these, but the API does not say which.")
			return nil
		},
	})

	return env
}
