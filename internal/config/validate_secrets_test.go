package config

import (
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

// TestValidateSecrets covers the fail-fast placeholder-secret check: it must reject baked
// dev/sample secrets for ENABLED features and ignore them for disabled features.
func TestValidateSecrets(t *testing.T) {
	// base is an otherwise-valid non-local config with real secrets. Individual cases mutate
	// one field to assert it is (or isn't) rejected.
	base := func() Configuration {
		c := Configuration{}
		c.Deployment.Mode = types.ModeAPI
		c.Auth.Provider = types.AuthProviderFlexprice
		c.Auth.Secret = "a-real-32-byte-production-secret-value"
		c.Postgres.Password = "a-real-db-password"
		c.ClickHouse.Password = "a-real-ch-password"
		c.Secrets.EncryptionKey = "031f6bbed1156eca651d48652c17a5bce727514cc804f185aca207153b2915abb79c0f1b53945915866dc3b63f37ea73aa86fc062f13e6008249e30819f87483"
		return c
	}

	cases := []struct {
		name       string
		mutate     func(*Configuration)
		wantErr    bool
		wantSubstr string
	}{
		{"all real secrets", func(c *Configuration) {}, false, ""},
		{"placeholder auth.secret", func(c *Configuration) {
			c.Auth.Secret = "dev-only-insecure-secret-prod-sets-FLEXPRICE_AUTH_SECRET"
		}, true, "auth.secret"},
		{"empty auth.secret (flexprice provider)", func(c *Configuration) {
			c.Auth.Secret = ""
		}, true, "auth.secret"},
		{"dev postgres password", func(c *Configuration) {
			c.Postgres.Password = "flexprice123"
		}, true, "postgres.password"},
		{"dev clickhouse password", func(c *Configuration) {
			c.ClickHouse.Password = "flexprice123"
		}, true, "clickhouse.password"},
		{"svix placeholder but DISABLED — ignored", func(c *Configuration) {
			c.Webhook.Svix.Enabled = false
			c.Webhook.Svix.AuthToken = "svix_auth_token"
		}, false, ""},
		{"svix placeholder and ENABLED — rejected", func(c *Configuration) {
			c.Webhook.Svix.Enabled = true
			c.Webhook.Svix.AuthToken = "svix_auth_token"
		}, true, "svix"},
		{"custom encryption_key is NOT flagged", func(c *Configuration) {}, false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mutate(&c)
			err := c.validateSecrets()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tc.wantErr && tc.wantSubstr != "" && !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

func TestValidateEncryptionKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"default key", "super-secret-encryption-key", true},
		{"empty key", "", true},
		{"whitespace key", "  ", true},
		{"custom key", "a-real-production-encryption-key", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("FLEXPRICE_ALLOW_INSECURE_DEFAULTS", "")
			c := Configuration{}
			c.Deployment.Mode = types.ModeAPI
			c.Secrets.EncryptionKey = tt.key
			if err := c.validateEncryptionKey(); (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCriticalSecrets(t *testing.T) {
	base := func() Configuration {
		c := Configuration{}
		c.Deployment.Mode = types.ModeAPI
		c.Auth.Provider = types.AuthProviderFlexprice
		c.Auth.Secret = "a-real-32-byte-production-secret-value"
		c.Auth.APIKey.Keys = map[string]APIKeyDetails{"custom-hash": {IsActive: true}}
		return c
	}

	cases := []struct {
		name    string
		mutate  func(*Configuration)
		wantErr string
	}{
		{"custom values", func(c *Configuration) {}, ""},
		{"super_secret", func(c *Configuration) { c.Auth.Secret = "super_secret" }, "auth.secret"},
		{"empty secret", func(c *Configuration) { c.Auth.Secret = " " }, "auth.secret"},
		{"supabase with super_secret", func(c *Configuration) {
			c.Auth.Provider = types.AuthProviderSupabase
			c.Auth.Secret = "super_secret"
		}, "auth.secret"},
		{"dev api key present", func(c *Configuration) {
			c.Auth.APIKey.Keys[devAPIKeyHash] = APIKeyDetails{IsActive: true}
		}, "dev key"},
		{"nil api keys", func(c *Configuration) { c.Auth.APIKey.Keys = nil }, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FLEXPRICE_ALLOW_INSECURE_DEFAULTS", "")
			c := base()
			tc.mutate(&c)
			err := c.validateCriticalSecrets()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateEncryptionKeyInsecureOptOut(t *testing.T) {
	c := Configuration{}
	c.Deployment.Mode = types.ModeAPI
	c.Secrets.EncryptionKey = "super-secret-encryption-key"

	t.Setenv("FLEXPRICE_ALLOW_INSECURE_DEFAULTS", "")
	if err := c.validateEncryptionKey(); err == nil {
		t.Fatal("default key without opt-out must fail")
	}
	t.Setenv("FLEXPRICE_ALLOW_INSECURE_DEFAULTS", "true")
	if err := c.validateEncryptionKey(); err != nil {
		t.Fatalf("opt-out must allow default key: %v", err)
	}
}

func TestNewValidatedConfigEncryptionKey(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		key     string
		wantErr bool
	}{
		{"local default ok", string(types.ModeLocal), "super-secret-encryption-key", false},
		{"api default fails", string(types.ModeAPI), "super-secret-encryption-key", true},
		{"api blank fails", string(types.ModeAPI), "   ", true},
		{"api custom ok", string(types.ModeAPI), "a-real-production-encryption-key", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("FLEXPRICE_ALLOW_INSECURE_DEFAULTS", "")
			t.Setenv("FLEXPRICE_DEPLOYMENT_MODE", tt.mode)
			t.Setenv("FLEXPRICE_AUTH_SECRET", "a-real-32-byte-production-secret-value")
			t.Setenv("FLEXPRICE_AUTH_API_KEY_KEYS", "{}")
			t.Setenv("FLEXPRICE_SECRETS_ENCRYPTION_KEY", tt.key)
			_, err := NewValidatedConfig()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestNewValidatedConfigLocalAllowsDefaults(t *testing.T) {
	t.Setenv("FLEXPRICE_DEPLOYMENT_MODE", "local")
	if _, err := NewValidatedConfig(); err != nil {
		t.Fatalf("local mode with defaults must boot: %v", err)
	}
}

func TestValidateCriticalSecretsInsecureOptOut(t *testing.T) {
	c := Configuration{}
	c.Deployment.Mode = types.ModeAPI
	c.Auth.Secret = "super_secret"
	c.Auth.APIKey.Keys = map[string]APIKeyDetails{devAPIKeyHash: {IsActive: true}}

	t.Setenv("FLEXPRICE_ALLOW_INSECURE_DEFAULTS", "")
	if err := c.validateCriticalSecrets(); err == nil {
		t.Fatal("defaults without opt-out must fail")
	}
	t.Setenv("FLEXPRICE_ALLOW_INSECURE_DEFAULTS", "true")
	if err := c.validateCriticalSecrets(); err != nil {
		t.Fatalf("opt-out must allow defaults: %v", err)
	}
}

func TestNewValidatedConfigNonLocalRejectsDefaults(t *testing.T) {
	t.Setenv("FLEXPRICE_ALLOW_INSECURE_DEFAULTS", "")
	t.Setenv("FLEXPRICE_DEPLOYMENT_MODE", "api")
	t.Setenv("FLEXPRICE_AUTH_SECRET", "")
	if _, err := NewValidatedConfig(); err == nil {
		t.Fatal("non-local mode with default secrets must fail")
	}
}
