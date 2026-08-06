package config

import (
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// An unset protocol must keep resolving to Native: every deployment predating the
// field relies on that, and the failure mode of getting it wrong (native client on
// an HTTP port) is an opaque "[handshake] unexpected packet [72] from server".
func TestClickHouseConfig_GetClientOptions_Protocol(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		want     clickhouse.Protocol
	}{
		{"unset defaults to native", "", clickhouse.Native},
		{"explicit native", "native", clickhouse.Native},
		{"http", "http", clickhouse.HTTP},
		{"http is case-insensitive", "HTTP", clickhouse.HTTP},
		{"unrecognised falls back to native", "grpc", clickhouse.Native},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ClickHouseConfig{
				Address:        "127.0.0.1:9000",
				Username:       "u",
				Password:       "p",
				Database:       "d",
				MaxMemoryUsage: 50,
				Protocol:       tt.protocol,
			}
			if got := c.GetClientOptions().Protocol; got != tt.want {
				t.Errorf("Protocol: got %v, want %v", got, tt.want)
			}
		})
	}
}

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
