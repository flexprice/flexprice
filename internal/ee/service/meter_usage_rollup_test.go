package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/usagerollup"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type fakeHourlyUsageRepository struct {
	calls  int
	usage  []usagerollup.HourlyUsage
	err    error
	active map[time.Time][]string
	hourly map[time.Time][]usagerollup.HourlyUsage
}

func (f *fakeHourlyUsageRepository) ListCustomerHourlyUsage(_ context.Context, _ string, hours []time.Time) ([]usagerollup.HourlyUsage, error) {
	var usage []usagerollup.HourlyUsage
	for _, hour := range hours {
		usage = append(usage, f.hourly[hour]...)
	}
	return usage, f.err
}

func (f *fakeHourlyUsageRepository) ListActiveCustomers(_ context.Context, hour time.Time) ([]string, error) {
	return f.active[hour], f.err
}

func (f *fakeHourlyUsageRepository) Increment(_ context.Context, usage []usagerollup.HourlyUsage) error {
	f.calls++
	f.usage = append(f.usage, usage...)
	return f.err
}

// total is the summed quantity across usage for one meter.
func (f *fakeHourlyUsageRepository) total(meterID string) decimal.Decimal {
	total := decimal.Zero
	for _, b := range f.usage {
		if b.MeterID == meterID {
			total = total.Add(b.Quantity)
		}
	}
	return total
}

func usageRollupCtx() context.Context {
	return types.SetEnvironmentID(context.WithValue(context.Background(), types.CtxTenantID, "tenant_on"), "env_1")
}

func TestRecordUsageRollup(t *testing.T) {
	now := time.Now().UTC()
	cutoff := usagerollup.Cutoff(now)
	sumMeter := &meter.Meter{ID: "mtr_sum", Aggregation: meter.Aggregation{Type: types.AggregationSum}}
	countMeter := &meter.Meter{ID: "mtr_count", Aggregation: meter.Aggregation{Type: types.AggregationCount}}
	uniqueMeter := &meter.Meter{ID: "mtr_unique", Aggregation: meter.Aggregation{Type: types.AggregationCountUnique}}
	metersByID := map[string]*meter.Meter{sumMeter.ID: sumMeter, countMeter.ID: countMeter, uniqueMeter.ID: uniqueMeter}

	usage := func(meterID string, ts time.Time, qty int64) *events.MeterUsage {
		return &events.MeterUsage{
			Event:    events.Event{ExternalCustomerID: "cust_1", Timestamp: ts},
			MeterID:  meterID,
			QtyTotal: decimal.NewFromInt(qty),
		}
	}

	tests := []struct {
		name        string
		usageRollup config.UsageRollupConfig
		records     []*events.MeterUsage
		storeErr    error
		wantCalls   int
		wantSum     string
		wantCount   string
	}{
		{
			name:        "enabled tenant writes supported meters only",
			usageRollup: config.UsageRollupConfig{Enabled: true, EnabledTenants: []string{"tenant_on"}},
			records:     []*events.MeterUsage{usage("mtr_sum", now, 2), usage("mtr_sum", now, 3), usage("mtr_unique", now, 1)},
			wantCalls:   1,
			wantSum:     "5",
		},
		{
			name:        "shadow tenant writes like an enabled one",
			usageRollup: config.UsageRollupConfig{Enabled: true, ShadowTenants: []string{"tenant_on"}},
			records:     []*events.MeterUsage{usage("mtr_sum", now, 2)},
			wantCalls:   1,
			wantSum:     "2",
		},
		{
			name:        "tenant not enabled writes nothing",
			usageRollup: config.UsageRollupConfig{Enabled: true, EnabledTenants: []string{"tenant_other"}},
			records:     []*events.MeterUsage{usage("mtr_sum", now, 2)},
			wantSum:     "0",
		},
		{
			name:        "usage older than the cutoff skips Redis",
			usageRollup: config.UsageRollupConfig{Enabled: true, EnabledTenants: []string{"tenant_on"}},
			records:     []*events.MeterUsage{usage("mtr_sum", now.Add(-5*time.Hour), 2)},
			wantSum:     "0",
		},
		{
			name:        "count is one per event, whatever the quantity",
			usageRollup: config.UsageRollupConfig{Enabled: true, EnabledTenants: []string{"tenant_on"}},
			records:     []*events.MeterUsage{usage("mtr_count", now, 7), usage("mtr_count", now, 0)},
			wantCalls:   1,
			wantSum:     "0",
			wantCount:   "2",
		},
		{
			name:        "the cutoff hour is kept, usage just before it is dropped",
			usageRollup: config.UsageRollupConfig{Enabled: true, EnabledTenants: []string{"tenant_on"}},
			records:     []*events.MeterUsage{usage("mtr_sum", cutoff, 2), usage("mtr_sum", cutoff.Add(-time.Second), 3)},
			wantCalls:   1,
			wantSum:     "2",
		},
		{
			name:        "rows without a customer are skipped",
			usageRollup: config.UsageRollupConfig{Enabled: true, EnabledTenants: []string{"tenant_on"}},
			records: []*events.MeterUsage{{
				Event: events.Event{Timestamp: now}, MeterID: "mtr_sum", QtyTotal: decimal.NewFromInt(2),
			}},
			wantSum: "0",
		},
		{
			name:        "store error is swallowed",
			usageRollup: config.UsageRollupConfig{Enabled: true, EnabledTenants: []string{"tenant_on"}},
			records:     []*events.MeterUsage{usage("mtr_sum", now, 2)},
			storeErr:    errors.New("redis down"),
			wantCalls:   1,
			wantSum:     "2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeHourlyUsageRepository{err: tt.storeErr}
			svc := &meterUsageTrackingService{
				ServiceParams: ServiceParams{
					Logger:          logger.NewNoopLogger(),
					Config:          &config.Configuration{UsageRollup: tt.usageRollup},
					HourlyUsageRepo: store,
				},
			}

			svc.recordUsageRollup(usageRollupCtx(), tt.records, metersByID)

			require.Equal(t, tt.wantCalls, store.calls)
			require.True(t, decimal.RequireFromString(tt.wantSum).Equal(store.total("mtr_sum")), "got %s", store.total("mtr_sum"))
			wantCount := tt.wantCount
			if wantCount == "" {
				wantCount = "0"
			}
			require.True(t, decimal.RequireFromString(wantCount).Equal(store.total("mtr_count")), "got %s", store.total("mtr_count"))
		})
	}
}

func TestRecordUsageRollupWithoutStore(t *testing.T) {
	svc := &meterUsageTrackingService{ServiceParams: ServiceParams{
		Logger: logger.NewNoopLogger(),
		Config: &config.Configuration{UsageRollup: config.UsageRollupConfig{Enabled: true, EnabledTenants: []string{"tenant_on"}}},
	}}
	require.NotPanics(t, func() {
		svc.recordUsageRollup(usageRollupCtx(), []*events.MeterUsage{{MeterID: "mtr_sum"}}, nil)
	})
}

// newUsageRollupConsumer wires a meter usage consumer with one SUM meter on "api_call",
// in-memory stores and locker, and a fake usage rollup store.
func newUsageRollupConsumer(t *testing.T, dedupEnabled bool) (*meterUsageTrackingService, *fakeHourlyUsageRepository) {
	t.Helper()
	meters := testutil.NewInMemoryMeterStore()
	require.NoError(t, meters.CreateMeter(usageRollupCtx(), &meter.Meter{
		ID:          "mtr_sum",
		EventName:   "api_call",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "tokens"},
		BaseModel:   types.BaseModel{TenantID: "tenant_on", Status: types.StatusPublished},
	}))
	store := &fakeHourlyUsageRepository{}
	svc := &meterUsageTrackingService{
		ServiceParams: ServiceParams{
			Logger:       logger.NewNoopLogger(),
			MeterRepo:    meters,
			CustomerRepo: testutil.NewInMemoryCustomerStore(),
			SettingsRepo: testutil.NewInMemorySettingsStore(),
			Locker:       testutil.NewInMemoryRedisLocker(testutil.NewInMemoryRedis().(*testutil.InMemoryRedis)),
			Config: &config.Configuration{
				UsageRollup:            config.UsageRollupConfig{Enabled: true, EnabledTenants: []string{"tenant_on"}},
				MeterUsageTracking:     config.MeterUsageTrackingConfig{RedisDeduplicationEnabled: dedupEnabled},
				BulkMeterUsageTracking: config.BulkMeterUsageTrackingConfig{RedisDeduplicationEnabled: dedupEnabled},
			},
			HourlyUsageRepo: store,
		},
		meterUsageRepo: testutil.NewInMemoryMeterUsageStore(),
	}
	return svc, store
}

func usageRollupEvent(id string, tokens float64, forceReprocess bool) *events.Event {
	return &events.Event{
		ID:                 id,
		TenantID:           "tenant_on",
		EnvironmentID:      "env_1",
		EventName:          "api_call",
		ExternalCustomerID: "cust_1",
		Timestamp:          time.Now().UTC(),
		Properties:         map[string]interface{}{"tokens": tokens},
		ForceReprocess:     forceReprocess,
	}
}

func TestBulkConsumerUsageRollup(t *testing.T) {
	tests := []struct {
		name         string
		dedupEnabled bool
		batches      [][]*events.Event
		wantTotal    string
	}{
		{
			name:         "a redelivery the dedup lock catches is not counted again",
			dedupEnabled: true,
			batches: [][]*events.Event{
				{usageRollupEvent("e1", 2, false), usageRollupEvent("e2", 3, false)},
				{usageRollupEvent("e1", 2, false), usageRollupEvent("e2", 3, false)},
			},
			wantTotal: "5",
		},
		{
			name:         "the same ID twice in one batch is counted once",
			dedupEnabled: true,
			batches:      [][]*events.Event{{usageRollupEvent("e1", 7, false), usageRollupEvent("e1", 7, false)}},
			wantTotal:    "7",
		},
		{
			name:         "dedup off still rolls up",
			dedupEnabled: false,
			batches:      [][]*events.Event{{usageRollupEvent("e1", 2, false)}},
			wantTotal:    "2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store := newUsageRollupConsumer(t, tt.dedupEnabled)
			for _, batch := range tt.batches {
				msg := scopedMsg(t, events.EventBatch{TenantID: "tenant_on", EnvironmentID: "env_1", Events: batch})
				require.NoError(t, svc.processBulkMessage(usageRollupCtx(), msg))
			}
			require.True(t, decimal.RequireFromString(tt.wantTotal).Equal(store.total("mtr_sum")), "got %s", store.total("mtr_sum"))
		})
	}
}

func TestSingleConsumerUsageRollup(t *testing.T) {
	tests := []struct {
		name         string
		dedupEnabled bool
		events       []*events.Event
		wantTotal    string
	}{
		{
			name:         "a redelivery the dedup lock catches is not counted again",
			dedupEnabled: true,
			events:       []*events.Event{usageRollupEvent("e1", 2, false), usageRollupEvent("e1", 2, false)},
			wantTotal:    "2",
		},
		{
			name:         "force reprocess takes no lock and is still rolled up",
			dedupEnabled: true,
			events:       []*events.Event{usageRollupEvent("e1", 2, true)},
			wantTotal:    "2",
		},
		{
			name:         "dedup off still rolls up",
			dedupEnabled: false,
			events:       []*events.Event{usageRollupEvent("e1", 2, false)},
			wantTotal:    "2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store := newUsageRollupConsumer(t, tt.dedupEnabled)
			for _, evt := range tt.events {
				require.NoError(t, svc.processEvent(usageRollupCtx(), evt))
			}
			require.True(t, decimal.RequireFromString(tt.wantTotal).Equal(store.total("mtr_sum")), "got %s", store.total("mtr_sum"))
		})
	}
}
