package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// routes builds a test server dispatching on method+path.
func routes(t *testing.T, h map[string]func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, fn := range h {
		mux.HandleFunc(pattern, fn)
	}
	return httptest.NewServer(mux)
}

func okSignup(w http.ResponseWriter, r *http.Request) {
	// Signup returns 200, NOT 201.
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"token":"jwt-abc","user_id":"user_1","tenant_id":"tenant_1"}`))
}

func okEnvs(w http.ResponseWriter, r *http.Request) {
	if got := r.Header.Get("Authorization"); got != "Bearer jwt-abc" {
		http.Error(w, "missing bearer", http.StatusUnauthorized)
		return
	}
	// The array key is "environments", NOT "items".
	_, _ = w.Write([]byte(`{"environments":[{"id":"env_1","name":"Sandbox","type":"development"}],"total":1,"offset":0,"limit":10}`))
}

func okMint(w http.ResponseWriter, r *http.Request) {
	if got := r.Header.Get("X-Environment-ID"); got != "env_1" {
		http.Error(w, "missing environment header", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"secret":{"id":"secret_1"},"api_key":"sk_minted"}`))
}

func TestRun_HappyPath(t *testing.T) {
	srv := routes(t, map[string]func(http.ResponseWriter, *http.Request){
		"/auth/signup":      okSignup,
		"/environments":     okEnvs,
		"/secrets/api/keys": okMint,
	})
	defer srv.Close()

	creds, err := Run(context.Background(), srv.Client(), srv.URL, "a@b.c", "pw12345678", "e2eprobe-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.APIKey() != "sk_minted" {
		t.Errorf("api key = %q, want sk_minted", creds.APIKey())
	}
	if creds.TenantID() != "tenant_1" {
		t.Errorf("tenant = %q, want tenant_1", creds.TenantID())
	}
	if creds.EnvironmentID() != "env_1" {
		t.Errorf("env = %q, want env_1", creds.EnvironmentID())
	}
	if creds.SecretID() != "secret_1" {
		t.Errorf("secret id = %q, want secret_1", creds.SecretID())
	}
}

func TestRun_DuplicateSignupFallsBackToLogin(t *testing.T) {
	var loginCalled bool
	srv := routes(t, map[string]func(http.ResponseWriter, *http.Request){
		"/auth/signup": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"already_exists","message":"user already exists"}`))
		},
		"/auth/login": func(w http.ResponseWriter, r *http.Request) {
			loginCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token":"jwt-abc","user_id":"user_1","tenant_id":"tenant_1"}`))
		},
		"/environments":     okEnvs,
		"/secrets/api/keys": okMint,
	})
	defer srv.Close()

	creds, err := Run(context.Background(), srv.Client(), srv.URL, "a@b.c", "pw12345678", "e2eprobe-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !loginCalled {
		t.Fatal("expected the 409 to fall back to login")
	}
	if creds.APIKey() != "sk_minted" {
		t.Errorf("api key = %q, want sk_minted", creds.APIKey())
	}
}

func TestRun_ProviderRejectionIsExplicit(t *testing.T) {
	// A Supabase-backed deployment rejects signup with 403 (or 500).
	for _, code := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		srv := routes(t, map[string]func(http.ResponseWriter, *http.Request){
			"/auth/signup": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				_, _ = w.Write([]byte(`{"code":"permission_denied","message":"Failed to sign up with authentication provider"}`))
			},
		})
		_, err := Run(context.Background(), srv.Client(), srv.URL, "a@b.c", "pw12345678", "e2eprobe-test")
		srv.Close()

		if err == nil {
			t.Fatalf("status %d: expected an error", code)
		}
		if !strings.Contains(err.Error(), "auth provider") {
			t.Errorf("status %d: error should name the auth provider, got: %v", code, err)
		}
	}
}

func TestRun_LoginRejectedAfterConflict(t *testing.T) {
	srv := routes(t, map[string]func(http.ResponseWriter, *http.Request){
		"/auth/signup": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"already_exists"}`))
		},
		"/auth/login": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"permission_denied","message":"Invalid email or password"}`))
		},
	})
	defer srv.Close()

	_, err := Run(context.Background(), srv.Client(), srv.URL, "a@b.c", "pw12345678", "e2eprobe-test")
	if err == nil {
		t.Fatal("expected an error when login is refused")
	}
	if !strings.Contains(err.Error(), "login") {
		t.Errorf("error should name the login step, got: %v", err)
	}
}

func TestRun_NoEnvironmentsIsFatal(t *testing.T) {
	srv := routes(t, map[string]func(http.ResponseWriter, *http.Request){
		"/auth/signup": okSignup,
		"/environments": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"environments":[],"total":0,"offset":0,"limit":10}`))
		},
	})
	defer srv.Close()

	if _, err := Run(context.Background(), srv.Client(), srv.URL, "a@b.c", "pw12345678", "e2eprobe-test"); err == nil {
		t.Fatal("expected an error when the tenant has no environments")
	}
}

// Guards the decode drift that live verification caught: the server keys the
// array "environments", so a decoder expecting "items" silently sees nothing.
func TestRun_RejectsItemsKeyedEnvelope(t *testing.T) {
	srv := routes(t, map[string]func(http.ResponseWriter, *http.Request){
		"/auth/signup": okSignup,
		"/environments": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"items":[{"id":"env_1"}],"total":1}`))
		},
	})
	defer srv.Close()

	if _, err := Run(context.Background(), srv.Client(), srv.URL, "a@b.c", "pw12345678", "e2eprobe-test"); err == nil {
		t.Fatal("an items-keyed envelope must not be decoded as environments")
	}
}
