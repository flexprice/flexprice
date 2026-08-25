package kafka

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
)

func TestGetSaramaConfigSetsOffsetRetention(t *testing.T) {
	const retention = 7 * 24 * time.Hour
	cfg, err := GetSaramaConfig(&config.KafkaConfig{OffsetRetention: retention})
	if err != nil {
		t.Fatalf("GetSaramaConfig() error = %v, want nil", err)
	}

	if cfg.Consumer.Offsets.Retention != retention {
		t.Fatalf("consumer offset retention = %s, want %s", cfg.Consumer.Offsets.Retention, retention)
	}
}
