// Package integration exercises the CLI against a running Flexprice server.
// It starts nothing — export FLEXPRICE_TEST_BASE_URL/API_KEY, or the whole
// package skips.
package integration

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func requireServer(t *testing.T) (baseURL, apiKey string) {
	t.Helper()
	baseURL = os.Getenv("FLEXPRICE_TEST_BASE_URL")
	apiKey = os.Getenv("FLEXPRICE_TEST_API_KEY")
	if baseURL == "" || apiKey == "" {
		t.Skip("set FLEXPRICE_TEST_BASE_URL and FLEXPRICE_TEST_API_KEY to run integration tests")
	}
	return baseURL, apiKey
}

// run invokes the built binary the way a user would.
func run(t *testing.T, baseURL, apiKey string, args ...string) (string, error) {
	t.Helper()
	full := append([]string{"--base-url", baseURL, "--api-key", apiKey, "--output", "json"}, args...)
	cmd := exec.Command("../bin/flexprice", full...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestIntegration_CustomersListReturnsJSON(t *testing.T) {
	baseURL, apiKey := requireServer(t)

	out, err := run(t, baseURL, apiKey, "customers", "list", "--limit", "1")
	if err != nil {
		t.Fatalf("customers list: %v\n%s", err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("stdout is not a JSON object:\n%s", out)
	}
}

func TestIntegration_UnknownFlagSuggestsAField(t *testing.T) {
	baseURL, apiKey := requireServer(t)

	out, _ := run(t, baseURL, apiKey, "customers", "create", "--externl_id", "x")
	if !strings.Contains(out, "Did you mean") {
		t.Errorf("want a suggestion for a misspelled flag, got:\n%s", out)
	}
}

func TestIntegration_BadKeyExitsWithAuthCode(t *testing.T) {
	baseURL, _ := requireServer(t)

	cmd := exec.Command("../bin/flexprice",
		"--base-url", baseURL, "--api-key", "sk_definitely_invalid", "customers", "list")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("want a failure for an invalid key, got:\n%s", out)
	}
	if code := cmd.ProcessState.ExitCode(); code != 3 {
		t.Errorf("exit code = %d, want 3 (auth)\n%s", code, out)
	}
}
