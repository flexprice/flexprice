package config

import (
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

// TestValidateSecrets covers the fail-fast placeholder-secret check: it must reject baked
// dev/sample secrets for ENABLED features, ignore them for disabled features, and never flag
// secrets.encryption_key (deliberately excluded pending key reconciliation).
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
		// baked encryption key present — must NOT be flagged.
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
		{"baked encryption_key is NOT flagged", func(c *Configuration) {
			// encryption key left at the baked hex from base(); everything else real.
		}, false, ""},
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
