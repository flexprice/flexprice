package cache

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/usagerollup"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestNewUsageRollupKeys(t *testing.T) {
	hour := time.Date(2026, 11, 18, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		keyPrefix  string
		bucket     usagerollup.Bucket
		wantQty    string
		wantMax    string
		wantActive string
		wantField  string
	}{
		{
			name:       "sum without a key prefix",
			bucket:     usagerollup.Bucket{ExternalCustomerID: "cust_1", Hour: hour, MeterID: "meter_a", Aggregation: types.AggregationSum},
			wantQty:    "qty:{tenant_1:env_1:cust_1}:2026111813",
			wantMax:    "max:{tenant_1:env_1:cust_1}:2026111813",
			wantActive: "active:{tenant_1:env_1}:2026111813",
			wantField:  "sum:meter_a",
		},
		{
			name:       "count with a key prefix and a non-UTC hour",
			keyPrefix:  "fp",
			bucket:     usagerollup.Bucket{ExternalCustomerID: "cust_1", Hour: hour.In(time.FixedZone("IST", 5*3600+1800)), MeterID: "meter_b", Aggregation: types.AggregationCount},
			wantQty:    "fp:qty:{tenant_1:env_1:cust_1}:2026111813",
			wantMax:    "fp:max:{tenant_1:env_1:cust_1}:2026111813",
			wantActive: "fp:active:{tenant_1:env_1}:2026111813",
			wantField:  "count:meter_b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := newUsageRollupKeys(tt.keyPrefix, "tenant_1", "env_1", tt.bucket)
			require.Equal(t, tt.wantQty, keys.qty)
			require.Equal(t, tt.wantMax, keys.max)
			require.Equal(t, tt.wantActive, keys.active)
			require.Equal(t, tt.wantField, keys.field)
		})
	}
}

func TestNewUsageRollupStoreWithoutRedis(t *testing.T) {
	tests := []struct {
		name       string
		rollup     config.UsageRollupConfig
		singleDup  bool
		bulkDedup  bool
		wantErrMsg string
	}{
		{name: "disabled returns no store, whatever dedup is"},
		{name: "enabled for all tenants without Redis fails", rollup: config.UsageRollupConfig{Enabled: true}, singleDup: true, bulkDedup: true, wantErrMsg: "Redis is not available"},
		{name: "enabled for listed tenants without Redis fails", rollup: config.UsageRollupConfig{EnabledTenants: []string{"tenant_1"}}, singleDup: true, bulkDedup: true, wantErrMsg: "Redis is not available"},
		{name: "enabled with single-event dedup off fails", rollup: config.UsageRollupConfig{Enabled: true}, bulkDedup: true, wantErrMsg: "meter_usage_tracking.redis_deduplication_enabled=true"},
		{name: "enabled with bulk dedup off fails", rollup: config.UsageRollupConfig{Enabled: true}, singleDup: true, wantErrMsg: "bulk_meter_usage_tracking.redis_deduplication_enabled=true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Configuration{
				UsageRollup:            tt.rollup,
				MeterUsageTracking:     config.MeterUsageTrackingConfig{RedisDeduplicationEnabled: tt.singleDup},
				BulkMeterUsageTracking: config.BulkMeterUsageTrackingConfig{RedisDeduplicationEnabled: tt.bulkDedup},
			}
			store, err := NewUsageRollupStore(cfg, nil, logger.NewNoopLogger())
			require.Nil(t, store)
			if tt.wantErrMsg != "" {
				require.ErrorContains(t, err, tt.wantErrMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestActiveSetOutlivesLastRead(t *testing.T) {
	hour := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	lastCutoffReadingHour := hour.Add(usagerollup.LateWindow)
	lastRunReadingHour := lastCutoffReadingHour.Add(usagerollup.LateWindow + usagerollup.CutoffGrace)
	require.True(t, usagerollup.Cutoff(lastRunReadingHour).Equal(lastCutoffReadingHour))
	require.True(t, hour.Add(usagerollup.ActiveSetTTL).After(lastRunReadingHour),
		"a set written at the start of its hour must still exist when the last run reads it")
}
