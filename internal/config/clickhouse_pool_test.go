package config

import (
	"testing"
	"time"
)

// TestClickHousePoolFromEnv guards the regression this fix closes: before
// max_open_conns/max_idle_conns existed on ClickHouseConfig,
// FLEXPRICE_CLICKHOUSE_MAX_OPEN_CONNS was a silent no-op and every process ran on
// clickhouse-go's defaults (idle 5, open idle+5 = 10) — a hidden 10-query cap that
// bottlenecks event ingest and surfaces only as client-side "acquire conn timeout".
func TestClickHousePoolFromEnv(t *testing.T) {
	t.Setenv("FLEXPRICE_CLICKHOUSE_MAX_OPEN_CONNS", "100")
	t.Setenv("FLEXPRICE_CLICKHOUSE_MAX_IDLE_CONNS", "20")
	t.Setenv("FLEXPRICE_CLICKHOUSE_DIAL_TIMEOUT", "3s")
	t.Setenv("FLEXPRICE_CLICKHOUSE_READ_TIMEOUT", "45s")

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() error: %v", err)
	}

	if cfg.ClickHouse.MaxOpenConns != 100 {
		t.Errorf("clickhouse.max_open_conns = %d, want 100 (env override not honored)", cfg.ClickHouse.MaxOpenConns)
	}
	if cfg.ClickHouse.MaxIdleConns != 20 {
		t.Errorf("clickhouse.max_idle_conns = %d, want 20 (env override not honored)", cfg.ClickHouse.MaxIdleConns)
	}

	// The value must survive the hop into the driver options, not just land on the struct.
	opts := cfg.ClickHouse.GetClientOptions()
	if opts.MaxOpenConns != 100 {
		t.Errorf("options.MaxOpenConns = %d, want 100 (config not passed to clickhouse-go)", opts.MaxOpenConns)
	}
	if opts.MaxIdleConns != 20 {
		t.Errorf("options.MaxIdleConns = %d, want 20 (config not passed to clickhouse-go)", opts.MaxIdleConns)
	}
	if opts.DialTimeout != 3*time.Second {
		t.Errorf("options.DialTimeout = %v, want 3s", opts.DialTimeout)
	}
	if opts.ReadTimeout != 45*time.Second {
		t.Errorf("options.ReadTimeout = %v, want 45s", opts.ReadTimeout)
	}
}

// TestClickHousePoolDefaults pins the unset behaviour: zero pool values are passed through
// so clickhouse-go applies its own defaults, while the dial/read deadlines keep the finite
// values that make an in-order dial fail over instead of hanging on a dead PrivateLink ENI.
func TestClickHousePoolDefaults(t *testing.T) {
	c := ClickHouseConfig{Address: "localhost:9000", Database: "flexprice"}
	opts := c.GetClientOptions()

	if opts.MaxOpenConns != 0 || opts.MaxIdleConns != 0 {
		t.Errorf("unset pool = open %d/idle %d, want 0/0 so the driver applies its defaults",
			opts.MaxOpenConns, opts.MaxIdleConns)
	}
	if opts.DialTimeout != defaultClickHouseDialTimeout {
		t.Errorf("options.DialTimeout = %v, want %v", opts.DialTimeout, defaultClickHouseDialTimeout)
	}
	if opts.ReadTimeout != defaultClickHouseReadTimeout {
		t.Errorf("options.ReadTimeout = %v, want %v", opts.ReadTimeout, defaultClickHouseReadTimeout)
	}
}
