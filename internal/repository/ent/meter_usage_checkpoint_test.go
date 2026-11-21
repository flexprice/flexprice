package ent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestMeterUsageCheckpointRepository(t *testing.T) {
	client := newRealPostgresTestClient(t)
	repo := NewMeterUsageCheckpointRepository(client, logger.NewNoopLogger())
	tenantID := types.GenerateUUIDWithPrefix("tenant")
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), tenantID), "env_checkpoint")
	ctx = types.SetUserID(ctx, "checkpoint_test")
	cutoff := time.Now().UTC().Truncate(time.Second)
	checkpoint := func(total int64) *meterusagecheckpoint.MeterUsageCheckpoint {
		return &meterusagecheckpoint.MeterUsageCheckpoint{
			ID: types.GenerateUUIDWithPrefix("muc"), CustomerID: "cust_checkpoint", MeterID: "meter_checkpoint",
			AggregationType: types.AggregationSum, Total: decimal.NewFromInt(total), AggregatedUntil: cutoff,
			EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
		}
	}
	params := meterusagecheckpoint.GetLatestBeforeParams{CustomerID: "cust_checkpoint", MeterID: "meter_checkpoint", AggregationType: types.AggregationSum, Before: cutoff.Add(time.Second)}
	first, err := repo.Upsert(ctx, checkpoint(-20))
	require.NoError(t, err)
	second, err := repo.Upsert(ctx, checkpoint(-30))
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.True(t, second.Total.Equal(decimal.NewFromInt(-30)))
	atBoundary := params
	atBoundary.Before = cutoff
	got, err := repo.GetLatestBefore(ctx, atBoundary)
	require.NoError(t, err)
	require.Nil(t, got)
	for _, other := range []context.Context{
		types.SetTenantID(ctx, types.GenerateUUIDWithPrefix("tenant")),
		types.SetEnvironmentID(ctx, "env_other"),
	} {
		got, err := repo.GetLatestBefore(other, params)
		require.NoError(t, err)
		require.Nil(t, got)
		_, err = repo.Upsert(other, checkpoint(999))
		require.Error(t, err, "writes cannot change scope through a model field")
	}
	var wg sync.WaitGroup
	ids := make(chan string, 4)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			saved, err := repo.Upsert(ctx, checkpoint(-40))
			if err != nil {
				errs <- err
				return
			}
			ids <- saved.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	for id := range ids {
		require.Equal(t, first.ID, id)
	}
	got, err = repo.GetLatestBefore(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, got.Total.Equal(decimal.NewFromInt(-40)))
}

func TestMeterUsageCheckpointRepositoryUpdateCustomerCheckpoints(t *testing.T) {
	client := newRealPostgresTestClient(t)
	repo := NewMeterUsageCheckpointRepository(client, logger.NewNoopLogger())
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.GenerateUUIDWithPrefix("tenant")), "env_period")
	periodStart := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	hour := func(h int) time.Time { return periodStart.Add(time.Duration(h) * time.Hour) }
	total := func(meterID string, aggregation types.AggregationType, qty int64) meterusagecheckpoint.PeriodTotal {
		return meterusagecheckpoint.PeriodTotal{PeriodStart: periodStart, MeterID: meterID, AggregationType: aggregation, Quantity: decimal.NewFromInt(qty)}
	}
	update := func(customerID string, from, to time.Time, totals ...meterusagecheckpoint.PeriodTotal) *meterusagecheckpoint.CustomerCheckpointUpdate {
		return &meterusagecheckpoint.CustomerCheckpointUpdate{CustomerID: customerID, From: from, To: to, Totals: totals}
	}
	totals := func(customerID string) map[string]string {
		rows, err := repo.ListPeriodTotals(ctx, customerID, periodStart)
		require.NoError(t, err)
		got := map[string]string{}
		for _, r := range rows {
			got[r.MeterID] = r.Total.String()
		}
		return got
	}

	n, err := repo.UpdateCustomerCheckpoints(ctx, []*meterusagecheckpoint.CustomerCheckpointUpdate{
		update("cust_a", time.Time{}, hour(3), total("m_sum", types.AggregationSum, 5), total("m_max", types.AggregationMax, -4)),
		update("cust_b", time.Time{}, hour(3), total("m_sum", types.AggregationSum, 1)),
	})
	require.NoError(t, err)
	require.Equal(t, 2, n, "customers without a watermark get one at To")

	n, err = repo.UpdateCustomerCheckpoints(ctx, []*meterusagecheckpoint.CustomerCheckpointUpdate{
		update("cust_a", hour(3), hour(4), total("m_sum", types.AggregationSum, 3), total("m_max", types.AggregationMax, -9), total("m_count", types.AggregationCount, 2)),
		update("cust_b", hour(2), hour(4), total("m_sum", types.AggregationSum, 100)),
	})
	require.NoError(t, err)
	require.Equal(t, 1, n, "cust_b's watermark is no longer at From, so its usage is not added again")
	require.Equal(t, map[string]string{"m_sum": "8", "m_max": "-4", "m_count": "2"}, totals("cust_a"),
		"SUM and COUNT add, MAX keeps the higher value")
	require.Equal(t, map[string]string{"m_sum": "1"}, totals("cust_b"))

	watermarks, err := repo.ListMeterUsageCheckpointWatermarks(ctx, []string{"cust_a", "cust_b", "cust_none"})
	require.NoError(t, err)
	until := map[string]time.Time{}
	for _, w := range watermarks {
		until[w.CustomerID] = w.AggregatedUntil
	}
	require.Len(t, until, 2)
	require.True(t, until["cust_a"].Equal(hour(4)))
	require.True(t, until["cust_b"].Equal(hour(3)))

	other, err := repo.ListMeterUsageCheckpointWatermarks(types.SetEnvironmentID(ctx, "env_other"), []string{"cust_a"})
	require.NoError(t, err)
	require.Empty(t, other, "watermarks are scoped to the environment")

	// Running totals live on other meters (never-reset SUM); their reads skip per-period rows.
	_, err = repo.Upsert(ctx, &meterusagecheckpoint.MeterUsageCheckpoint{
		ID: types.GenerateUUIDWithPrefix("muc"), CustomerID: "cust_a", MeterID: "m_running", AggregationType: types.AggregationSum,
		Total: decimal.NewFromInt(42), AggregatedUntil: hour(4), EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	})
	require.NoError(t, err)
	latest, err := repo.GetLatestBefore(ctx, meterusagecheckpoint.GetLatestBeforeParams{
		CustomerID: "cust_a", MeterID: "m_running", AggregationType: types.AggregationSum, Before: hour(5),
	})
	require.NoError(t, err)
	require.True(t, latest.Total.Equal(decimal.NewFromInt(42)))
	require.Nil(t, latest.PeriodStart)
}
