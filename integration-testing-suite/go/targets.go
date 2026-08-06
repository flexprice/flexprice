package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Target represents a single Flexprice region/environment to run the sanity
// suite against: a base URL + API key pair.
type Target struct {
	// Name is a human-friendly label for the target (e.g. "aws-staging").
	// Optional; defaults to the host if empty.
	Name string `json:"name"`
	// APIHost is the base URL or host, with or without scheme and /v1 suffix
	// (e.g. "https://api-dev.cloud.flexprice.io/v1").
	APIHost string `json:"api_host"`
	// APIKey is the secret API key for this target.
	APIKey string `json:"api_key"`
	// Insecure skips TLS certificate verification for this target. Use only
	// for environments whose cert does not cover the host (e.g. preprod).
	// Can also be forced for all targets via FLEXPRICE_INSECURE_SKIP_VERIFY.
	Insecure bool `json:"insecure"`
	// Enabled controls whether this target is included in the run.
	// Optional — omitting the field (or setting it to true) keeps the target active.
	// Set to false to skip a target without removing it from the file.
	Enabled *bool `json:"enabled"`
}

// isEnabled reports whether this target should be included in a run.
// Defaults to true when the field is omitted from JSON.
func (t Target) isEnabled() bool {
	return t.Enabled == nil || *t.Enabled
}

// skipTLSVerify reports whether TLS verification should be skipped for this
// target, honouring both the per-target flag and the global env override.
func (t Target) skipTLSVerify() bool {
	return t.Insecure || envTruthy("FLEXPRICE_INSECURE_SKIP_VERIFY")
}

// envTruthy reports whether an env var is set to a truthy value.
func envTruthy(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "y", "on":
		return true
	}
	return false
}

// host returns the scheme-stripped host (and path) for this target, applying
// the default host when none is provided.
func (t Target) host() string {
	h := t.APIHost
	if h == "" {
		h = defaultAPIHost
	}
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	return h
}

// serverURL builds the full base URL including scheme. localhost/127.0.0.1
// use http, everything else uses https.
func (t Target) serverURL() string {
	h := t.host()
	scheme := "https://"
	if strings.HasPrefix(h, "localhost") || strings.HasPrefix(h, "127.0.0.1") {
		scheme = "http://"
	}
	return scheme + h
}

// label returns the display name for this target.
func (t Target) label() string {
	if t.Name != "" {
		return t.Name
	}
	return t.host()
}

// maskedKey returns the API key with the middle redacted for display.
// Always masks, regardless of length, to avoid leaking secrets in logs.
func (t Target) maskedKey() string {
	k := t.APIKey
	if len(k) <= 4 {
		return "***"
	}
	if len(k) <= 12 {
		return k[:2] + "..." + k[len(k)-2:]
	}
	return k[:8] + "..." + k[len(k)-4:]
}

// loadTargets resolves the list of targets to run, in priority order:
//
//  1. FLEXPRICE_TARGETS_FILE — path to a JSON file containing an array of
//     targets. Best for multiple regions, keeps secrets out of the shell.
//  2. FLEXPRICE_TARGETS — inline JSON array of targets.
//  3. FLEXPRICE_API_KEY / FLEXPRICE_API_HOST — the original single-pair flow.
//
// JSON array shape (both 1 and 2):
//
//	[
//	  {"name": "aws-staging",  "api_host": "https://api-dev.cloud.flexprice.io/v1", "api_key": "sk_..."},
//	  {"name": "tirdad-prod",  "api_host": "https://api.tirdad.ai/v1",              "api_key": "sk_..."}
//	]
func loadTargets() ([]Target, error) {
	if path := os.Getenv("FLEXPRICE_TARGETS_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read FLEXPRICE_TARGETS_FILE %q: %w", path, err)
		}
		return parseTargets(data, fmt.Sprintf("FLEXPRICE_TARGETS_FILE (%s)", path))
	}

	if raw := os.Getenv("FLEXPRICE_TARGETS"); strings.TrimSpace(raw) != "" {
		return parseTargets([]byte(raw), "FLEXPRICE_TARGETS")
	}

	// Fall back to the original single-pair env vars.
	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("no targets configured: set FLEXPRICE_TARGETS_FILE, FLEXPRICE_TARGETS, or FLEXPRICE_API_KEY")
	}
	return []Target{{
		APIHost: os.Getenv("FLEXPRICE_API_HOST"),
		APIKey:  apiKey,
	}}, nil
}

// parseTargets decodes and validates a JSON array of targets.
func parseTargets(data []byte, source string) ([]Target, error) {
	var targets []Target
	if err := json.Unmarshal(data, &targets); err != nil {
		return nil, fmt.Errorf("parse %s as JSON array of targets: %w", source, err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("%s contains no targets", source)
	}
	enabled := targets[:0]
	for i, t := range targets {
		if t.APIKey == "" {
			return nil, fmt.Errorf("%s: target #%d (%s) is missing api_key", source, i+1, t.label())
		}
		if !t.isEnabled() {
			fmt.Printf("  (skipping disabled target #%d: %s)\n", i+1, t.label())
			continue
		}
		enabled = append(enabled, t)
	}
	if len(enabled) == 0 {
		return nil, fmt.Errorf("%s: all targets are disabled", source)
	}
	return enabled, nil
}
