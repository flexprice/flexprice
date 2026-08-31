package e2eprobe

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	APIHost         string
	APIKey          string
	Enabled         bool
	DryRun          bool
	EventIngestRate int
	EventIngestSeed int64
	ListenerPort    int

	// TenantID and EnvironmentID are optional context fields included in every
	// failure report to make Slack/OTEL alerts immediately actionable.
	TenantID      string // E2EPROBE_TENANT_ID, optional but recommended
	EnvironmentID string // E2EPROBE_ENVIRONMENT_ID, optional but recommended

	// Email and Password enable signup bootstrap for airgapped deployments
	// that have no way to pre-provision an API key. Used only when APIKey
	// is empty. Bootstrap requires the flexprice-native auth provider.
	Email    string // E2EPROBE_EMAIL
	Password string // E2EPROBE_PASSWORD

	// BootstrapSecretName/Key name the Kubernetes Secret that bootstrap
	// patches the minted key into, and PodNamespace is where it lives.
	BootstrapSecretName string // E2EPROBE_BOOTSTRAP_SECRET_NAME
	BootstrapSecretKey  string // E2EPROBE_BOOTSTRAP_SECRET_KEY
	PodNamespace        string // POD_NAMESPACE, via the downward API

	// HeartbeatInterval controls how often a structured "e2eprobe.heartbeat" log
	// line is emitted summarising run counts and success rate. Set to 0 to disable.
	HeartbeatInterval time.Duration // E2EPROBE_HEARTBEAT_INTERVAL, default 1h

	// JanitorMaxAge is the minimum age of an ephemeral entity before the janitor
	// deletes it. Also controls the Flexprice-wide orphan scan sweep.
	JanitorMaxAge time.Duration // E2EPROBE_JANITOR_MAX_AGE, default 1h

	// LogLevel controls the zerolog level: "debug", "info", "warn", "error".
	// Default "info". Flip to "debug" to surface the per-tick checkpoints
	// each probe emits (analytics fetched, drop event ingested, webhook
	// received, aggregation observed, startup grace hits, etc.).
	LogLevel string // E2EPROBE_LOG_LEVEL, default "info"

	Slack SlackConfig
	OTEL  OTELConfig

	Checks map[string]CheckConfig

	// Warnings collected during LoadConfig (e.g. malformed env vars that fell
	// back to defaults). main.go drains these into the logger after the logger
	// has been constructed, so we don't write to stderr from a non-bootstrap
	// package.
	Warnings []string
}

type SlackConfig struct {
	WebhookURL string
	Channel    string
}

type OTELConfig struct {
	Enabled bool
}

type CheckConfig struct {
	Enabled  bool
	Interval time.Duration
}

// CheckNames is the canonical list of check identifiers. Adding a new check
// requires extending this list AND adding a default interval below.
var CheckNames = []string{
	"SEED_ENSURE",
	"EVENT_INGEST_DRIVER",
	"ANALYTICS_PROBE",
	"METER_AGGREGATION_PROBE",
	"WALLET_BALANCE_PROBE",
	"WALLET_DEBIT_VERIFICATION",
	"CYCLE_INVOICE_PROBE",
	"MULTI_CADENCE_INVOICE_PROBE",
	"ENTITLEMENT_AND_USAGE_PROBE",
	"NEW_CUSTOMER_LIFECYCLE",
	"CANCEL_CUSTOMER_FLOW",
	"SUBSCRIPTION_MODIFICATION_FLOW",
	"BUCKETED_METER_PROBE",
	"COMMITMENT_TRUE_UP_PROBE",
	"ENTITLEMENT_ENFORCEMENT_PROBE",
	"TAX_APPLICATION_PROBE",
	"COUPON_APPLICATION_PROBE",
	"PERSISTENT_BILLING_INVARIANTS_PROBE",
	"ENTITLEMENT_GRANT_ADDITIVE_PROBE",
	"LOW_WALLET_ALERT_LISTENER",
	"LOW_BALANCE_ALERT_PROBE",
	"JANITOR",
}

var checkDefaultIntervals = map[string]time.Duration{
	"SEED_ENSURE":                         6 * time.Hour,
	"EVENT_INGEST_DRIVER":                 1 * time.Second, // rate-scheduled internally
	"ANALYTICS_PROBE":                     2 * time.Minute,
	"METER_AGGREGATION_PROBE":             3 * time.Minute,
	"WALLET_BALANCE_PROBE":                2 * time.Minute,
	"WALLET_DEBIT_VERIFICATION":           20 * time.Minute,
	"CYCLE_INVOICE_PROBE":                 15 * time.Minute,
	"MULTI_CADENCE_INVOICE_PROBE":         15 * time.Minute,
	"ENTITLEMENT_AND_USAGE_PROBE":         5 * time.Minute,
	"NEW_CUSTOMER_LIFECYCLE":              10 * time.Minute,
	"CANCEL_CUSTOMER_FLOW":                30 * time.Minute,
	"SUBSCRIPTION_MODIFICATION_FLOW":      20 * time.Minute,
	"BUCKETED_METER_PROBE":                12 * time.Minute,
	"COMMITMENT_TRUE_UP_PROBE":            15 * time.Minute,
	"ENTITLEMENT_ENFORCEMENT_PROBE":       8 * time.Minute,
	"TAX_APPLICATION_PROBE":               15 * time.Minute,
	"COUPON_APPLICATION_PROBE":            15 * time.Minute,
	"PERSISTENT_BILLING_INVARIANTS_PROBE": 30 * time.Minute,
	"ENTITLEMENT_GRANT_ADDITIVE_PROBE":    15 * time.Minute,
	"LOW_WALLET_ALERT_LISTENER":           0, // listener — not a ticker
	"LOW_BALANCE_ALERT_PROBE":             5 * time.Minute,
	"JANITOR":                             1 * time.Hour,
}

func LoadConfig() (*Config, error) {
	var warnings []string
	c := &Config{
		APIHost:             os.Getenv("E2EPROBE_API_HOST"),
		APIKey:              os.Getenv("E2EPROBE_API_KEY"),
		Enabled:             getBool("E2EPROBE_ENABLED", true),
		DryRun:              getBool("E2EPROBE_DRY_RUN", false),
		EventIngestRate:     getInt(&warnings, "E2EPROBE_EVENT_INGEST_RATE", 1),
		EventIngestSeed:     getInt64(&warnings, "E2EPROBE_EVENT_INGEST_SEED", 42),
		ListenerPort:        getInt(&warnings, "E2EPROBE_LISTENER_PORT", 8765),
		TenantID:            os.Getenv("E2EPROBE_TENANT_ID"),
		EnvironmentID:       os.Getenv("E2EPROBE_ENVIRONMENT_ID"),
		Email:               os.Getenv("E2EPROBE_EMAIL"),
		Password:            os.Getenv("E2EPROBE_PASSWORD"),
		BootstrapSecretName: os.Getenv("E2EPROBE_BOOTSTRAP_SECRET_NAME"),
		BootstrapSecretKey:  os.Getenv("E2EPROBE_BOOTSTRAP_SECRET_KEY"),
		PodNamespace:        os.Getenv("POD_NAMESPACE"),
		HeartbeatInterval:   getDuration(&warnings, "E2EPROBE_HEARTBEAT_INTERVAL", 1*time.Hour),
		JanitorMaxAge:       getDuration(&warnings, "E2EPROBE_JANITOR_MAX_AGE", 1*time.Hour),
		LogLevel:            getLogLevel(&warnings, "E2EPROBE_LOG_LEVEL", "info"),
		Slack: SlackConfig{
			WebhookURL: os.Getenv("E2EPROBE_SLACK_WEBHOOK_URL"),
			Channel:    os.Getenv("E2EPROBE_SLACK_CHANNEL"),
		},
		OTEL: OTELConfig{
			Enabled: getBool("E2EPROBE_OTEL_ENABLED", false),
		},
		Checks: make(map[string]CheckConfig, len(CheckNames)),
	}
	for _, name := range CheckNames {
		c.Checks[name] = CheckConfig{
			Enabled:  getBool("E2EPROBE_CHECK_"+name+"_ENABLED", true),
			Interval: getDuration(&warnings, "E2EPROBE_CHECK_"+name+"_INTERVAL", checkDefaultIntervals[name]),
		}
	}
	c.Warnings = warnings
	if c.APIHost == "" {
		return nil, errors.New("E2EPROBE_API_HOST is required")
	}
	if c.APIKey == "" && !c.NeedsBootstrap() {
		return nil, errors.New("no credentials: set E2EPROBE_API_KEY, or set E2EPROBE_EMAIL and E2EPROBE_PASSWORD to bootstrap one (requires the flexprice-native auth provider)")
	}
	return c, nil
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

func getInt64(warnings *[]string, key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("%s=%q is not a valid int64; using default %d", key, v, def))
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

func getDuration(warnings *[]string, key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("%s=%q is not a valid duration; using default %s", key, v, def))
		return def
	}
	return d
}

// NeedsBootstrap reports whether the probe must provision its own API key.
// An explicit API key always wins, so an existing deployment never changes
// behaviour by having credentials present.
func (c *Config) NeedsBootstrap() bool {
	return c.APIKey == "" && c.Email != "" && c.Password != ""
}

func init() {
	if len(CheckNames) != len(checkDefaultIntervals) {
		panic(fmt.Sprintf("e2eprobe config: CheckNames has %d entries but checkDefaultIntervals has %d", len(CheckNames), len(checkDefaultIntervals)))
	}
	for _, name := range CheckNames {
		if _, ok := checkDefaultIntervals[name]; !ok {
			panic(fmt.Sprintf("e2eprobe config: CheckNames has %q but checkDefaultIntervals lacks it", name))
		}
	}
}
