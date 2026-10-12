package e2eprobe

import (
	"testing"
	"time"
)

func TestLoadConfig_RequiredFields(t *testing.T) {
	t.Run("missing API host", func(t *testing.T) {
		t.Setenv("E2EPROBE_API_HOST", "")
		t.Setenv("E2EPROBE_API_KEY", "k")
		if _, err := LoadConfig(); err == nil {
			t.Fatal("expected error when E2EPROBE_API_HOST missing")
		}
	})
	t.Run("missing API key", func(t *testing.T) {
		t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
		t.Setenv("E2EPROBE_API_KEY", "")
		if _, err := LoadConfig(); err == nil {
			t.Fatal("expected error when E2EPROBE_API_KEY missing")
		}
	})
}

func TestLoadConfig_MalformedEnvFallsBackQuietly(t *testing.T) {
	t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
	t.Setenv("E2EPROBE_API_KEY", "k")
	t.Setenv("E2EPROBE_LISTENER_PORT", "not-a-number")
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: unexpected error: %v", err)
	}
	if c.ListenerPort != 8765 {
		t.Errorf("ListenerPort=%d, want 8765 (default)", c.ListenerPort)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
	t.Setenv("E2EPROBE_API_KEY", "k")
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !c.Enabled || c.DryRun || c.EventIngestRate != 1 || c.OTEL.Enabled {
		t.Errorf("defaults mismatch: %+v", c)
	}
	if c.EventIngestSeed != 42 {
		t.Errorf("EventIngestSeed=%d, want 42", c.EventIngestSeed)
	}
	if c.ListenerPort != 8765 {
		t.Errorf("ListenerPort=%d, want 8765", c.ListenerPort)
	}
	ap, ok := c.Checks["ANALYTICS_PROBE"]
	if !ok || ap.Interval != 2*time.Minute {
		t.Errorf("ANALYTICS_PROBE default missing/wrong: %+v", ap)
	}
	if _, ok := c.Checks["WALLET_DEBIT_VERIFICATION"]; !ok {
		t.Error("WALLET_DEBIT_VERIFICATION missing")
	}
	if _, ok := c.Checks["LOW_WALLET_ALERT_LISTENER"]; !ok {
		t.Error("LOW_WALLET_ALERT_LISTENER missing")
	}
	cc, ok2 := c.Checks["CANCEL_CUSTOMER_FLOW"]
	if !ok2 || cc.Interval != 30*time.Minute {
		t.Errorf("CANCEL_CUSTOMER_FLOW default interval=%v, want 30m", cc.Interval)
	}
	if c.JanitorMaxAge != 1*time.Hour {
		t.Errorf("JanitorMaxAge=%v, want 1h", c.JanitorMaxAge)
	}
	if c.BillingMatrix.Enabled {
		t.Error("billing matrix should be off unless E2EPROBE_BILLING_MATRIX_ENABLED is set")
	}
}

func TestLoadConfig_BillingMatrixGate(t *testing.T) {
	t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
	t.Setenv("E2EPROBE_API_KEY", "k")
	t.Setenv("E2EPROBE_BILLING_MATRIX_ENABLED", "true")
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !c.BillingMatrix.Enabled || c.BillingMatrix.ScenariosPerRun != 3 {
		t.Errorf("billing matrix config = %+v, want enabled with 3 scenarios per run", c.BillingMatrix)
	}
}

func TestLoadConfig_Overrides(t *testing.T) {
	t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
	t.Setenv("E2EPROBE_API_KEY", "k")
	t.Setenv("E2EPROBE_DRY_RUN", "true")
	t.Setenv("E2EPROBE_EVENT_INGEST_RATE", "12")
	t.Setenv("E2EPROBE_LISTENER_PORT", "9000")
	t.Setenv("E2EPROBE_CHECK_JANITOR_ENABLED", "false")
	t.Setenv("E2EPROBE_CHECK_JANITOR_INTERVAL", "30m")
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !c.DryRun || c.EventIngestRate != 12 || c.ListenerPort != 9000 {
		t.Errorf("overrides not applied: %+v", c)
	}
	jan := c.Checks["JANITOR"]
	if jan.Enabled || jan.Interval != 30*time.Minute {
		t.Errorf("JANITOR override: %+v", jan)
	}
}

func TestLoadConfig_JanitorMaxAge(t *testing.T) {
	t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
	t.Setenv("E2EPROBE_API_KEY", "k")

	t.Run("default is 1h", func(t *testing.T) {
		t.Setenv("E2EPROBE_JANITOR_MAX_AGE", "")
		c, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if c.JanitorMaxAge != 1*time.Hour {
			t.Errorf("JanitorMaxAge=%v, want 1h", c.JanitorMaxAge)
		}
	})

	t.Run("override to 30m", func(t *testing.T) {
		t.Setenv("E2EPROBE_JANITOR_MAX_AGE", "30m")
		c, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if c.JanitorMaxAge != 30*time.Minute {
			t.Errorf("JanitorMaxAge=%v, want 30m", c.JanitorMaxAge)
		}
	})
}

func TestLoadConfig_HeartbeatInterval(t *testing.T) {
	t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
	t.Setenv("E2EPROBE_API_KEY", "k")

	t.Run("default is 1h", func(t *testing.T) {
		t.Setenv("E2EPROBE_HEARTBEAT_INTERVAL", "")
		c, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if c.HeartbeatInterval != 1*time.Hour {
			t.Errorf("HeartbeatInterval=%v, want 1h", c.HeartbeatInterval)
		}
	})

	t.Run("override to 2m", func(t *testing.T) {
		t.Setenv("E2EPROBE_HEARTBEAT_INTERVAL", "2m")
		c, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if c.HeartbeatInterval != 2*time.Minute {
			t.Errorf("HeartbeatInterval=%v, want 2m", c.HeartbeatInterval)
		}
	})

	t.Run("0s disables heartbeat", func(t *testing.T) {
		t.Setenv("E2EPROBE_HEARTBEAT_INTERVAL", "0s")
		c, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if c.HeartbeatInterval != 0 {
			t.Errorf("HeartbeatInterval=%v, want 0 (disabled)", c.HeartbeatInterval)
		}
	})
}

func TestLoadConfig_TenantAndEnvironment(t *testing.T) {
	t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
	t.Setenv("E2EPROBE_API_KEY", "k")
	t.Run("both set", func(t *testing.T) {
		t.Setenv("E2EPROBE_TENANT_ID", "tenant-abc")
		t.Setenv("E2EPROBE_ENVIRONMENT_ID", "env-prod")
		c, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if c.TenantID != "tenant-abc" {
			t.Errorf("TenantID=%q, want tenant-abc", c.TenantID)
		}
		if c.EnvironmentID != "env-prod" {
			t.Errorf("EnvironmentID=%q, want env-prod", c.EnvironmentID)
		}
	})
	t.Run("not set defaults to empty string", func(t *testing.T) {
		t.Setenv("E2EPROBE_TENANT_ID", "")
		t.Setenv("E2EPROBE_ENVIRONMENT_ID", "")
		c, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if c.TenantID != "" {
			t.Errorf("TenantID=%q, want empty", c.TenantID)
		}
		if c.EnvironmentID != "" {
			t.Errorf("EnvironmentID=%q, want empty", c.EnvironmentID)
		}
	})
}

func TestLoadConfig_APIKeyOptionalWithCredentials(t *testing.T) {
	t.Setenv("E2EPROBE_API_HOST", "http://localhost:8080/v1")
	t.Setenv("E2EPROBE_API_KEY", "")
	t.Setenv("E2EPROBE_EMAIL", "probe@example.com")
	t.Setenv("E2EPROBE_PASSWORD", "hunter2hunter2")

	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("expected config to load without an API key, got: %v", err)
	}
	if !c.NeedsBootstrap() {
		t.Fatal("expected NeedsBootstrap() to be true")
	}
}

func TestLoadConfig_APIKeyStillRequiredWithoutCredentials(t *testing.T) {
	t.Setenv("E2EPROBE_API_HOST", "http://localhost:8080/v1")
	t.Setenv("E2EPROBE_API_KEY", "")
	t.Setenv("E2EPROBE_EMAIL", "")
	t.Setenv("E2EPROBE_PASSWORD", "")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected an error naming both auth options")
	}
}

func TestLoadConfig_APIKeyWinsOverCredentials(t *testing.T) {
	t.Setenv("E2EPROBE_API_HOST", "http://localhost:8080/v1")
	t.Setenv("E2EPROBE_API_KEY", "sk_existing")
	t.Setenv("E2EPROBE_EMAIL", "probe@example.com")
	t.Setenv("E2EPROBE_PASSWORD", "hunter2hunter2")

	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.NeedsBootstrap() {
		t.Fatal("an explicit API key must take precedence over credentials")
	}
}

func TestLoadConfig_Payments(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
		check   func(t *testing.T, p PaymentsConfig)
	}{
		{
			name: "unset disables payment probes",
			check: func(t *testing.T, p PaymentsConfig) {
				if len(p.Providers) != 0 {
					t.Errorf("providers = %+v, want none", p.Providers)
				}
				if p.SettleTimeout != 90*time.Second {
					t.Errorf("SettleTimeout = %s, want 90s", p.SettleTimeout)
				}
			},
		},
		{
			name: "providers with defaults and overrides",
			env: map[string]string{
				"E2EPROBE_PAYMENTS_PROVIDERS":          " Stripe ,razorpay,chargebee",
				"E2EPROBE_PAYMENTS_CHARGEBEE_CURRENCY": "eur",
				"E2EPROBE_PAYMENTS_SETTLE_TIMEOUT":     "2m",
			},
			check: func(t *testing.T, p PaymentsConfig) {
				if len(p.Providers) != 3 {
					t.Fatalf("providers = %+v, want 3", p.Providers)
				}
				if p.Providers[0].Provider != "stripe" || p.Providers[0].Currency != "USD" {
					t.Errorf("stripe = %+v", p.Providers[0])
				}
				if p.Providers[1].Provider != "razorpay" || p.Providers[1].Currency != "INR" {
					t.Errorf("razorpay = %+v", p.Providers[1])
				}
				if p.Providers[2].Provider != "chargebee" || p.Providers[2].Currency != "EUR" {
					t.Errorf("chargebee = %+v", p.Providers[2])
				}
				if p.SettleTimeout != 2*time.Minute {
					t.Errorf("SettleTimeout = %s, want 2m", p.SettleTimeout)
				}
			},
		},
		{
			name:    "unknown provider",
			env:     map[string]string{"E2EPROBE_PAYMENTS_PROVIDERS": "paypal"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
			t.Setenv("E2EPROBE_API_KEY", "k")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			c, err := LoadConfig()
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			tt.check(t, c.Payments)
		})
	}
}

func TestLoadConfig_PaymentsSettleTimeoutPerProvider(t *testing.T) {
	t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
	t.Setenv("E2EPROBE_API_KEY", "k")
	t.Setenv("E2EPROBE_PAYMENTS_PROVIDERS", "razorpay,stripe")
	t.Setenv("E2EPROBE_PAYMENTS_STRIPE_SETTLE_TIMEOUT", "2m")
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := c.Payments.Providers[0].SettleTimeout; got != 10*time.Minute {
		t.Errorf("razorpay SettleTimeout = %s, want 10m default", got)
	}
	if got := c.Payments.Providers[1].SettleTimeout; got != 2*time.Minute {
		t.Errorf("stripe SettleTimeout = %s, want 2m override", got)
	}
}

func TestLoadConfig_PaymentsFixedCustomer(t *testing.T) {
	t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
	t.Setenv("E2EPROBE_API_KEY", "k")
	t.Setenv("E2EPROBE_PAYMENTS_PROVIDERS", "stripe,chargebee,razorpay")
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	want := map[string][2]string{
		"stripe":    {"e2eprobe-cust-pay-stripe-cards", "0341"},
		"chargebee": {"e2eprobe-cust-pay-chargebee-cards", "0004"},
		"razorpay":  {"e2eprobe-cust-pay-razorpay-mandate", ""},
	}
	for _, p := range c.Payments.Providers {
		if got := [2]string{p.FixedCustomerExternalID, p.DeclineCardLast4}; got != want[p.Provider] {
			t.Errorf("%s = %v, want %v", p.Provider, got, want[p.Provider])
		}
		if wantInterval := map[string]time.Duration{"stripe": 6 * time.Hour, "chargebee": 6 * time.Hour, "razorpay": 12 * time.Hour}[p.Provider]; p.AutoChargeInterval != wantInterval {
			t.Errorf("%s auto-charge interval = %v, want %v", p.Provider, p.AutoChargeInterval, wantInterval)
		}
	}
}
