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
