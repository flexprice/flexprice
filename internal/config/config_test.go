package config

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

// TestSAMLConfigValidate covers the boot-time gate. The alternative to failing
// here is worse than a crash: an empty or relative base URL produces SP metadata
// whose endpoints an identity provider cannot call, and the failure then
// surfaces much later as an audience mismatch on every assertion.
func TestSAMLConfigValidate(t *testing.T) {
	valid := SAMLConfig{
		Enabled:      true,
		BaseURL:      "https://billing.example.com",
		DashboardURL: "https://app.example.com/auth/callback",
	}

	if err := valid.validate(); err != nil {
		t.Fatalf("a complete https configuration must pass: %v", err)
	}

	// Nothing is enforced while the feature is off, so a deployment that does
	// not offer SSO cannot be taken down by any of this.
	if err := (SAMLConfig{}).validate(); err != nil {
		t.Errorf("a disabled configuration must not be validated: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*SAMLConfig)
		want   string
	}{
		{"base url empty", func(c *SAMLConfig) { c.BaseURL = "" }, "base_url is required"},
		{"base url relative", func(c *SAMLConfig) { c.BaseURL = "/v1" }, "absolute URL"},
		{"base url plaintext", func(c *SAMLConfig) { c.BaseURL = "http://billing.example.com" }, "https"},
		{"dashboard url empty", func(c *SAMLConfig) { c.DashboardURL = "" }, "dashboard_url is required"},
		{"dashboard url relative", func(c *SAMLConfig) { c.DashboardURL = "/auth/callback" }, "absolute URL"},
		{"dashboard url plaintext", func(c *SAMLConfig) { c.DashboardURL = "http://app.example.com" }, "https"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.mutate(&cfg)
			err := cfg.validate()
			if err == nil {
				t.Fatalf("expected rejection mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}

	// Loopback keeps plain http: it never leaves the machine, and it is how this
	// is developed against a local identity provider.
	for _, host := range []string{"http://localhost:8080", "http://127.0.0.1:8080"} {
		cfg := valid
		cfg.BaseURL = host
		cfg.DashboardURL = host + "/auth/callback"
		if err := cfg.validate(); err != nil {
			t.Errorf("loopback %s must be allowed for local development: %v", host, err)
		}
	}
}

// TestValidateSAMLDependencies pins the Redis requirement. SAML keeps its
// outstanding login requests there so a login begun on one replica can be
// completed on another; without it a multi-replica deployment fails most logins
// at random, which looks like an identity provider fault.
func TestValidateSAMLDependencies(t *testing.T) {
	withRedis := Configuration{}
	withRedis.Auth.SAML.Enabled = true
	withRedis.Auth.Secret = "a-real-signing-secret-at-least-32b!"
	withRedis.Cache.Enabled = true
	withRedis.Cache.Redis.Enabled = true

	if err := withRedis.validateSAMLDependencies(); err != nil {
		t.Fatalf("SAML with Redis available must start: %v", err)
	}

	for _, tc := range []struct {
		name         string
		cache, redis bool
	}{
		{"cache disabled entirely", false, true},
		{"redis cache disabled", true, false},
		{"neither", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := withRedis
			cfg.Cache.Enabled = tc.cache
			cfg.Cache.Redis.Enabled = tc.redis
			if err := cfg.validateSAMLDependencies(); err == nil {
				t.Error("SAML must not start without Redis")
			}
		})
	}

	// A deployment that does not offer SAML is unaffected.
	off := Configuration{}
	if err := off.validateSAMLDependencies(); err != nil {
		t.Errorf("a non-SAML deployment must not require Redis: %v", err)
	}
}

// TestValidateSAMLDependenciesRequiresSigningSecret pins the signing secret as a
// hard requirement whenever SSO is on.
//
// auth.secret is the HMAC key for the SSO token. An empty key still produces a
// verifiable signature, so a deployment that boots without one accepts a token
// anybody can mint: the forger names any user in any tenant and the middleware
// then loads that user and grants their roles. validateSecrets does not cover
// this — it is warn-only, and checks auth.secret only under the Flexprice
// provider, so a Supabase deployment with SSO enabled and no secret started
// silently.
//
// The check is scoped to SAML being enabled, so it cannot take down a
// deployment that does not offer SSO — the same reasoning that keeps the rest
// of the secret validation warn-only.
func TestValidateSAMLDependenciesRequiresSigningSecret(t *testing.T) {
	base := Configuration{}
	base.Auth.SAML.Enabled = true
	base.Cache.Enabled = true
	base.Cache.Redis.Enabled = true

	for _, secret := range []string{"", "   ", "\t"} {
		cfg := base
		cfg.Auth.Secret = secret
		if err := cfg.validateSAMLDependencies(); err == nil {
			t.Errorf("SAML must not start with a blank auth.secret (%q)", secret)
		}
	}

	cfg := base
	cfg.Auth.Secret = "a-real-signing-secret-at-least-32b!"
	if err := cfg.validateSAMLDependencies(); err != nil {
		t.Errorf("SAML with a signing secret must start: %v", err)
	}

	// A deployment that does not offer SSO is unaffected, even with no secret.
	off := Configuration{}
	if err := off.validateSAMLDependencies(); err != nil {
		t.Errorf("a non-SAML deployment must not require a signing secret: %v", err)
	}
}

func TestReportsConfig_Enabled(t *testing.T) {
	assert.False(t, ReportsConfig{}.Enabled())
	assert.False(t, ReportsConfig{Database: "d"}.Enabled())
	assert.False(t, ReportsConfig{Username: "u"}.Enabled())
	assert.True(t, ReportsConfig{Database: "d", Username: "u"}.Enabled())
}

func TestBillingConfig_IsSelfBillingEnabled(t *testing.T) {
	full := BillingConfig{
		Enabled:      true,
		Region:       "us",
		APIHost:      "https://api.flexprice.io/v1",
		APIKey:       "sk",
		PlanID:       "plan_1",
		DashboardURL: "https://us.app.flexprice.io",
	}
	with := func(mut func(*BillingConfig)) BillingConfig {
		c := full
		mut(&c)
		return c
	}

	tests := []struct {
		name string
		cfg  BillingConfig
		want bool
	}{
		{"complete", full, true},
		{"disabled", with(func(c *BillingConfig) { c.Enabled = false }), false},
		{"no host", with(func(c *BillingConfig) { c.APIHost = "" }), false},
		{"no key", with(func(c *BillingConfig) { c.APIKey = "" }), false},
		{"no region", with(func(c *BillingConfig) { c.Region = "" }), false},
		{"no plan", with(func(c *BillingConfig) { c.PlanID = "" }), false},
		{"no dashboard", with(func(c *BillingConfig) { c.DashboardURL = "" }), false},
		{"plain http host", with(func(c *BillingConfig) { c.APIHost = "http://api.flexprice.io/v1" }), false},
		{"relative host", with(func(c *BillingConfig) { c.APIHost = "api.flexprice.io/v1" }), false},
		{"http localhost", with(func(c *BillingConfig) { c.APIHost = "http://localhost:8080/v1" }), true},
		{"http 127.0.0.1", with(func(c *BillingConfig) { c.APIHost = "http://127.0.0.1:8080/v1" }), true},
		{"http ::1", with(func(c *BillingConfig) { c.APIHost = "http://[::1]:8080/v1" }), true},
		{"explicit provider", with(func(c *BillingConfig) { c.PaymentProvider = types.CheckoutPaymentProviderRazorpay }), true},
		{"unknown provider", with(func(c *BillingConfig) { c.PaymentProvider = "paypal" }), false},
		{"legacy only", BillingConfig{TenantID: "t_billing", EnvironmentID: "env"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.IsSelfBillingEnabled())
		})
	}
}

func TestBillingConfig_CheckoutProvider(t *testing.T) {
	assert.Equal(t, types.CheckoutPaymentProviderStripe, BillingConfig{}.CheckoutProvider())
	assert.Equal(t, types.CheckoutPaymentProviderChargebee, BillingConfig{PaymentProvider: types.CheckoutPaymentProviderChargebee}.CheckoutProvider())
}

func TestBillingConfig_UsageLookback(t *testing.T) {
	tests := []struct {
		name  string
		hours int
		want  time.Duration
	}{
		{"unset_defaults_to_6h", 0, 6 * time.Hour},
		{"configured", 12, 12 * time.Hour},
		{"negative_defaults_to_6h", -1, 6 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, BillingConfig{UsageLookbackHours: tt.hours}.UsageLookback())
		})
	}
}

func TestBillingConfig_IsBillingTenant(t *testing.T) {
	assert.True(t, BillingConfig{TenantID: "t_b"}.IsBillingTenant("t_b"))
	assert.False(t, BillingConfig{}.IsBillingTenant(""))
	assert.False(t, BillingConfig{TenantID: "t_b"}.IsBillingTenant("t_x"))
}

func TestBillingConfig_HasSecureZapierWebhookURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://hooks.zapier.com/hooks/catch/1/abc/", true},
		{"http://hooks.zapier.com/hooks/catch/1/abc/", false},
		{"http://localhost:8080/hook", true},
		{"http://127.0.0.1:8080/hook", true},
		{"", false},
		{"not a url", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.want, BillingConfig{ZapierWebhookURL: tt.url}.HasSecureZapierWebhookURL())
		})
	}
}
