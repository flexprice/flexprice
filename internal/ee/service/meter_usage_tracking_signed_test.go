package service

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/expression"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestMeterUsageTrackingSignedSum(t *testing.T) {
	for _, pipeline := range []string{"single", "bulk"} {
		for _, tc := range []struct {
			name        string
			aggregation types.AggregationType
			expression  string
			values      []interface{}
			want        string
		}{
			{name: "positive net", aggregation: types.AggregationSum, values: []interface{}{100, -30}, want: "70"},
			{name: "negative net", aggregation: types.AggregationSum, values: []interface{}{50, -70}, want: "-20"},
			{name: "fractional strings", aggregation: types.AggregationSum, values: []interface{}{"1.25", "-2.5"}, want: "-1.25"},
			{name: "zero net", aggregation: types.AggregationSum, values: []interface{}{30, -30}, want: "0"},
			{name: "negative expression", aggregation: types.AggregationSum, expression: "quantity * 2", values: []interface{}{50, -70}, want: "-40"},
			{name: "max unchanged", aggregation: types.AggregationMax, values: []interface{}{50, -70}, want: "50"},
			{name: "avg unchanged", aggregation: types.AggregationAvg, values: []interface{}{50, -70}, want: "50"},
			{name: "latest unchanged", aggregation: types.AggregationLatest, values: []interface{}{50, -70}, want: "50"},
			{name: "count unchanged", aggregation: types.AggregationCount, values: []interface{}{50, -70}, want: "2"},
		} {
			t.Run(pipeline+"/"+tc.name, func(t *testing.T) {
				ctx := testutil.SetupContext()
				meterStore := testutil.NewInMemoryMeterStore()
				usageStore := testutil.NewInMemoryMeterUsageStore()
				customerStore := testutil.NewInMemoryCustomerStore()
				require.NoError(t, customerStore.Create(ctx, &customer.Customer{
					ID: "cust_signed", ExternalID: "ext_signed", EnvironmentID: types.GetEnvironmentID(ctx),
					BaseModel: types.BaseModel{TenantID: types.GetTenantID(ctx), Status: types.StatusPublished},
				}))
				require.NoError(t, meterStore.CreateMeter(ctx, &meter.Meter{
					ID: "meter_signed", EventName: "storage_changed", ResetUsage: types.ResetUsageBillingPeriod,
					Aggregation: meter.Aggregation{Type: tc.aggregation, Field: "quantity", Expression: tc.expression},
					BaseModel:   types.BaseModel{TenantID: types.GetTenantID(ctx), Status: types.StatusPublished},
				}))
				cfg := &config.Configuration{
					MeterUsageTracking:     config.MeterUsageTrackingConfig{RedisDeduplicationEnabled: true},
					BulkMeterUsageTracking: config.BulkMeterUsageTrackingConfig{RedisDeduplicationEnabled: true},
				}
				svc := &meterUsageTrackingService{
					ServiceParams:  ServiceParams{Config: cfg, Logger: logger.NewNoopLogger(), MeterRepo: meterStore, CustomerRepo: customerStore, Locker: testutil.NewInMemoryRedisLocker(nil)},
					meterUsageRepo: usageStore, expressionEvaluator: expression.NewCELEvaluator(),
				}
				at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
				batch := events.EventBatch{TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx)}
				for i, value := range tc.values {
					batch.Events = append(batch.Events, &events.Event{
						ID: fmt.Sprintf("event_%d", i), TenantID: batch.TenantID, EnvironmentID: batch.EnvironmentID,
						ExternalCustomerID: "ext_signed", EventName: "storage_changed", Timestamp: at.Add(time.Duration(i) * time.Minute),
						Properties: map[string]interface{}{"quantity": value},
					})
				}
				// Replay the same delivery to exercise the consumer's event-ID deduplication.
				for attempt := 0; attempt < 2; attempt++ {
					if pipeline == "single" {
						for _, event := range batch.Events {
							require.NoError(t, svc.processEvent(ctx, event))
						}
					} else {
						payload, err := json.Marshal(batch)
						require.NoError(t, err)
						require.NoError(t, svc.processBulkMessage(ctx, message.NewMessage("batch_signed", payload)))
					}
				}
				// Sum the persisted quantities even for other meter types to verify ingestion clamping.
				got, err := usageStore.GetUsage(ctx, &events.MeterUsageQueryParams{
					TenantID: batch.TenantID, EnvironmentID: batch.EnvironmentID, MeterID: "meter_signed",
					ExternalCustomerID: "ext_signed", StartTime: at, EndTime: at.Add(time.Hour), AggregationType: types.AggregationSum,
				})
				require.NoError(t, err)
				require.True(t, got.TotalValue.Equal(decimal.RequireFromString(tc.want)), "want %s, got %s", tc.want, got.TotalValue)
				require.Equal(t, uint64(2), got.EventCount)
			})
		}
	}
}
