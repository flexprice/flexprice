package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type factoryRow struct{ scan func(dest ...any) error }

func (r factoryRow) Err() error                { return nil }
func (r factoryRow) Scan(dest ...any) error    { return r.scan(dest...) }
func (r factoryRow) ScanStruct(dest any) error { return nil }

type factoryConn struct {
	driver.Conn
	existing   map[string]bool
	failExists bool
	queries    []string
}

func (c *factoryConn) QueryRow(_ context.Context, query string, _ ...any) driver.Row {
	c.queries = append(c.queries, query)
	return factoryRow{scan: func(dest ...any) error {
		if table, ok := strings.CutPrefix(query, "EXISTS TABLE "); ok {
			if c.failExists {
				return errors.New("down")
			}
			if c.existing[table] {
				*dest[0].(*uint8) = 1
			}
			return nil
		}
		*dest[0].(*decimal.Decimal) = decimal.NewFromInt(1)
		*dest[1].(*uint64) = 1
		return nil
	}}
}

func observed() (*logger.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return logger.NewFromSugared(zap.New(core).Sugar()), logs
}

func usageQuery(t *testing.T, conn *factoryConn, log *logger.Logger, cfg *config.Configuration) string {
	repo := NewMeterUsageRepository(RepositoryParams{ClickHouseDB: clickhouse.NewClickHouseStoreFromConn(conn), Logger: log}, cfg)
	start := time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)
	conn.queries = nil
	_, err := repo.GetUsage(context.Background(), &events.MeterUsageQueryParams{
		TenantID: "t", EnvironmentID: "e", ExternalCustomerID: "c", MeterID: "m",
		AggregationType: types.AggregationSum, StartTime: start, EndTime: start.Add(52 * time.Hour),
	})
	require.NoError(t, err)
	require.Len(t, conn.queries, 1)
	return conn.queries[0]
}

func rollupCfg(enabled bool, hourly, daily string) *config.Configuration {
	return &config.Configuration{MeterUsage: config.MeterUsageConfig{Rollup: config.MeterUsageRollupConfig{Enabled: enabled, Table: hourly, DailyTable: daily}}}
}

func TestNewMeterUsageRepositoryRollupWiring(t *testing.T) {
	present := map[string]bool{"meter_usage_hourly": true, "meter_usage_daily": true}

	tests := []struct {
		name        string
		cfg         *config.Configuration
		existing    map[string]bool
		failExists  bool
		wantHourly  bool
		wantDaily   bool
		wantWarning bool
	}{
		{"nil config", nil, present, false, false, false, false},
		{"flag off", rollupCfg(false, "meter_usage_hourly", ""), present, false, false, false, false},
		{"hourly only", rollupCfg(true, "meter_usage_hourly", ""), present, false, true, false, false},
		{"hourly and daily", rollupCfg(true, "meter_usage_hourly", "meter_usage_daily"), present, false, true, true, false},
		{"hourly missing", rollupCfg(true, "meter_usage_hourly", "meter_usage_daily"), map[string]bool{"meter_usage_daily": true}, false, false, false, true},
		{"daily missing", rollupCfg(true, "meter_usage_hourly", "meter_usage_daily"), map[string]bool{"meter_usage_hourly": true}, false, true, false, true},
		{"check fails", rollupCfg(true, "meter_usage_hourly", "meter_usage_daily"), present, true, false, false, true},
		{"invalid hourly name", rollupCfg(true, "x; DROP", ""), present, false, false, false, true},
		{"invalid daily name", rollupCfg(true, "meter_usage_hourly", "y; DROP"), present, false, true, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &factoryConn{existing: tt.existing, failExists: tt.failExists}
			log, logs := observed()
			q := usageQuery(t, conn, log, tt.cfg)
			assert.Equal(t, tt.wantHourly, strings.Contains(q, "meter_usage_hourly"))
			assert.Equal(t, tt.wantDaily, strings.Contains(q, "meter_usage_daily"))
			assert.Equal(t, tt.wantWarning, logs.FilterLevelExact(zapcore.WarnLevel).Len() > 0)
		})
	}
}

func TestNewMeterUsageRepositoryNilStoreDisablesRollup(t *testing.T) {
	log, logs := observed()
	repo := NewMeterUsageRepository(RepositoryParams{Logger: log}, rollupCfg(true, "meter_usage_hourly", ""))
	require.NotNil(t, repo)
	assert.Equal(t, 1, logs.FilterLevelExact(zapcore.WarnLevel).Len())
}
