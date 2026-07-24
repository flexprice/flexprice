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
// setup (master name set, no reachable sentinels) fails loudly with an error
// rather than panicking or hanging. Uses a short timeout and an unroutable
// address so it returns fast without external infra.
func TestNewClient_SentinelMissingAddrsErrors(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Redis.Timeout = 500 * time.Millisecond
	cfg.Redis.SentinelMasterName = "mymaster"
	cfg.Redis.SentinelAddrs = []string{"127.0.0.1:1"} // nothing listens here

	log, err := logger.NewLogger(cfg)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	client, err := NewClient(cfg, log)
	if err == nil {
		if client != nil {
			_ = client.Close()
		}
		t.Fatal("expected an error for unreachable sentinels, got nil")
	}
}
