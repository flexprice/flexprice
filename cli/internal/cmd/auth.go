package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/config"
	"github.com/flexprice/cli/internal/keyring"
	"github.com/flexprice/cli/internal/spec"
)

// VerifyKey confirms a key works against a region. It deliberately returns no
// identity: nothing reachable by an environment-scoped key reveals which
// environment it belongs to, so there is nothing trustworthy to report.

// environmentsResponse matches dto.ListEnvironmentsResponse.
//
// GET /v1/environments is a real, authenticated route but carries no swaggo
// annotations, so it is absent from the OpenAPI spec and cannot be resolved
// through the registry. It is called by literal path here. Annotating the
// handler upstream is tracked in "Before release".
type environmentsResponse struct {
	Environments []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"environments"`
}

func VerifyKey(ctx context.Context, baseURL, apiKey, version string, debug bool, debugOut io.Writer) error {
	c := client.New(client.Options{
		BaseURL: baseURL, APIKey: apiKey, Version: version,
		Debug: debug, DebugOut: debugOut,
	})

	if _, err := c.Do(ctx, http.MethodGet, "/environments", nil, nil); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
			// A wrong-region key and an invalid key both return 401 with an
			// identical body, so the message has to name the possibility. Wrapped
			// with %w so errors.As in main.go still finds the *client.APIError and
			// exits with exitcode.Auth instead of the generic fallback.
			return fmt.Errorf(
				"this key was rejected by %s.\n"+
					"  Keys are region-specific. If your account is in another region, re-run with --region\n"+
					"  (for example: flexprice login --region in): %w", baseURL, err)
		}
		return err
	}
	return nil
}

// MaskKey renders a key for display: enough to identify it, not enough to use.
func MaskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:8] + "…" + key[len(key)-2:]
}

// readSecret reads a key from the terminal without echoing it, so it never lands
// in shell history or the process table.
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
					region, err = promptRegion(regions)
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

			if err := VerifyKey(ctx, baseURL, apiKey, version, g.Debug, os.Stderr); err != nil {
				return err
			}

			profileName = config.ProfileName(profileName)

			store, warn, err := keyring.Open()
			if err != nil {
				return err
			}
			if warn != "" {
				fmt.Fprintln(os.Stderr, warn)
			}

			path, err := config.DefaultPath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}

			// Rotation: show what is being replaced rather than silently overwriting.
			if _, existed := cfg.Profiles[profileName]; existed {
				if old, err := store.Get(profileName); err == nil {
					fmt.Fprintf(os.Stderr, "Replacing key %s with %s for profile %q\n",
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

			fmt.Fprintf(os.Stderr, "Verified — stored as profile %q in %s\n", profileName, store.Name())
			fmt.Fprintln(os.Stderr,
				"Note: the API does not report which environment a key belongs to, so label your\n"+
					"profiles yourself (--profile-name, --label) and check with: flexprice whoami")
			return nil
		},
	}

	cmd.Flags().StringVar(&profileName, "profile-name", "", "name for the stored profile (default: \"default\")")
	cmd.Flags().StringVar(&label, "label", "", "free-text note shown by whoami, e.g. \"sandbox\"")
	return cmd
}

// promptRegion asks which data region the key belongs to, using an arrow-key
// menu when a real terminal is attached.
//
// The TTY guard is load-bearing and must stay: huh drives a full-screen
// terminal session, so it can only run for a human at a keyboard. Without this
// check every scripted and CI invocation would break. huh itself also fails
// fast rather than hanging when no terminal exists (it opens /dev/tty directly
// and errors immediately) — verified in the implementation spike — so the two
// form independent, agreeing safety nets, but this check is the primary one.
func promptRegion(regions []spec.Region) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no terminal available — pass --region (for example --region us)")
	}

	options := make([]huh.Option[string], len(regions))
	for i, r := range regions {
		options[i] = huh.NewOption(fmt.Sprintf("%-6s  %s", r.Key, r.BaseURL), r.Key)
	}

	var choice string
	sel := huh.NewSelect[string]().
		Title("Data region").
		Options(options...).
		Value(&choice)

	if err := sel.Run(); err != nil {
		return "", fmt.Errorf("region selection cancelled: %w", err)
	}
	return choice, nil
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
			fmt.Fprintf(os.Stderr, "Removed profile %q\n", name)
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

			fmt.Fprintf(os.Stdout, "Profile:      %s\n", name)
			fmt.Fprintf(os.Stdout, "Label:        %s\n", profile.Label)
			fmt.Fprintf(os.Stdout, "Region:       %s\n", profile.Region)
			fmt.Fprintf(os.Stdout, "Base URL:     %s\n", profile.BaseURL)
			fmt.Fprintf(os.Stdout, "Key backend:  %s\n", store.Name())
			if keyErr == nil {
				fmt.Fprintf(os.Stdout, "Key:          %s\n", MaskKey(key))
			} else {
				fmt.Fprintf(os.Stdout, "Key:          (not stored — run flexprice login)\n")
			}
			return nil
		},
	}
}
