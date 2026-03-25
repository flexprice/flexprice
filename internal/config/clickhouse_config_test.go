package config

import (
	"testing"
)

func TestClickHouseConfig_GetClientOptions_MaxMemoryUsage(t *testing.T) {
	// Hardcoded limit: 50 GB
	const wantMaxMemoryUsageBytes = 50 * 1024 * 1024 * 1024

	c := ClickHouseConfig{
		Address:        "127.0.0.1:9000",
		Username:       "u",
		Password:       "p",
		Database:       "d",
		MaxMemoryUsage: 50,
	}
	opts := c.GetClientOptions()
	if opts.Settings == nil {
		t.Fatal("expected Settings to be set")
	}
	v, ok := opts.Settings["max_memory_usage"]
	if !ok {
		t.Fatal("expected max_memory_usage to be in Settings")
	}
	got, ok := v.(int64)
	if !ok {
		t.Fatalf("max_memory_usage value type: got %T, want int64", v)
	}
	if got != wantMaxMemoryUsageBytes {
		t.Errorf("max_memory_usage: got %d, want %d (50 GB)", got, wantMaxMemoryUsageBytes)
	}
}
