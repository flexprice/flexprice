package redis

import (
	"crypto/tls"
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

// TestBuildOptions_CredentialSplit locks the two independent credential sets:
// Username/Password auth the data nodes, SentinelUsername/SentinelPassword auth
// the sentinels. Crossing them only fails against a real auth-enabled cluster.
func TestBuildOptions_CredentialSplit(t *testing.T) {
	cfg := config.RedisConfig{
		Host:               "redis.internal",
		Port:               6379,
		Username:           "app-user",
		Password:           "data-pw",
		SentinelMasterName: "mymaster",
		SentinelAddrs:      []string{"s1:26379"},
		SentinelUsername:   "sentinel-user",
		SentinelPassword:   "sentinel-pw",
	}

	opts, mode, err := buildOptions(cfg)
	if err != nil {
		t.Fatalf("buildOptions() unexpected error: %v", err)
	}
	if mode != modeSentinel {
		t.Fatalf("mode = %q, want %q", mode, modeSentinel)
	}
	checks := []struct {
		field, got, want string
	}{
		{"Username", opts.Username, "app-user"},
		{"Password", opts.Password, "data-pw"},
		{"SentinelUsername", opts.SentinelUsername, "sentinel-user"},
		{"SentinelPassword", opts.SentinelPassword, "sentinel-pw"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("opts.%s = %q, want %q", c.field, c.got, c.want)
		}
	}

	// Failover() is what actually reaches go-redis in Sentinel mode.
	fo := opts.Failover()
	if fo.Username != "app-user" || fo.Password != "data-pw" {
		t.Errorf("Failover() data-node creds = %q/%q, want app-user/data-pw", fo.Username, fo.Password)
	}
	if fo.SentinelUsername != "sentinel-user" || fo.SentinelPassword != "sentinel-pw" {
		t.Errorf("Failover() sentinel creds = %q/%q, want sentinel-user/sentinel-pw", fo.SentinelUsername, fo.SentinelPassword)
	}
}

// TestBuildOptions_TLS guards the TLS wiring: skip-verify is configurable (not
// hardcoded), ServerName is honoured, and MinVersion is TLS 1.2.
func TestBuildOptions_TLS(t *testing.T) {
	t.Run("no TLS -> nil config", func(t *testing.T) {
		opts, _, err := buildOptions(config.RedisConfig{Host: "r", Port: 6379})
		if err != nil {
			t.Fatalf("buildOptions() error: %v", err)
		}
		if opts.TLSConfig != nil {
			t.Fatalf("TLSConfig = %v, want nil when UseTLS is false", opts.TLSConfig)
		}
	})

	t.Run("verify opt-in wires ServerName", func(t *testing.T) {
		opts, _, err := buildOptions(config.RedisConfig{Host: "r", Port: 6379, UseTLS: true, TLSSkipVerify: false, TLSServerName: "cache.example.com"})
		if err != nil {
			t.Fatalf("buildOptions() error: %v", err)
		}
		if opts.TLSConfig == nil {
			t.Fatal("TLSConfig = nil, want set when UseTLS is true")
		}
		if opts.TLSConfig.InsecureSkipVerify {
			t.Error("InsecureSkipVerify = true, want false when TLSSkipVerify is false")
		}
		if opts.TLSConfig.ServerName != "cache.example.com" {
			t.Errorf("ServerName = %q, want cache.example.com", opts.TLSConfig.ServerName)
		}
		if opts.TLSConfig.MinVersion != tls.VersionTLS12 {
			t.Errorf("MinVersion = %d, want TLS 1.2", opts.TLSConfig.MinVersion)
		}
	})

	t.Run("skip-verify honoured", func(t *testing.T) {
		opts, _, err := buildOptions(config.RedisConfig{Host: "r", Port: 6379, UseTLS: true, TLSSkipVerify: true})
		if err != nil {
			t.Fatalf("buildOptions() error: %v", err)
		}
		if !opts.TLSConfig.InsecureSkipVerify {
			t.Error("InsecureSkipVerify = false, want true when TLSSkipVerify is set")
		}
	})
}

// TestBuildOptions_UsernameOptional guards backward compatibility: an unset
// Username must stay empty so go-redis keeps using plain AUTH <password>.
func TestBuildOptions_UsernameOptional(t *testing.T) {
	modes := []struct {
		name string
		cfg  config.RedisConfig
	}{
		{"standalone", config.RedisConfig{Host: "h", Port: 6379, Password: "pw"}},
		{"cluster", config.RedisConfig{Host: "h", Port: 6379, Password: "pw", ClusterMode: true}},
		{"sentinel", config.RedisConfig{Password: "pw", SentinelMasterName: "m", SentinelAddrs: []string{"s1:26379"}}},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			opts, _, err := buildOptions(m.cfg)
			if err != nil {
				t.Fatalf("buildOptions() unexpected error: %v", err)
			}
			if opts.Username != "" {
				t.Errorf("opts.Username = %q, want empty (requirepass-style auth)", opts.Username)
			}
			if opts.Password != "pw" {
				t.Errorf("opts.Password = %q, want %q", opts.Password, "pw")
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
