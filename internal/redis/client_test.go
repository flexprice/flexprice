package redis

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
)

// TestResolveRedisMode locks mode-selection precedence (Sentinel > Cluster >
// Standalone) and the Sentinel coherence guards: master name and addresses must
// be set together. No infra required.
func TestResolveRedisMode(t *testing.T) {
	addrs := []string{"s1:26379", "s2:26379"}
	tests := []struct {
		name    string
		cfg     config.RedisConfig
		want    redisMode
		wantErr bool
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
			name: "sentinel master + addrs -> sentinel",
			cfg:  config.RedisConfig{SentinelMasterName: "mymaster", SentinelAddrs: addrs},
			want: modeSentinel,
		},
		{
			name: "sentinel + route reads -> sentinel-replica-read",
			cfg:  config.RedisConfig{SentinelMasterName: "mymaster", SentinelAddrs: addrs, RouteReadsToReplicas: true},
			want: modeSentinelReplicaRead,
		},
		{
			name: "sentinel AND cluster both set -> sentinel wins",
			cfg:  config.RedisConfig{SentinelMasterName: "mymaster", SentinelAddrs: addrs, ClusterMode: true},
			want: modeSentinel,
		},
		{
			name: "route reads without sentinel is ignored -> cluster",
			cfg:  config.RedisConfig{ClusterMode: true, RouteReadsToReplicas: true},
			want: modeCluster,
		},
		{
			name:    "master without addrs -> error",
			cfg:     config.RedisConfig{SentinelMasterName: "mymaster"},
			wantErr: true,
		},
		{
			name:    "addrs without master -> error (silent HA loss)",
			cfg:     config.RedisConfig{SentinelAddrs: addrs},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveRedisMode(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveRedisMode() expected error, got mode %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveRedisMode() unexpected error: %v", err)
			}
			if got != tt.want {
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

// TestNewClient_SentinelAddrsWithoutMasterErrors guards the inverse misconfig:
// sentinel addresses set but SentinelMasterName empty must fail fast rather than
// silently dropping the addresses and running without HA.
func TestNewClient_SentinelAddrsWithoutMasterErrors(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Redis.Timeout = 500 * time.Millisecond
	cfg.Redis.SentinelMasterName = "" // empty / typo'd
	cfg.Redis.SentinelAddrs = []string{"10.0.0.1:26379"}

	log, err := logger.NewLogger(cfg)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	client, err := NewClient(cfg, log)
	if err == nil {
		if client != nil {
			_ = client.Close()
		}
		t.Fatal("expected an error for sentinel addrs without master name, got nil")
	}
}
