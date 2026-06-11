package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const defaultAPIHost = "api.cloud.flexprice.io/v1"

// Target is a single Flexprice region/environment: base URL + API key.
type Target struct {
	// Name is a human-friendly label (e.g. "aws-staging"). Defaults to host.
	Name string `json:"name"`
	// APIHost is the base URL or host, with or without scheme and /v1 suffix.
	APIHost string `json:"api_host"`
	// APIKey is the secret API key for this target.
	APIKey string `json:"api_key"`
}

// host returns the scheme-stripped host (and path), applying the default.
func (t Target) host() string {
	h := t.APIHost
	if h == "" {
		h = defaultAPIHost
	}
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	return h
}

// serverURL builds the full base URL. localhost/127.0.0.1 use http.
func (t Target) serverURL() string {
	h := t.host()
	scheme := "https://"
	if strings.HasPrefix(h, "localhost") || strings.HasPrefix(h, "127.0.0.1") {
		scheme = "http://"
	}
	return scheme + h
}

// label returns the display name.
func (t Target) label() string {
	if t.Name != "" {
		return t.Name
	}
	return t.host()
}

// maskedKey redacts the API key for logs.
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

// loadTargets resolves the targets to run, in priority order:
//
//  1. FLEXPRICE_TARGETS_FILE — path to a JSON array of targets.
//  2. FLEXPRICE_TARGETS — inline JSON array of targets.
//  3. FLEXPRICE_API_KEY / FLEXPRICE_API_HOST — single-pair flow.
//
// JSON array shape:
//
//	[{"name": "aws-staging", "api_host": "https://api-dev.cloud.flexprice.io/v1", "api_key": "sk_..."}]
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

	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("no targets configured: set FLEXPRICE_TARGETS_FILE, FLEXPRICE_TARGETS, or FLEXPRICE_API_KEY")
	}
	return []Target{{
		APIHost: os.Getenv("FLEXPRICE_API_HOST"),
		APIKey:  apiKey,
	}}, nil
}

func parseTargets(data []byte, source string) ([]Target, error) {
	var targets []Target
	if err := json.Unmarshal(data, &targets); err != nil {
		return nil, fmt.Errorf("parse %s as JSON array of targets: %w", source, err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("%s contains no targets", source)
	}
	for i, t := range targets {
		if t.APIKey == "" {
			return nil, fmt.Errorf("%s: target #%d (%s) is missing api_key", source, i+1, t.label())
		}
	}
	return targets, nil
}
