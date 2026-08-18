package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/config"
	"github.com/flexprice/cli/internal/keyring"
	"github.com/flexprice/cli/internal/spec"
	"github.com/flexprice/cli/internal/ui"
)

// GET /v1/environments is a real authenticated route but carries no swaggo
// annotations, so it is absent from the spec and called by literal path here.
type environmentsResponse struct {
	Environments []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"environments"`
}

// Returns no identity deliberately: nothing reachable by an environment-scoped
// key reveals which environment it belongs to.
func VerifyKey(ctx context.Context, baseURL, apiKey, version string, debug bool, debugOut io.Writer) error {
	c := client.New(client.Options{
		BaseURL: baseURL, APIKey: apiKey, Version: version,
		Debug: debug, DebugOut: debugOut,
	})

	if _, err := c.Do(ctx, http.MethodGet, "/environments", nil, nil); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
			// Identical 401 for wrong-region and invalid keys, so the message
			// names the possibility. %w keeps errors.As finding the APIError.
			return fmt.Errorf(
				"this key was rejected by %s.\n"+
					"  Keys are region-specific. If your account is in another region, re-run with --region\n"+
					"  (for example: flexprice login --region in): %w", baseURL, err)
		}
		return err
	}
	return nil
}

// Enough to identify a key, not enough to use it.
func MaskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:8] + "…" + key[len(key)-2:]
}

// Reads without echoing, so the key never lands in shell history or the
// process table.
func readSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no terminal available — pass --api-key, or set FLEXPRICE_API_KEY")
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read key: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func newLoginCommand(g *Globals, version string) *cobra.Command {
	var profileName, label string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store credentials for a region and environment",
		Long: "Verifies your API key, resolves the tenant and environment it is scoped to,\n" +
			"and stores it in your OS keychain.\n\n" +
			"An API key belongs to exactly one environment, so use one profile per environment.",
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()

			doc, err := spec.Load()
			if err != nil {
				return err
			}
			regions := spec.Regions(doc)

			baseURL := g.BaseURL
			if baseURL == "" {
				region := g.Region
				if region == "" {
					region, err = promptRegion(g, regions)
					if err != nil {
						return err
					}
				}
				for _, r := range regions {
					if r.Key == region {
						baseURL = r.BaseURL
					}
				}
				if baseURL == "" {
					return fmt.Errorf("unknown region %q", region)
				}
			}

			apiKey := g.APIKey
			if apiKey == "" {
				apiKey, err = readSecret("API key: ")
				if err != nil {
					return err
				}
			}
			if apiKey == "" {
				return fmt.Errorf("no API key provided")
			}

			sp := g.UI.Spinner("Verifying your key…")
			verifyErr := VerifyKey(ctx, baseURL, apiKey, version, g.Debug, os.Stderr)
			sp.Stop()
			if verifyErr != nil {
				return verifyErr
			}

			profileName = config.ProfileName(profileName)

			store, warn, err := keyring.Open()
			if err != nil {
				return err
			}
			if warn != "" {
				g.UI.Info("%s", warn)
			}

			path, err := config.DefaultPath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}

			// Show what is being replaced rather than silently overwriting.
			if _, existed := cfg.Profiles[profileName]; existed {
				if old, err := store.Get(profileName); err == nil {
					g.UI.Info("Replacing key %s with %s for profile %q",
						MaskKey(old), MaskKey(apiKey), profileName)
				}
			}

			if err := store.Set(profileName, apiKey); err != nil {
				return fmt.Errorf("store key: %w", err)
			}

			cfg.Profiles[profileName] = config.Profile{
				Region:  g.Region,
				BaseURL: baseURL,
				Label:   label,
				KeyRef:  "keyring:" + profileName,
			}
			if cfg.DefaultProfile == "" {
				cfg.DefaultProfile = profileName
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}

			g.UI.Success("Verified — stored as profile %q in %s", profileName, store.Name())
			g.UI.Info("Note: the API does not report which environment a key belongs to, so label your\n" +
				"profiles yourself (--profile-name, --label) and check with: flexprice whoami")
			return nil
		},
	}

	cmd.Flags().StringVar(&profileName, "profile-name", "", "name for the stored profile (default: \"default\")")
	cmd.Flags().StringVar(&label, "label", "", "free-text note shown by whoami, e.g. \"sandbox\"")
	return cmd
}

// The TTY guard lives in ui.SelectWithHint, which refuses with an actionable
// message rather than hanging.
func promptRegion(g *Globals, regions []spec.Region) (string, error) {
	opts := make([]ui.Option, len(regions))
	for i, r := range regions {
		opts[i] = ui.Option{
			Label: fmt.Sprintf("%-6s  %s", r.Key, r.BaseURL),
			Value: r.Key,
		}
	}
	return g.UI.SelectWithHint("Data region", "--region", opts)
}

func newLogoutCommand(g *Globals) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove a stored profile and its key",
		RunE: func(c *cobra.Command, _ []string) error {
			path, err := config.DefaultPath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			name, _, err := cfg.Resolve(g.Profile)
			if err != nil {
				return err
			}

			store, _, err := keyring.Open()
			if err != nil {
				return err
			}
			if err := store.Delete(name); err != nil {
				return err
			}

			delete(cfg.Profiles, name)
			if cfg.DefaultProfile == name {
				cfg.DefaultProfile = ""
				for other := range cfg.Profiles {
					cfg.DefaultProfile = other
					break
				}
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			g.UI.Success("Removed profile %q", name)
			return nil
		},
	}
}

func newWhoamiCommand(g *Globals) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the active profile, environment and key backend",
		RunE: func(c *cobra.Command, _ []string) error {
			path, err := config.DefaultPath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			name, profile, err := cfg.Resolve(g.Profile)
			if err != nil {
				return err
			}

			store, _, err := keyring.Open()
			if err != nil {
				return err
			}
			key, keyErr := store.Get(name)

			// Stays on stdout via ui.Data: whoami's output is a result people
			// parse, not commentary.
			g.UI.Data("Profile:      %s", name)
			g.UI.Data("Label:        %s", profile.Label)
			g.UI.Data("Region:       %s", profile.Region)
			g.UI.Data("Base URL:     %s", profile.BaseURL)
			g.UI.Data("Key backend:  %s", store.Name())
			if keyErr == nil {
				g.UI.Data("Key:          %s", MaskKey(key))
			} else {
				g.UI.Data("Key:          (not stored — run flexprice login)")
			}
			return nil
		},
	}
}
