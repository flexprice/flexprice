package e2eprobe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Config is the environment the probe runs in. Everything about what it runs
// lives in the suite file; only credentials, endpoints and process knobs stay here.
type Config struct {
	APIHost string
	APIKey  string
	Enabled bool
	DryRun  bool

	// TenantID and EnvironmentID are optional context for alerts.
	TenantID      string
	EnvironmentID string

	// Email and Password enable signup bootstrap when APIKey is empty.
	Email               string
	Password            string
	BootstrapSecretName string
	BootstrapSecretKey  string
	PodNamespace        string

	LogLevel     string
	ListenerPort int

	SuiteFile    string // E2EPROBE_SUITE_FILE; empty means the embedded default suite
	StateFile    string // E2EPROBE_STATE_FILE; incident snapshot for restarts
	WebhookToken string // E2EPROBE_WEBHOOK_TOKEN; shared secret the inbox requires when set

	Slack SlackConfig
	OTEL  OTELConfig

	// Warnings are malformed or removed env vars; main logs them once the
	// logger exists.
	Warnings []string
}

type SlackConfig struct {
	WebhookURL string
	// BotToken wins over WebhookURL and requires Channel.
	BotToken string
	Channel  string
}

type OTELConfig struct {
	Enabled bool
}

// removedEnv maps env vars the suite file replaced to the suite key to use.
var removedEnv = map[string]string{
	"E2EPROBE_EVENT_INGEST_RATE":  "defaults.tick and the persona's events profile",
	"E2EPROBE_EVENT_INGEST_SEED":  "the events profile (batches are deterministic)",
	"E2EPROBE_HEARTBEAT_INTERVAL": "defaults.digest.every",
	"E2EPROBE_JANITOR_MAX_AGE":    "defaults.janitor.max_age",
}

// removedEnvPrefixes cover the per-check, payment and billing-matrix families.
var removedEnvPrefixes = map[string]string{
	"E2EPROBE_CHECK_":          "the scenarios list (name, every, enabled, personas)",
	"E2EPROBE_PAYMENTS_":       "a persona with the payment trait",
	"E2EPROBE_BILLING_MATRIX_": "the billing-matrix scenario entry (enabled, families, per_run, assert_known_issues)",
}

func LoadConfig() (*Config, error) {
	var warnings []string
	c := &Config{
		APIHost:             os.Getenv("E2EPROBE_API_HOST"),
		APIKey:              os.Getenv("E2EPROBE_API_KEY"),
		Enabled:             getBool("E2EPROBE_ENABLED", true),
		DryRun:              getBool("E2EPROBE_DRY_RUN", false),
		TenantID:            os.Getenv("E2EPROBE_TENANT_ID"),
		EnvironmentID:       os.Getenv("E2EPROBE_ENVIRONMENT_ID"),
		Email:               os.Getenv("E2EPROBE_EMAIL"),
		Password:            os.Getenv("E2EPROBE_PASSWORD"),
		BootstrapSecretName: os.Getenv("E2EPROBE_BOOTSTRAP_SECRET_NAME"),
		BootstrapSecretKey:  os.Getenv("E2EPROBE_BOOTSTRAP_SECRET_KEY"),
		PodNamespace:        os.Getenv("POD_NAMESPACE"),
		LogLevel:            getLogLevel(&warnings, "E2EPROBE_LOG_LEVEL", "info"),
		ListenerPort:        getInt(&warnings, "E2EPROBE_LISTENER_PORT", 8765),
		SuiteFile:           os.Getenv("E2EPROBE_SUITE_FILE"),
		StateFile:           os.Getenv("E2EPROBE_STATE_FILE"),
		WebhookToken:        os.Getenv("E2EPROBE_WEBHOOK_TOKEN"),
		Slack: SlackConfig{
			WebhookURL: os.Getenv("E2EPROBE_SLACK_WEBHOOK_URL"),
			BotToken:   os.Getenv("E2EPROBE_SLACK_BOT_TOKEN"),
			Channel:    os.Getenv("E2EPROBE_SLACK_CHANNEL"),
		},
		OTEL: OTELConfig{
			Enabled: getBool("E2EPROBE_OTEL_ENABLED", false),
		},
	}
	if c.StateFile == "" {
		c.StateFile = filepath.Join(os.TempDir(), "e2eprobe-state.json")
	}
	warnings = append(warnings, removedEnvWarnings(os.Environ())...)
	c.Warnings = warnings

	if c.APIHost == "" {
		return nil, errors.New("E2EPROBE_API_HOST is required")
	}
	if c.APIKey == "" && !c.NeedsBootstrap() {
		return nil, errors.New("no credentials: set E2EPROBE_API_KEY, or set E2EPROBE_EMAIL and E2EPROBE_PASSWORD to bootstrap one (requires the flexprice-native auth provider)")
	}
	if c.Slack.BotToken != "" && c.Slack.Channel == "" {
		return nil, errors.New("E2EPROBE_SLACK_CHANNEL is required when E2EPROBE_SLACK_BOT_TOKEN is set")
	}
	return c, nil
}

// removedEnvWarnings names every env var the suite file replaced that is still set.
func removedEnvWarnings(environ []string) []string {
	var out []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if key, ok := removedEnv[name]; ok {
			out = append(out, fmt.Sprintf("%s is no longer read; set %s in the suite file", name, key))
			continue
		}
		for prefix, key := range removedEnvPrefixes {
			if strings.HasPrefix(name, prefix) {
				out = append(out, fmt.Sprintf("%s is no longer read; set %s in the suite file", name, key))
			}
		}
	}
	sort.Strings(out)
	return out
}

// NeedsBootstrap reports whether the probe must provision its own API key.
func (c *Config) NeedsBootstrap() bool {
	return c.APIKey == "" && c.Email != "" && c.Password != ""
}

func getBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return strings.EqualFold(v, "true") || v == "1"
}

func getInt(warnings *[]string, key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("%s=%q is not a valid int; using default %d", key, v, def))
		return def
	}
	return n
}

func getLogLevel(warnings *[]string, key, def string) string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "debug", "info", "warn", "error":
		return v
	}
	*warnings = append(*warnings, fmt.Sprintf("%s=%q is not a valid log level (debug|info|warn|error); using default %q", key, v, def))
	return def
}
