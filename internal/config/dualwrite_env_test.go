package config

import "testing"

// TestKafkaSecondaryEnvBinding verifies the second-cluster keys (whose segment contains an
// underscore) actually bind from FLEXPRICE_* env vars through the real NewConfig loader, so
// setting FLEXPRICE_KAFKA_SECONDARY_* makes cfg.KafkaSecondary non-nil (presence-based
// dual-write). Without the explicit BindEnv calls these would be silently dropped.
func TestKafkaSecondaryEnvBinding(t *testing.T) {
	t.Setenv("FLEXPRICE_KAFKA_SECONDARY_BROKERS", "other-bootstrap:9092")
	t.Setenv("FLEXPRICE_KAFKA_SECONDARY_USE_SASL", "true")
	t.Setenv("FLEXPRICE_KAFKA_SECONDARY_SASL_MECHANISM", "OAUTHBEARER")
	t.Setenv("FLEXPRICE_KAFKA_SECONDARY_TOPIC", "events")

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig failed: %v", err)
	}

	if cfg.KafkaSecondary == nil {
		t.Fatal("expected KafkaSecondary to be populated from env (dual-write on), got nil")
	}
	if len(cfg.KafkaSecondary.Brokers) == 0 || cfg.KafkaSecondary.Brokers[0] != "other-bootstrap:9092" {
		t.Errorf("expected secondary brokers from env, got %v", cfg.KafkaSecondary.Brokers)
	}
	if !cfg.KafkaSecondary.UseSASL || string(cfg.KafkaSecondary.SASLMechanism) != "OAUTHBEARER" {
		t.Errorf("expected secondary SASL OAUTHBEARER, got use_sasl=%v mechanism=%q",
			cfg.KafkaSecondary.UseSASL, cfg.KafkaSecondary.SASLMechanism)
	}
}

// TestKafkaSecondaryAbsentByDefault confirms single-cluster is the default (no dual-write).
func TestKafkaSecondaryAbsentByDefault(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig failed: %v", err)
	}
	if cfg.KafkaSecondary != nil {
		t.Errorf("expected KafkaSecondary nil by default (single-cluster), got %+v", cfg.KafkaSecondary)
	}
}
