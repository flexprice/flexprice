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
