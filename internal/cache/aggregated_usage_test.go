package cache

import (
	"context"
	"github.com/redis/go-redis/v9"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/aggregatedusage"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestNewAggregatedUsageKeys(t *testing.T) {
	hour := time.Date(2026, 11, 18, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		keyPrefix  string
		usage      aggregatedusage.HourlyUsage
		wantQty    string
		wantMax    string
		wantActive string
		wantField  string
	}{
		{
			name:       "sum without a key prefix",
			usage:      aggregatedusage.HourlyUsage{ExternalCustomerID: "cust_1", Hour: hour, MeterID: "meter_a", Aggregation: types.AggregationSum},
			wantQty:    "qty:{tenant_1:env_1:cust_1}:2026111813",
			wantMax:    "max:{tenant_1:env_1:cust_1}:2026111813",
			wantActive: "active:{tenant_1:env_1}:2026111813",
			wantField:  "sum:meter_a",
		},
		{
			name:       "count with a key prefix and a non-UTC hour",
			keyPrefix:  "fp",
			usage:      aggregatedusage.HourlyUsage{ExternalCustomerID: "cust_1", Hour: hour.In(time.FixedZone("IST", 5*3600+1800)), MeterID: "meter_b", Aggregation: types.AggregationCount},
			wantQty:    "fp:qty:{tenant_1:env_1:cust_1}:2026111813",
			wantMax:    "fp:max:{tenant_1:env_1:cust_1}:2026111813",
			wantActive: "fp:active:{tenant_1:env_1}:2026111813",
			wantField:  "count:meter_b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := newAggregatedUsageKeys(tt.keyPrefix, "tenant_1", "env_1", tt.usage)
			require.Equal(t, tt.wantQty, keys.qty)
			require.Equal(t, tt.wantMax, keys.max)
			require.Equal(t, tt.wantActive, keys.active)
			require.Equal(t, tt.wantField, keys.field)
		})
	}
}

func TestNewHourlyUsageRepositoryWithoutRedis(t *testing.T) {
	tests := []struct {
		name            string
		aggregatedUsage config.AggregatedUsageConfig
		singleDup       bool
		bulkDedup       bool
		wantErrMsg      string
	}{
		{name: "disabled returns no store, whatever dedup is"},
		{name: "enabled without tenants returns no store", aggregatedUsage: config.AggregatedUsageConfig{Enabled: true}, singleDup: true, bulkDedup: true},
		{name: "enabled for listed tenants without Redis fails", aggregatedUsage: config.AggregatedUsageConfig{Enabled: true, EnabledTenants: []string{"tenant_1"}}, singleDup: true, bulkDedup: true, wantErrMsg: "Redis is not available"},
		{name: "enabled with single-event dedup off fails", aggregatedUsage: config.AggregatedUsageConfig{Enabled: true, EnabledTenants: []string{"tenant_1"}}, bulkDedup: true, wantErrMsg: "meter_usage_tracking.redis_deduplication_enabled=true"},
		{name: "enabled with bulk dedup off fails", aggregatedUsage: config.AggregatedUsageConfig{Enabled: true, EnabledTenants: []string{"tenant_1"}}, singleDup: true, wantErrMsg: "bulk_meter_usage_tracking.redis_deduplication_enabled=true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Configuration{
				AggregatedUsage:        tt.aggregatedUsage,
				MeterUsageTracking:     config.MeterUsageTrackingConfig{RedisDeduplicationEnabled: tt.singleDup},
				BulkMeterUsageTracking: config.BulkMeterUsageTrackingConfig{RedisDeduplicationEnabled: tt.bulkDedup},
			}
			store, err := NewHourlyUsageRepository(cfg, nil, logger.NewNoopLogger())
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
	run := hour.Add(time.Hour + aggregatedusage.LateWindow + aggregatedusage.CutoffGrace)
	require.True(t, aggregatedusage.Cutoff(run).Equal(hour.Add(time.Hour)), "this run checkpoints the hour")
	require.True(t, hour.Add(activeSetTTL).After(run.Add(30*time.Minute)),
		"a set written at the start of its hour must outlive the run that reads it, with room for a retry")
}

func TestMergeHourlyUsage(t *testing.T) {
	hour := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	at := func(hours int) time.Time { return hour.Add(time.Duration(hours) * time.Hour) }
	d := decimal.RequireFromString
	usage := func(customer string, ts time.Time, meterID string, aggregation types.AggregationType, qty string) aggregatedusage.HourlyUsage {
		return aggregatedusage.HourlyUsage{ExternalCustomerID: customer, Hour: ts, MeterID: meterID, Aggregation: aggregation, Quantity: d(qty)}
	}
	tests := []struct {
		name string
		in   []aggregatedusage.HourlyUsage
		want []aggregatedusage.HourlyUsage
	}{
		{
			name: "sum adds within an hour and keeps hours apart",
			in: []aggregatedusage.HourlyUsage{
				usage("c1", at(0), "m1", types.AggregationSum, "2.5"),
				usage("c1", at(0), "m1", types.AggregationSum, "-0.5"),
				usage("c1", at(1), "m1", types.AggregationSum, "4"),
			},
			want: []aggregatedusage.HourlyUsage{
				usage("c1", at(0), "m1", types.AggregationSum, "2"),
				usage("c1", at(1), "m1", types.AggregationSum, "4"),
			},
		},
		{
			name: "count adds the event counts",
			in: []aggregatedusage.HourlyUsage{
				usage("c1", at(0), "m2", types.AggregationCount, "1"),
				usage("c1", at(0), "m2", types.AggregationCount, "1"),
			},
			want: []aggregatedusage.HourlyUsage{usage("c1", at(0), "m2", types.AggregationCount, "2")},
		},
		{
			name: "max keeps the highest value, including when all are negative",
			in: []aggregatedusage.HourlyUsage{
				usage("c1", at(0), "m3", types.AggregationMax, "-5"),
				usage("c1", at(0), "m3", types.AggregationMax, "-2"),
				usage("c1", at(0), "m3", types.AggregationMax, "-9"),
			},
			want: []aggregatedusage.HourlyUsage{usage("c1", at(0), "m3", types.AggregationMax, "-2")},
		},
		{
			name: "customers and meters stay separate",
			in: []aggregatedusage.HourlyUsage{
				usage("c1", at(0), "m1", types.AggregationSum, "1"),
				usage("c2", at(0), "m1", types.AggregationSum, "1"),
				usage("c1", at(0), "m4", types.AggregationSum, "1"),
			},
			want: []aggregatedusage.HourlyUsage{
				usage("c1", at(0), "m1", types.AggregationSum, "1"),
				usage("c2", at(0), "m1", types.AggregationSum, "1"),
				usage("c1", at(0), "m4", types.AggregationSum, "1"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeHourlyUsage(tt.in)
			require.Len(t, got, len(tt.want))
			for i := range tt.want {
				require.Equal(t, tt.want[i].ExternalCustomerID, got[i].ExternalCustomerID)
				require.True(t, tt.want[i].Hour.Equal(got[i].Hour), "hour %s, want %s", got[i].Hour, tt.want[i].Hour)
				require.Equal(t, tt.want[i].MeterID, got[i].MeterID)
				require.Equal(t, tt.want[i].Aggregation, got[i].Aggregation)
				require.True(t, tt.want[i].Quantity.Equal(got[i].Quantity), "quantity %s, want %s", got[i].Quantity, tt.want[i].Quantity)
			}
		})
	}
}

// TestHourlyUsageRepositoryRoundTrip writes usage and reads them back from a real Redis on localhost:6379.
func TestHourlyUsageRepositoryRoundTrip(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("skipping: Redis not reachable at localhost:6379 (%v)", err)
	}
	store := &hourlyUsageRepository{client: client, keyPrefix: "roundtrip_" + types.GenerateUUID()}
	ctx := types.SetEnvironmentID(context.WithValue(context.Background(), types.CtxTenantID, "tenant_rt"), "env_rt")
	hour := time.Now().UTC().Truncate(time.Hour)
	usage := func(meterID string, aggregation types.AggregationType, qty string) aggregatedusage.HourlyUsage {
		return aggregatedusage.HourlyUsage{ExternalCustomerID: "cust_rt", Hour: hour, MeterID: meterID, Aggregation: aggregation, Quantity: decimal.RequireFromString(qty)}
	}
	require.NoError(t, store.Increment(ctx, []aggregatedusage.HourlyUsage{
		usage("m_sum", types.AggregationSum, "0.1"), usage("m_count", types.AggregationCount, "3"), usage("m_max", types.AggregationMax, "-2"),
	}))
	require.NoError(t, store.Increment(ctx, []aggregatedusage.HourlyUsage{
		usage("m_sum", types.AggregationSum, "0.2"), usage("m_max", types.AggregationMax, "-5"),
	}))

	got, err := store.ListForCustomer(ctx, "cust_rt", []time.Time{hour, hour.Add(time.Hour)})
	require.NoError(t, err)
	byMeter := map[string]aggregatedusage.HourlyUsage{}
	for _, b := range got {
		byMeter[b.MeterID] = b
	}
	require.Len(t, byMeter, 3)
	require.Equal(t, "0.3", byMeter["m_sum"].Quantity.String(), "float drift is rounded off on read")
	require.Equal(t, types.AggregationCount, byMeter["m_count"].Aggregation)
	require.Equal(t, "3", byMeter["m_count"].Quantity.String())
	require.Equal(t, "-2", byMeter["m_max"].Quantity.String())
}
