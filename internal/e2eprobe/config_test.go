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
	if !c.Enabled || c.DryRun || c.EventIngestRate != 5 || !c.OTEL.Enabled {
		t.Errorf("defaults mismatch: %+v", c)
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
	if !ok2 || cc.Interval != 10*time.Minute {
		t.Errorf("CANCEL_CUSTOMER_FLOW default interval=%v, want 10m", cc.Interval)
	}
	if c.JanitorMaxAge != 1*time.Hour {
		t.Errorf("JanitorMaxAge=%v, want 1h", c.JanitorMaxAge)
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

	t.Run("default is 5m", func(t *testing.T) {
		t.Setenv("E2EPROBE_HEARTBEAT_INTERVAL", "")
		c, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if c.HeartbeatInterval != 5*time.Minute {
			t.Errorf("HeartbeatInterval=%v, want 5m", c.HeartbeatInterval)
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
