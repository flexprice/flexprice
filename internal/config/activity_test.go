package config

import (
	"testing"

	"github.com/spf13/viper"
)

// TestSetActivityDefaults pins the fallback for a deployment whose mounted
// config.yaml predates the `activity:` block (or omits it). Struct `default:`
// tags on ActivityConfig/ArchiveConfig have no effect at Unmarshal time, so
// without setActivityDefaults such a deployment would silently get
// HotWindowDays=0 and RowsPerFile=0 — values the archiver later divides by and
// compares against.
func TestSetActivityDefaults(t *testing.T) {
	v := viper.New()
	setActivityDefaults(v)

	var cfg Configuration
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !cfg.Activity.Enabled {
		t.Errorf("Activity.Enabled = false, want true")
	}
	if cfg.Activity.HotWindowDays != 90 {
		t.Errorf("Activity.HotWindowDays = %d, want 90", cfg.Activity.HotWindowDays)
	}
	if cfg.Activity.Archive.Enabled {
		t.Errorf("Activity.Archive.Enabled = true, want false")
	}
	if cfg.Activity.Archive.Destination != "local" {
		t.Errorf("Activity.Archive.Destination = %q, want %q", cfg.Activity.Archive.Destination, "local")
	}
	if cfg.Activity.Archive.LocalDir != "/tmp/flexprice-activity-archive" {
		t.Errorf("Activity.Archive.LocalDir = %q, want %q", cfg.Activity.Archive.LocalDir, "/tmp/flexprice-activity-archive")
	}
	if cfg.Activity.Archive.Bucket != "" {
		t.Errorf("Activity.Archive.Bucket = %q, want empty", cfg.Activity.Archive.Bucket)
	}
	if cfg.Activity.Archive.KeyPrefix != "activity_logs" {
		t.Errorf("Activity.Archive.KeyPrefix = %q, want %q", cfg.Activity.Archive.KeyPrefix, "activity_logs")
	}
	if cfg.Activity.Archive.RowsPerFile != 50000 {
		t.Errorf("Activity.Archive.RowsPerFile = %d, want 50000", cfg.Activity.Archive.RowsPerFile)
	}
}
