package synthetic

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

	Slack SlackConfig
	OTEL  OTELConfig

	Checks map[string]CheckConfig
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
	"WALLET_BALANCE_PROBE",
	"WALLET_DEBIT_VERIFICATION",
	"CYCLE_INVOICE_PROBE",
	"ENTITLEMENT_AND_USAGE_PROBE",
	"NEW_CUSTOMER_LIFECYCLE",
	"CANCEL_CUSTOMER_FLOW",
	"SUBSCRIPTION_MODIFICATION_FLOW",
	"LOW_WALLET_ALERT_LISTENER",
	"JANITOR",
}

var checkDefaultIntervals = map[string]time.Duration{
	"SEED_ENSURE":                    6 * time.Hour,
	"EVENT_INGEST_DRIVER":            1 * time.Second, // rate-scheduled internally
	"ANALYTICS_PROBE":                2 * time.Minute,
	"WALLET_BALANCE_PROBE":           2 * time.Minute,
	"WALLET_DEBIT_VERIFICATION":      20 * time.Minute,
	"CYCLE_INVOICE_PROBE":            15 * time.Minute,
	"ENTITLEMENT_AND_USAGE_PROBE":    5 * time.Minute,
	"NEW_CUSTOMER_LIFECYCLE":         10 * time.Minute,
	"CANCEL_CUSTOMER_FLOW":           30 * time.Minute,
	"SUBSCRIPTION_MODIFICATION_FLOW": 20 * time.Minute,
	"LOW_WALLET_ALERT_LISTENER":      0, // listener — not a ticker
	"JANITOR":                        1 * time.Hour,
}

func LoadConfig() (*Config, error) {
	c := &Config{
		APIHost:         os.Getenv("SYNTHETIC_API_HOST"),
		APIKey:          os.Getenv("SYNTHETIC_API_KEY"),
		Enabled:         getBool("SYNTHETIC_ENABLED", true),
		DryRun:          getBool("SYNTHETIC_DRY_RUN", false),
		EventIngestRate: getInt("SYNTHETIC_EVENT_INGEST_RATE", 5),
		EventIngestSeed: getInt64("SYNTHETIC_EVENT_INGEST_SEED", time.Now().UnixNano()),
		ListenerPort:    getInt("SYNTHETIC_LISTENER_PORT", 8765),
		Slack: SlackConfig{
			WebhookURL: os.Getenv("SYNTHETIC_SLACK_WEBHOOK_URL"),
			Channel:    os.Getenv("SYNTHETIC_SLACK_CHANNEL"),
		},
		OTEL: OTELConfig{
			Enabled: getBool("SYNTHETIC_OTEL_ENABLED", true),
		},
		Checks: make(map[string]CheckConfig, len(CheckNames)),
	}
	for _, name := range CheckNames {
		c.Checks[name] = CheckConfig{
			Enabled:  getBool("SYNTHETIC_CHECK_"+name+"_ENABLED", true),
			Interval: getDuration("SYNTHETIC_CHECK_"+name+"_INTERVAL", checkDefaultIntervals[name]),
		}
	}
	if c.APIHost == "" {
		return nil, errors.New("SYNTHETIC_API_HOST is required")
	}
	if c.APIKey == "" {
		return nil, errors.New("SYNTHETIC_API_KEY is required")
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

func getInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "synthetic: warning: %s=%q is not a valid int; using default %d\n", key, v, def)
		return def
	}
	return n
}

func getInt64(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "synthetic: warning: %s=%q is not a valid int64; using default %d\n", key, v, def)
		return def
	}
	return n
}

func getDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "synthetic: warning: %s=%q is not a valid duration; using default %s\n", key, v, def)
		return def
	}
	return d
}

func init() {
	if len(CheckNames) != len(checkDefaultIntervals) {
		panic(fmt.Sprintf("synthetic config: CheckNames has %d entries but checkDefaultIntervals has %d", len(CheckNames), len(checkDefaultIntervals)))
	}
	for _, name := range CheckNames {
		if _, ok := checkDefaultIntervals[name]; !ok {
			panic(fmt.Sprintf("synthetic config: CheckNames has %q but checkDefaultIntervals lacks it", name))
		}
	}
}
