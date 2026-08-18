package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

	if err := VerifyKey(t.Context(), srv.URL, "sk_good", "test"); err != nil {
		t.Fatalf("VerifyKey: %v", err)
	}
}

// A wrong-region key returns 401, identical to an invalid key. The message must
// disambiguate. Design doc §6.
func TestVerifyKey_RejectionMentionsRegion(t *testing.T) {
	srv := envServer(t, "production")
	defer srv.Close()

	err := VerifyKey(t.Context(), srv.URL, "sk_wrong", "test")
	if err == nil {
		t.Fatal("want an error for a rejected key")
	}
	if got := err.Error(); !strings.Contains(got, "region") {
		t.Errorf("error = %q, want it to mention region", got)
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
