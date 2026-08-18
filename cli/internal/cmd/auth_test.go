package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/exitcode"
	"github.com/flexprice/cli/internal/spec"
	"github.com/flexprice/cli/internal/ui"
)

func envServer(t *testing.T, envType string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk_good" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"environments": []map[string]any{
				{"id": "env_1", "name": "Production", "type": envType},
			},
		})
	}))
}

func TestVerifyKey_AcceptsAWorkingKey(t *testing.T) {
	srv := envServer(t, "development")
	defer srv.Close()

	if err := VerifyKey(t.Context(), srv.URL, "sk_good", "test", false, nil); err != nil {
		t.Fatalf("VerifyKey: %v", err)
	}
}

// A wrong-region key returns 401, identical to an invalid key. The message must
// disambiguate. Design doc §6.
func TestVerifyKey_RejectionMentionsRegion(t *testing.T) {
	srv := envServer(t, "production")
	defer srv.Close()

	err := VerifyKey(t.Context(), srv.URL, "sk_wrong", "test", false, nil)
	if err == nil {
		t.Fatal("want an error for a rejected key")
	}
	if got := err.Error(); !strings.Contains(got, "region") {
		t.Errorf("error = %q, want it to mention region", got)
	}
}

// main.go dispatches exit codes via errors.As(err, &apiErr); a rejection must
// stay a *client.APIError under wrapping so a bad key exits with exitcode.Auth
// (3), not the generic fallback (1).
func TestVerifyKey_RejectionPreservesAPIErrorType(t *testing.T) {
	srv := envServer(t, "production")
	defer srv.Close()

	err := VerifyKey(t.Context(), srv.URL, "sk_wrong", "test", false, nil)
	if err == nil {
		t.Fatal("want an error for a rejected key")
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As(err, &apiErr) = false, want the *client.APIError to survive wrapping; err = %v", err)
	}
	if got := apiErr.ExitCode(); got != exitcode.Auth {
		t.Errorf("ExitCode() = %d, want exitcode.Auth (%d)", got, exitcode.Auth)
	}
}

// VerifyKey's own client.New call must thread through debug settings like every
// other call site in this package, or `flexprice login --debug` silently
// produces no output for the verification request.
func TestVerifyKey_DebugWritesRequestOutput(t *testing.T) {
	srv := envServer(t, "development")
	defer srv.Close()

	var buf bytes.Buffer
	if err := VerifyKey(t.Context(), srv.URL, "sk_good", "test", true, &buf); err != nil {
		t.Fatalf("VerifyKey: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("debug=true produced no debug output")
	}

	buf.Reset()
	if err := VerifyKey(t.Context(), srv.URL, "sk_good", "test", false, &buf); err != nil {
		t.Fatalf("VerifyKey: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("debug=false wrote %d bytes, want none", buf.Len())
	}
}

func TestMaskKey_ShowsOnlyAPrefix(t *testing.T) {
	got := MaskKey("sk_live_abcdefghijklmnop")
	if got == "sk_live_abcdefghijklmnop" {
		t.Fatal("key was not masked")
	}
	if len(got) > 20 {
		t.Errorf("mask = %q, too revealing", got)
	}
}

// TestMaskKey_ShortKeyDoesNotPanic guards the login command's rotation display,
// which calls MaskKey on whatever was previously stored — including a key
// shorter than the 8-byte prefix MaskKey normally takes.
func TestMaskKey_ShortKeyDoesNotPanic(t *testing.T) {
	for _, key := range []string{"", "a", "1234567", "12345678"} {
		got := MaskKey(key)
		if got == "" {
			t.Errorf("MaskKey(%q) = %q, want a non-empty placeholder", key, got)
		}
	}
}

// promptRegion's non-TTY fallback must be preserved exactly: huh.Select must
// never be invoked when stdin is not a real terminal, or every existing test
// and CI/script invocation of this CLI breaks. This is the single most
// important test in the interactive-UI work.
func TestPromptRegion_NoTTYFallsBackToExactPriorBehavior(t *testing.T) {
	regions := []spec.Region{
		{Key: "us", BaseURL: "https://us.api.flexprice.io/v1"},
		{Key: "in", BaseURL: "https://api.cloud.flexprice.io/v1"},
	}
	// The TTY state is now injected rather than probed, so this no longer
	// depends on go test's stdin happening not to be a terminal — it asserts
	// the behaviour directly.
	g := &Globals{UI: ui.New(ui.Options{StderrTTY: true, StdinTTY: false, Term: "dumb"})}
	_, err := promptRegion(g, regions)
	if err == nil {
		t.Fatal("want an error when stdin is not a terminal")
	}
	if !strings.Contains(err.Error(), "--region") {
		t.Errorf("error = %q, want it to name --region as the alternative", err.Error())
	}
}
