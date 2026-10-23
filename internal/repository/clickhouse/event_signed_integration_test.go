package clickhouse

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestSignedSumUsageIntegration(t *testing.T) {
	store := newRealClickHouseTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	scope := "signed_sum_test_" + uuid.NewString()
	ctx = types.SetTenantID(ctx, scope)
	ctx = types.SetEnvironmentID(ctx, scope)
	raw := NewEventRepository(store, logger.NewNoopLogger())
	persisted := NewMeterUsageRepository(store, logger.NewNoopLogger())
	at := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	meterID := scope + "_meter"
	var records []*events.MeterUsage
	for i, quantity := range []int64{50, -70} {
		event := events.Event{
			ID: uuid.NewString(), TenantID: scope, EnvironmentID: scope, EventName: scope,
			ExternalCustomerID: scope, Source: "test", Timestamp: at.Add(time.Duration(i+1) * time.Minute),
			IngestedAt: time.Now().UTC(), Properties: map[string]interface{}{"quantity": quantity},
		}
		require.NoError(t, raw.InsertEvent(ctx, &event))
		records = append(records, &events.MeterUsage{Event: event, MeterID: meterID, QtyTotal: decimal.NewFromInt(quantity)})
	}
	require.NoError(t, persisted.BulkInsertMeterUsage(ctx, records))
	stored, err := persisted.GetUsage(ctx, &events.MeterUsageQueryParams{
		TenantID: scope, EnvironmentID: scope, ExternalCustomerID: scope, MeterID: meterID,
		AggregationType: types.AggregationSum, StartTime: at, EndTime: at.Add(time.Hour), UseFinal: true,
	})
	require.NoError(t, err)
	require.True(t, stored.TotalValue.Equal(decimal.NewFromInt(-20)), "persisted sum: %s", stored.TotalValue)
	for _, tc := range []struct {
		name           string
		window, bucket types.WindowSize
		want           int64
	}{
		{name: "scalar", want: -20},
		{name: "windowed", window: types.WindowSizeHour, want: -20},
		{name: "bucketed", bucket: types.WindowSizeHour, want: -20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := raw.GetUsage(ctx, &events.UsageParams{
				EventName: scope, ExternalCustomerID: scope, PropertyName: "quantity", AggregationType: types.AggregationSum,
				StartTime: at, EndTime: at.Add(time.Hour), WindowSize: tc.window, BucketSize: tc.bucket,
			})
			require.NoError(t, err)
			value := got.Value
			if tc.window != "" {
				require.Len(t, got.Results, 1)
				value = got.Results[0].Value
			}
			require.True(t, value.Equal(decimal.NewFromInt(tc.want)), "want %d, got %s", tc.want, value)
		})
	}
}
