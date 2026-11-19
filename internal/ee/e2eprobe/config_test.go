package e2eprobe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
	t.Setenv("E2EPROBE_API_KEY", "k")
}

func TestLoadConfig_Required(t *testing.T) {
	t.Run("missing API host", func(t *testing.T) {
		t.Setenv("E2EPROBE_API_HOST", "")
		t.Setenv("E2EPROBE_API_KEY", "k")
		if _, err := LoadConfig(); err == nil {
			t.Fatal("expected error when E2EPROBE_API_HOST missing")
		}
	})
	t.Run("missing API key without credentials", func(t *testing.T) {
		t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
		t.Setenv("E2EPROBE_API_KEY", "")
		t.Setenv("E2EPROBE_EMAIL", "")
		t.Setenv("E2EPROBE_PASSWORD", "")
		if _, err := LoadConfig(); err == nil {
			t.Fatal("expected error when no credential source is set")
		}
	})
	t.Run("credentials replace the API key", func(t *testing.T) {
		t.Setenv("E2EPROBE_API_HOST", "https://api.example/v1")
		t.Setenv("E2EPROBE_API_KEY", "")
		t.Setenv("E2EPROBE_EMAIL", "probe@example.com")
		t.Setenv("E2EPROBE_PASSWORD", "pw12345678")
		c, err := LoadConfig()
		if err != nil || !c.NeedsBootstrap() {
			t.Fatalf("expected bootstrap config, got %+v %v", c, err)
		}
	})
	t.Run("API key wins over credentials", func(t *testing.T) {
		setRequired(t)
		t.Setenv("E2EPROBE_EMAIL", "probe@example.com")
		t.Setenv("E2EPROBE_PASSWORD", "pw12345678")
		c, err := LoadConfig()
		if err != nil || c.NeedsBootstrap() {
			t.Fatalf("explicit key must disable bootstrap: %+v %v", c, err)
		}
	})
}

func TestLoadConfig_Defaults(t *testing.T) {
	setRequired(t)
	t.Setenv("TMPDIR", t.TempDir())
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !c.Enabled || c.DryRun || c.OTEL.Enabled || c.LogLevel != "info" || c.ListenerPort != 8765 {
		t.Errorf("defaults mismatch: %+v", c)
	}
	if c.SuiteFile != "" || c.WebhookToken != "" {
		t.Errorf("suite file and webhook token default to empty: %+v", c)
	}
	if want := filepath.Join(os.TempDir(), "e2eprobe-state.json"); c.StateFile != want {
		t.Errorf("StateFile = %q, want %q", c.StateFile, want)
	}
	if len(c.Warnings) != 0 {
		t.Errorf("no warnings expected, got %v", c.Warnings)
	}
}

func TestLoadConfig_Overrides(t *testing.T) {
	setRequired(t)
	t.Setenv("E2EPROBE_SUITE_FILE", "/etc/e2eprobe/suite.yaml")
	t.Setenv("E2EPROBE_STATE_FILE", "/var/lib/e2eprobe/state.json")
	t.Setenv("E2EPROBE_WEBHOOK_TOKEN", "s3cret")
	t.Setenv("E2EPROBE_TENANT_ID", "tenant_1")
	t.Setenv("E2EPROBE_ENVIRONMENT_ID", "env_1")
	t.Setenv("E2EPROBE_DRY_RUN", "true")
	t.Setenv("E2EPROBE_LOG_LEVEL", "debug")
	c, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.SuiteFile != "/etc/e2eprobe/suite.yaml" || c.StateFile != "/var/lib/e2eprobe/state.json" || c.WebhookToken != "s3cret" ||
		c.TenantID != "tenant_1" || c.EnvironmentID != "env_1" || !c.DryRun || c.LogLevel != "debug" {
		t.Errorf("overrides not applied: %+v", c)
	}
}

func TestLoadConfig_MalformedEnvFallsBackQuietly(t *testing.T) {
	setRequired(t)
	t.Setenv("E2EPROBE_LISTENER_PORT", "not-a-number")
	t.Setenv("E2EPROBE_LOG_LEVEL", "loud")
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: unexpected error: %v", err)
	}
	if c.ListenerPort != 8765 || c.LogLevel != "info" {
		t.Errorf("fallbacks not applied: port=%d level=%s", c.ListenerPort, c.LogLevel)
	}
	if len(c.Warnings) != 2 {
		t.Errorf("expected two warnings, got %v", c.Warnings)
	}
}

func TestLoadConfig_WarnsOnRemovedEnv(t *testing.T) {
	cases := []struct {
		env  string
		want string
	}{
		{"E2EPROBE_EVENT_INGEST_RATE", "defaults.tick"},
		{"E2EPROBE_EVENT_INGEST_SEED", "events profile"},
		{"E2EPROBE_HEARTBEAT_INTERVAL", "defaults.digest"},
		{"E2EPROBE_JANITOR_MAX_AGE", "defaults.janitor.max_age"},
		{"E2EPROBE_CHECK_ANALYTICS_PROBE_ENABLED", "scenarios"},
		{"E2EPROBE_PAYMENTS_PROVIDERS", "payment"},
		{"E2EPROBE_BILLING_MATRIX_ENABLED", "billing-matrix"},
	}
	for _, tc := range cases {
		t.Run(tc.env, func(t *testing.T) {
			setRequired(t)
			t.Setenv(tc.env, "1")
			c, err := LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], tc.env) || !strings.Contains(c.Warnings[0], tc.want) {
				t.Errorf("warnings = %v, want one naming %s and %s", c.Warnings, tc.env, tc.want)
			}
		})
	}
}

func TestLoadConfig_SlackBotNeedsChannel(t *testing.T) {
	setRequired(t)
	t.Setenv("E2EPROBE_SLACK_BOT_TOKEN", "xoxb-1")
	t.Setenv("E2EPROBE_SLACK_CHANNEL", "")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("bot token without channel must fail")
	}
	t.Setenv("E2EPROBE_SLACK_CHANNEL", "C1")
	c, err := LoadConfig()
	if err != nil || c.Slack.BotToken != "xoxb-1" || c.Slack.Channel != "C1" {
		t.Fatalf("slack config = %+v %v", c.Slack, err)
	}
}
