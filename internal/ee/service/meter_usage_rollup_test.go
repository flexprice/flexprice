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

type fakeUsageRollupStore struct {
	calls   int
	buckets []usagerollup.Bucket
	err     error
}

func (f *fakeUsageRollupStore) Increment(_ context.Context, buckets []usagerollup.Bucket) error {
	f.calls++
	f.buckets = append(f.buckets, buckets...)
	return f.err
}

// total is the summed quantity across buckets for one meter.
func (f *fakeUsageRollupStore) total(meterID string) decimal.Decimal {
	total := decimal.Zero
	for _, b := range f.buckets {
		if b.MeterID == meterID {
			total = total.Add(b.Quantity)
		}
	}
	return total
}

func rollupCtx() context.Context {
	return types.SetEnvironmentID(context.WithValue(context.Background(), types.CtxTenantID, "tenant_on"), "env_1")
}

func TestRecordUsageRollup(t *testing.T) {
	now := time.Now().UTC()
	sumMeter := &meter.Meter{ID: "mtr_sum", Aggregation: meter.Aggregation{Type: types.AggregationSum}}
	uniqueMeter := &meter.Meter{ID: "mtr_unique", Aggregation: meter.Aggregation{Type: types.AggregationCountUnique}}
	metersByID := map[string]*meter.Meter{sumMeter.ID: sumMeter, uniqueMeter.ID: uniqueMeter}

	usage := func(meterID string, ts time.Time, qty int64) *events.MeterUsage {
		return &events.MeterUsage{
			Event:    events.Event{ExternalCustomerID: "cust_1", Timestamp: ts},
			MeterID:  meterID,
			QtyTotal: decimal.NewFromInt(qty),
		}
	}

	tests := []struct {
		name      string
		rollup    config.UsageRollupConfig
		records   []*events.MeterUsage
		storeErr  error
		wantCalls int
		wantSum   string
	}{
		{
			name:      "enabled tenant writes supported meters only",
			rollup:    config.UsageRollupConfig{EnabledTenants: []string{"tenant_on"}},
			records:   []*events.MeterUsage{usage("mtr_sum", now, 2), usage("mtr_sum", now, 3), usage("mtr_unique", now, 1)},
			wantCalls: 1,
			wantSum:   "5",
		},
		{
			name:    "tenant not enabled writes nothing",
			rollup:  config.UsageRollupConfig{EnabledTenants: []string{"tenant_other"}},
			records: []*events.MeterUsage{usage("mtr_sum", now, 2)},
			wantSum: "0",
		},
		{
			name:    "usage older than the cutoff skips Redis",
			rollup:  config.UsageRollupConfig{Enabled: true},
			records: []*events.MeterUsage{usage("mtr_sum", now.Add(-5*time.Hour), 2)},
			wantSum: "0",
		},
		{
			name:      "store error is swallowed",
			rollup:    config.UsageRollupConfig{Enabled: true},
			records:   []*events.MeterUsage{usage("mtr_sum", now, 2)},
			storeErr:  errors.New("redis down"),
			wantCalls: 1,
			wantSum:   "2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeUsageRollupStore{err: tt.storeErr}
			svc := &meterUsageTrackingService{
				ServiceParams: ServiceParams{
					Logger:           logger.NewNoopLogger(),
					Config:           &config.Configuration{UsageRollup: tt.rollup},
					UsageRollupStore: store,
				},
			}

			svc.recordUsageRollup(rollupCtx(), tt.records, 0, metersByID)

			require.Equal(t, tt.wantCalls, store.calls)
			require.True(t, decimal.RequireFromString(tt.wantSum).Equal(store.total("mtr_sum")), "got %s", store.total("mtr_sum"))
		})
	}
}

func TestRecordUsageRollupWithoutStore(t *testing.T) {
	svc := &meterUsageTrackingService{ServiceParams: ServiceParams{
		Logger: logger.NewNoopLogger(),
		Config: &config.Configuration{UsageRollup: config.UsageRollupConfig{Enabled: true}},
	}}
	require.NotPanics(t, func() {
		svc.recordUsageRollup(rollupCtx(), []*events.MeterUsage{{MeterID: "mtr_sum"}}, 0, nil)
	})
}

// newRollupConsumer wires a meter usage consumer with one SUM meter on "api_call",
// in-memory stores and locker, and a fake rollup store.
func newRollupConsumer(t *testing.T, dedupEnabled bool) (*meterUsageTrackingService, *fakeUsageRollupStore) {
	t.Helper()
	meters := testutil.NewInMemoryMeterStore()
	require.NoError(t, meters.CreateMeter(rollupCtx(), &meter.Meter{
		ID:          "mtr_sum",
		EventName:   "api_call",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "tokens"},
		BaseModel:   types.BaseModel{TenantID: "tenant_on", Status: types.StatusPublished},
	}))
	store := &fakeUsageRollupStore{}
	svc := &meterUsageTrackingService{
		ServiceParams: ServiceParams{
			Logger:       logger.NewNoopLogger(),
			MeterRepo:    meters,
			CustomerRepo: testutil.NewInMemoryCustomerStore(),
			SettingsRepo: testutil.NewInMemorySettingsStore(),
			Locker:       testutil.NewInMemoryRedisLocker(testutil.NewInMemoryRedis().(*testutil.InMemoryRedis)),
			Config: &config.Configuration{
				UsageRollup:            config.UsageRollupConfig{Enabled: true},
				MeterUsageTracking:     config.MeterUsageTrackingConfig{RedisDeduplicationEnabled: dedupEnabled},
				BulkMeterUsageTracking: config.BulkMeterUsageTrackingConfig{RedisDeduplicationEnabled: dedupEnabled},
			},
			UsageRollupStore: store,
		},
		meterUsageRepo: testutil.NewInMemoryMeterUsageStore(),
	}
	return svc, store
}

func rollupEvent(id string, tokens float64, forceReprocess bool) *events.Event {
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
			name:         "locked events are rolled up once and a redelivery is skipped",
			dedupEnabled: true,
			batches: [][]*events.Event{
				{rollupEvent("e1", 2, false), rollupEvent("e2", 3, false)},
				{rollupEvent("e1", 2, false), rollupEvent("e2", 3, false)},
			},
			wantTotal: "5",
		},
		{
			name:         "the same ID twice in one batch is counted once",
			dedupEnabled: true,
			batches:      [][]*events.Event{{rollupEvent("e1", 7, false), rollupEvent("e1", 7, false)}},
			wantTotal:    "7",
		},
		{
			name:         "dedup off rolls up nothing",
			dedupEnabled: false,
			batches:      [][]*events.Event{{rollupEvent("e1", 2, false)}},
			wantTotal:    "0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store := newRollupConsumer(t, tt.dedupEnabled)
			for _, batch := range tt.batches {
				msg := scopedMsg(t, events.EventBatch{TenantID: "tenant_on", EnvironmentID: "env_1", Events: batch})
				require.NoError(t, svc.processBulkMessage(rollupCtx(), msg))
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
			name:         "locked event is rolled up once and a redelivery is skipped",
			dedupEnabled: true,
			events:       []*events.Event{rollupEvent("e1", 2, false), rollupEvent("e1", 2, false)},
			wantTotal:    "2",
		},
		{
			name:         "force reprocess takes no lock and is not rolled up",
			dedupEnabled: true,
			events:       []*events.Event{rollupEvent("e1", 2, true)},
			wantTotal:    "0",
		},
		{
			name:         "dedup off rolls up nothing",
			dedupEnabled: false,
			events:       []*events.Event{rollupEvent("e1", 2, false)},
			wantTotal:    "0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store := newRollupConsumer(t, tt.dedupEnabled)
			for _, evt := range tt.events {
				require.NoError(t, svc.processEvent(rollupCtx(), evt))
			}
			require.True(t, decimal.RequireFromString(tt.wantTotal).Equal(store.total("mtr_sum")), "got %s", store.total("mtr_sum"))
		})
	}
}
