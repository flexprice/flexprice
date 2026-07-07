package config

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

// TestAutoBindEnvCategories proves that the reflective bindEnvs registration makes every
// category of config key resolvable from its FLEXPRICE_* env var via NewConfig — including
// the exact cases that previously required a hand-written v.BindEnv (secrets, env-only
// toggles, nested-underscore keys, the per-pod deployment mode, and the optional
// kafka_secondary pointer struct). The package's config.yaml is not on Viper's search path
// here, so every asserted value comes purely from the environment — the worst case (a key
// absent from the loaded file), which is exactly what used to silently zero-out.
func TestAutoBindEnvCategories(t *testing.T) {
	t.Setenv("FLEXPRICE_POSTGRES_HOST", "rds.example.com")                // normal nested key
	t.Setenv("FLEXPRICE_AUTH_SECRET", "super-secret")                     // secret
	t.Setenv("FLEXPRICE_REDIS_CLUSTER_MODE", "true")                      // env-only bool toggle
	t.Setenv("FLEXPRICE_OTEL_TRACES_ENDPOINT", "ingest.example.com:443")  // deep nested underscore
	t.Setenv("FLEXPRICE_DEPLOYMENT_MODE", "consumer")                     // per-pod
	t.Setenv("FLEXPRICE_KAFKA_SASL_PASSWORD", "scram-pw")                 // secret, nested
	t.Setenv("FLEXPRICE_KAFKA_SECONDARY_CONSUMER_GROUP", "gmk-dualwrite") // pointer struct field

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() error: %v", err)
	}

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"postgres.host", cfg.Postgres.Host, "rds.example.com"},
		{"auth.secret", cfg.Auth.Secret, "super-secret"},
		{"otel.traces.endpoint", cfg.Otel.Traces.Endpoint, "ingest.example.com:443"},
		{"deployment.mode", string(cfg.Deployment.Mode), string(types.ModeConsumer)},
		{"kafka.sasl_password", cfg.Kafka.SASLPassword, "scram-pw"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q (env override not honored)", c.name, c.got, c.want)
		}
	}

	if !cfg.Redis.ClusterMode {
		t.Errorf("redis.cluster_mode = false, want true (env-only bool not honored)")
	}

	// kafka_secondary is a *KafkaConfig (nil unless configured); the env binding must both
	// allocate it and populate the field.
	if cfg.KafkaSecondary == nil {
		t.Fatalf("kafka_secondary is nil; env binding did not allocate the pointer struct")
	}
	if cfg.KafkaSecondary.ConsumerGroup != "gmk-dualwrite" {
		t.Errorf("kafka_secondary.consumer_group = %q, want %q", cfg.KafkaSecondary.ConsumerGroup, "gmk-dualwrite")
	}
}

// TestAutoBindLeavesManualJSONIntact confirms the JSON-string env vars that are parsed by
// hand after Unmarshal (and deliberately NOT bound, since mapstructure would choke on a
// JSON string -> map) still work — i.e. bindEnvs correctly skips map fields.
func TestAutoBindLeavesManualJSONIntact(t *testing.T) {
	t.Setenv("FLEXPRICE_AUTH_API_KEY_KEYS", `{"hash123":{"tenant_id":"t1","user_id":"u1","name":"k","is_active":true}}`)

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() error: %v", err)
	}
	d, ok := cfg.Auth.APIKey.Keys["hash123"]
	if !ok {
		t.Fatalf("auth.api_key.keys not parsed from JSON env var; got %#v", cfg.Auth.APIKey.Keys)
	}
	if d.TenantID != "t1" {
		t.Errorf("tenant_id = %q, want %q", d.TenantID, "t1")
	}
}

// TestAutoBindLeavesOptionalPointerNil guards a subtle risk of reflective binding: bindEnvs
// registers every kafka_secondary.* key, but registration alone must NOT materialize the
// optional *KafkaConfig pointer. If it did, KafkaSecondary would become a non-nil empty
// struct and the source event publisher would silently switch on dual-write (publishing to a
// second, unconfigured cluster). With no FLEXPRICE_KAFKA_SECONDARY_* env set, the pointer
// must stay nil so publishing remains single-cluster.
func TestAutoBindLeavesOptionalPointerNil(t *testing.T) {
	// Intentionally set no FLEXPRICE_KAFKA_SECONDARY_* variables.
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() error: %v", err)
	}
	if cfg.KafkaSecondary != nil {
		t.Errorf("kafka_secondary = %#v, want nil (reflective bind must not allocate the optional pointer struct)", cfg.KafkaSecondary)
	}
}

// TestCustomEnvAliasesBind guards the env-var names that do NOT follow the struct-derived
// FLEXPRICE_<PATH> convention and therefore CANNOT be covered by the reflection walker —
// they must stay as explicit v.BindEnv calls. Dropping the svix alias broke Svix with 401s
// on staging (2026-07-06); this test fails loudly if any such alias is removed again.
func TestCustomEnvAliasesBind(t *testing.T) {
	cases := []struct {
		env, val string
		get      func(*Configuration) string
	}{
		{"SERVICE_NAME", "probe-svc", func(c *Configuration) string { return c.Logging.ServiceName }},
		{"ENVIRONMENT", "probe-env", func(c *Configuration) string { return c.Logging.Environment }},
		{"REGION", "probe-region", func(c *Configuration) string { return c.Logging.Region }},
		{"FLEXPRICE_SVIX_API_KEY", "probe-svix-token", func(c *Configuration) string { return c.Webhook.Svix.AuthToken }},
	}
	for _, tc := range cases {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv(tc.env, tc.val)
			cfg, err := NewConfig()
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := tc.get(cfg); got != tc.val {
				t.Errorf("env %s did not bind: got %q, want %q — custom alias likely dropped", tc.env, got, tc.val)
			}
		})
	}
}
