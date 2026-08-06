package config

import "testing"

// The producer switch is env-only in most deployments, so it has to bind from the
// environment rather than depend on config.yaml being present.
func TestEventBulkPublishEnabledFromEnv(t *testing.T) {
	t.Setenv("FLEXPRICE_EVENT_BULK_PUBLISH_ENABLED", "true")

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() error: %v", err)
	}
	if !cfg.Event.BulkPublishEnabled {
		t.Error("event.bulk_publish_enabled = false, want true (env override not honored)")
	}
}
