package ent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	ierr "github.com/flexprice/flexprice/internal/errors"
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

func TestMeterUsageCheckpointRepositoryPeriodTotals(t *testing.T) {
	client := newRealPostgresTestClient(t)
	repo := NewMeterUsageCheckpointRepository(client, logger.NewNoopLogger())
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.GenerateUUIDWithPrefix("tenant")), "env_period")
	periodStart := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	hour := func(h int) time.Time { return periodStart.Add(time.Duration(h) * time.Hour) }
	row := func(meterID string, total int64, until time.Time) *meterusagecheckpoint.MeterUsageCheckpoint {
		return &meterusagecheckpoint.MeterUsageCheckpoint{
			ID: types.GenerateUUIDWithPrefix("muc"), MeterID: meterID, AggregationType: types.AggregationSum,
			Total: decimal.NewFromInt(total), AggregatedUntil: until,
			EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
		}
	}
	update := func(expected time.Time, rows ...*meterusagecheckpoint.MeterUsageCheckpoint) error {
		return repo.UpdatePeriodTotals(ctx, meterusagecheckpoint.UpdatePeriodTotalsParams{
			CustomerID: "cust_period", PeriodStart: periodStart, ExpectedAggregatedUntil: expected, Rows: rows,
		})
	}

	require.NoError(t, update(periodStart, row("m1", 5, hour(3))))
	require.NoError(t, update(hour(3), row("m1", 8, hour(4)), row("m2", 1, hour(4))))

	err := update(hour(3), row("m1", 100, hour(5)), row("m3", 1, hour(5)))
	require.True(t, ierr.IsVersionConflict(err), "a stale expected instant must be rejected as a version conflict: %v", err)

	err = update(periodStart, row("m4", 1, hour(5)))
	require.True(t, ierr.IsVersionConflict(err), "a stale reader writing only new meters must be rejected too: %v", err)

	rows, err := repo.ListPeriodTotals(ctx, "cust_period", periodStart)
	require.NoError(t, err)
	require.Len(t, rows, 2, "the rejected update wrote nothing")
	for _, r := range rows {
		require.True(t, r.AggregatedUntil.Equal(hour(4)))
		require.NotNil(t, r.PeriodStart)
	}

	// A running total at the same instant does not collide, and running-total reads skip per-period rows.
	running := row("m1", 42, hour(4))
	running.CustomerID = "cust_period"
	_, err = repo.Upsert(ctx, running)
	require.NoError(t, err)
	latest, err := repo.GetLatestBefore(ctx, meterusagecheckpoint.GetLatestBeforeParams{
		CustomerID: "cust_period", MeterID: "m1", AggregationType: types.AggregationSum, Before: hour(5),
	})
	require.NoError(t, err)
	require.True(t, latest.Total.Equal(decimal.NewFromInt(42)))
	require.Nil(t, latest.PeriodStart)
}
