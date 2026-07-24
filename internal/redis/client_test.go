package redis

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
)

// TestResolveRedisMode locks the mode-selection precedence: Sentinel wins over
// Cluster, RouteReadsToReplicas only refines Sentinel, and the zero config is
// standalone (the backward-compatible default). No infra required.
func TestResolveRedisMode(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.RedisConfig
		want redisMode
	}{
		{
			name: "zero config -> standalone (backward compatible)",
			cfg:  config.RedisConfig{},
			want: modeStandalone,
		},
		{
			name: "cluster_mode set -> cluster",
			cfg:  config.RedisConfig{ClusterMode: true},
			want: modeCluster,
		},
		{
			name: "sentinel master set -> sentinel",
			cfg:  config.RedisConfig{SentinelMasterName: "mymaster"},
			want: modeSentinel,
		},
		{
			name: "sentinel + route reads -> sentinel-replica-read",
			cfg:  config.RedisConfig{SentinelMasterName: "mymaster", RouteReadsToReplicas: true},
			want: modeSentinelReplicaRead,
		},
		{
			name: "sentinel AND cluster both set -> sentinel wins",
			cfg:  config.RedisConfig{SentinelMasterName: "mymaster", ClusterMode: true},
			want: modeSentinel,
		},
		{
			name: "route reads without sentinel is ignored -> cluster",
			cfg:  config.RedisConfig{ClusterMode: true, RouteReadsToReplicas: true},
			want: modeCluster,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveRedisMode(tt.cfg); got != tt.want {
				t.Fatalf("resolveRedisMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestNewClient_SentinelMissingAddrsErrors verifies that a misconfigured Sentinel
// setup fails loudly rather than panicking, hanging, or (worse) silently
// connecting to go-redis's 127.0.0.1:26379 default when addrs are empty.
func TestNewClient_SentinelMissingAddrsErrors(t *testing.T) {
	tests := []struct {
		name  string
		addrs []string
	}{
		// Empty addrs must be rejected up front — go-redis would otherwise
		// substitute 127.0.0.1:26379 and connect to a phantom local sentinel.
		{name: "empty addrs", addrs: nil},
		// Unreachable addrs must surface a connection error, not hang.
		{name: "unreachable addr", addrs: []string{"127.0.0.1:1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.GetDefaultConfig()
			cfg.Redis.Timeout = 500 * time.Millisecond
			cfg.Redis.SentinelMasterName = "mymaster"
			cfg.Redis.SentinelAddrs = tt.addrs

			log, err := logger.NewLogger(cfg)
			if err != nil {
				t.Fatalf("logger: %v", err)
			}
			client, err := NewClient(cfg, log)
			if err == nil {
				if client != nil {
					_ = client.Close()
				}
				t.Fatal("expected an error, got nil")
			}
		})
	}
}
