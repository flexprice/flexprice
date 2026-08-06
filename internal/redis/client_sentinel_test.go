package redis

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
)

// sentinelCfgFromEnv builds a Sentinel client config from env vars so the same
// tests exercise both the plain rig and the auth+TLS rig:
//
//	REDIS_SENTINEL_ADDRS       comma-separated sentinel endpoints (required)
//	REDIS_SENTINEL_MASTER      master group name (default "mymaster")
//	REDIS_PASSWORD             data-node password (auth rig)
//	REDIS_SENTINEL_PASSWORD    password to auth to the sentinels (auth rig)
//	REDIS_USE_TLS=1            connect over TLS (tls rig)
func sentinelCfgFromEnv() *config.Configuration {
	addrs := os.Getenv("REDIS_SENTINEL_ADDRS")
	if addrs == "" {
		addrs = "127.0.0.1:26379,127.0.0.1:26380,127.0.0.1:26381"
	}
	master := os.Getenv("REDIS_SENTINEL_MASTER")
	if master == "" {
		master = "mymaster"
	}

	cfg := config.GetDefaultConfig()
	cfg.Redis.Timeout = 3 * time.Second
	cfg.Redis.SentinelMasterName = master
	cfg.Redis.SentinelAddrs = strings.Split(addrs, ",")
	cfg.Redis.Password = os.Getenv("REDIS_PASSWORD")
	cfg.Redis.SentinelPassword = os.Getenv("REDIS_SENTINEL_PASSWORD")
	cfg.Redis.UseTLS = os.Getenv("REDIS_USE_TLS") == "1"
	cfg.Redis.RouteReadsToReplicas = os.Getenv("REDIS_SENTINEL_ROUTE_READS") == "1"
	return cfg
}

// TestNewClient_SurvivesFailover is the production scenario: a long-lived client
// holds a connection through a master failure. It connects via Sentinel, then
// hammers SET/GET for RUN_SECONDS while an external driver kills the master
// mid-run (see scripts/redis-sentinel-test/test-survive-failover.sh). It PASSES
// if the client recovers on its own (no restart) — the final ops succeed — and
// reports the longest stall so we can see how long the blip lasted.
//
// Gated behind REDIS_FAILOVER_TEST=1.
func TestNewClient_SurvivesFailover(t *testing.T) {
	if os.Getenv("REDIS_FAILOVER_TEST") != "1" {
		t.Skip("set REDIS_FAILOVER_TEST=1 and run via test-survive-failover.sh")
	}

	runSecs := 45
	if v := os.Getenv("RUN_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			runSecs = n
		}
	}

	cfg := sentinelCfgFromEnv()

	log, err := logger.NewLogger(cfg)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	client, err := NewClient(cfg, log)
	if err != nil {
		t.Fatalf("initial NewClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	rdb := client.GetClient()
	deadline := time.Now().Add(time.Duration(runSecs) * time.Second)

	var ok, fail int
	var curStall, maxStall int
	sawFailure := false
	tick := 0
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		key := "flexprice:failover:probe"
		err := rdb.Set(ctx, key, "v", time.Minute).Err()
		if err == nil {
			_, err = rdb.Get(ctx, key).Result()
		}
		cancel()

		tick++
		if err != nil {
			fail++
			sawFailure = true
			curStall++
			if curStall > maxStall {
				maxStall = curStall
			}
			if fail%3 == 1 {
				t.Logf("tick %d: DOWN (%v)", tick, err)
			}
		} else {
			if curStall > 0 {
				t.Logf("tick %d: RECOVERED after %d failed ticks", tick, curStall)
			}
			ok++
			curStall = 0
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Logf("summary: ok=%d fail=%d longest-stall=%d ticks (~%.1fs) sawFailure=%v",
		ok, fail, maxStall, float64(maxStall)*0.5, sawFailure)

	// Must be healthy at the end (recovered without a restart).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("client did NOT recover — still failing after run: %v", err)
	}
	if ok == 0 {
		t.Fatal("no successful operations at all — check the rig")
	}
	// Prove the final stretch is stable, not a lucky single ping.
	for i := 0; i < 5; i++ {
		if err := rdb.Set(ctx, "flexprice:failover:final", "ok", time.Minute).Err(); err != nil {
			t.Fatalf("post-run stability check failed on op %d: %v", i, err)
		}
	}
	t.Logf("PASS: client survived and recovered on its own (no restart)")
}

// TestNewClient_StandaloneMode is a backward-compat guard: with no Sentinel
// config (the default), NewClient must still connect in standalone mode exactly
// as before. Gated behind REDIS_STANDALONE_TEST=1 (needs a plain Redis).
//
//	docker run -d --name r -p 6379:6379 redis:7-alpine
//	REDIS_STANDALONE_TEST=1 go test -v ./internal/redis -run TestNewClient_StandaloneMode
func TestNewClient_StandaloneMode(t *testing.T) {
	if os.Getenv("REDIS_STANDALONE_TEST") != "1" {
		t.Skip("set REDIS_STANDALONE_TEST=1 (and start a plain redis) to run the standalone backward-compat test")
	}

	host := os.Getenv("REDIS_HOST")
	if host == "" {
		host = "127.0.0.1"
	}

	cfg := config.GetDefaultConfig()
	cfg.Redis.Timeout = 5 * time.Second
	cfg.Redis.Host = host
	cfg.Redis.Port = 6379
	// SentinelMasterName and ClusterMode left at their zero values → standalone.

	log, err := logger.NewLogger(cfg)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	client, err := NewClient(cfg, log)
	if err != nil {
		t.Fatalf("NewClient (standalone) failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("standalone ping failed: %v", err)
	}
	const key, want = "flexprice:standalone:probe", "ok"
	if err := client.GetClient().Set(ctx, key, want, time.Minute).Err(); err != nil {
		t.Fatalf("standalone set failed: %v", err)
	}
	if got, _ := client.GetClient().Get(ctx, key).Result(); got != want {
		t.Fatalf("standalone round-trip mismatch: got %q want %q", got, want)
	}
}

// TestNewClient_SentinelMode exercises the real NewClient Sentinel code path
// against the rig in scripts/redis-sentinel-test. It is gated behind
// REDIS_SENTINEL_TEST=1 so `make test` never requires a running Sentinel quorum.
//
// Run it (from a host that can route to the Sentinel-discovered node IPs — a
// Linux box / CI, or from inside the compose network; see the rig README for the
// Mac networking caveat):
//
//	docker compose -f scripts/redis-sentinel-test/docker-compose.sentinel.yml up -d
//	REDIS_SENTINEL_TEST=1 \
//	  REDIS_SENTINEL_ADDRS=127.0.0.1:26379,127.0.0.1:26380,127.0.0.1:26381 \
//	  REDIS_SENTINEL_MASTER=mymaster \
//	  go test -v -race ./internal/redis -run TestNewClient_SentinelMode
func TestNewClient_SentinelMode(t *testing.T) {
	if os.Getenv("REDIS_SENTINEL_TEST") != "1" {
		t.Skip("set REDIS_SENTINEL_TEST=1 (and start the rig) to run the Sentinel integration test")
	}

	cfg := sentinelCfgFromEnv()

	log, err := logger.NewLogger(cfg)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	client, err := NewClient(cfg, log)
	if err != nil {
		t.Fatalf("NewClient (sentinel) failed — is the rig up and reachable? %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("ping via sentinel-resolved master failed: %v", err)
	}

	const key, want = "flexprice:sentinel:probe", "ok"
	if err := client.GetClient().Set(ctx, key, want, time.Minute).Err(); err != nil {
		t.Fatalf("set via sentinel master failed: %v", err)
	}
	got, err := client.GetClient().Get(ctx, key).Result()
	if err != nil {
		t.Fatalf("get via sentinel failed: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch: got %q want %q", got, want)
	}
}
