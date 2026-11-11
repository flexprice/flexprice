package testutil

import (
	"sync"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryMeterUsageCheckpointStore(t *testing.T) {
	ctx := testCtx("tenant_1")
	oct1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	before := func(at time.Time) meterusagecheckpoint.GetLatestBeforeParams {
		return meterusagecheckpoint.GetLatestBeforeParams{
			CustomerID:      "cust_1",
			MeterID:         "meter_1",
			AggregationType: types.AggregationSum,
			Before:          at,
		}
	}

	checkpoint := func(id string, aggregatedUntil time.Time, total int64) *meterusagecheckpoint.MeterUsageCheckpoint {
		return &meterusagecheckpoint.MeterUsageCheckpoint{
			ID:              id,
			CustomerID:      "cust_1",
			MeterID:         "meter_1",
			AggregationType: types.AggregationSum,
			Total:           decimal.NewFromInt(total),
			AggregatedUntil: aggregatedUntil,
			BaseModel:       types.BaseModel{TenantID: "tenant_1"},
		}
	}

	t.Run("no checkpoint returns nil", func(t *testing.T) {
		store := NewInMemoryMeterUsageCheckpointStore()
		got, err := store.GetLatestBefore(ctx, before(nov1))
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("latest strictly before wins", func(t *testing.T) {
		store := NewInMemoryMeterUsageCheckpointStore()
		_, err := store.Upsert(ctx, checkpoint("ck_oct", oct1, 40))
		require.NoError(t, err)
		_, err = store.Upsert(ctx, checkpoint("ck_nov", nov1, -5))
		require.NoError(t, err)

		got, err := store.GetLatestBefore(ctx, before(nov1))
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "ck_oct", got.ID, "a checkpoint at the instant itself is excluded")

		got, err = store.GetLatestBefore(ctx, before(nov1.Add(time.Second)))
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.True(t, decimal.NewFromInt(-5).Equal(got.Total), "negative totals are kept")
	})

	t.Run("upsert on the same instant replaces the total", func(t *testing.T) {
		store := NewInMemoryMeterUsageCheckpointStore()
		_, err := store.Upsert(ctx, checkpoint("ck_first", nov1, 40))
		require.NoError(t, err)
		saved, err := store.Upsert(ctx, checkpoint("ck_second", nov1, 46))
		require.NoError(t, err)

		assert.Equal(t, "ck_first", saved.ID)
		got, err := store.GetLatestBefore(ctx, before(nov1.Add(time.Second)))
		require.NoError(t, err)
		assert.True(t, decimal.NewFromInt(46).Equal(got.Total))
	})

	t.Run("another aggregation type is not read back", func(t *testing.T) {
		store := NewInMemoryMeterUsageCheckpointStore()
		_, err := store.Upsert(ctx, checkpoint("ck_1", oct1, 40))
		require.NoError(t, err)

		params := before(nov1)
		params.AggregationType = types.AggregationMax
		got, err := store.GetLatestBefore(ctx, params)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("other tenants and meters are not visible", func(t *testing.T) {
		store := NewInMemoryMeterUsageCheckpointStore()
		_, err := store.Upsert(ctx, checkpoint("ck_1", oct1, 40))
		require.NoError(t, err)

		got, err := store.GetLatestBefore(testCtx("tenant_2"), before(nov1))
		require.NoError(t, err)
		assert.Nil(t, got)

		params := before(nov1)
		params.MeterID = "meter_2"
		got, err = store.GetLatestBefore(ctx, params)
		require.NoError(t, err)
		assert.Nil(t, got)
	})
}

func TestInMemoryMeterUsageCheckpointConcurrentWrites(t *testing.T) {
	ctx := types.SetEnvironmentID(testCtx("tenant_checkpoint"), "env_checkpoint")
	store := NewInMemoryMeterUsageCheckpointStore()
	cutoff := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	ids := make(chan string, 16)
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := &meterusagecheckpoint.MeterUsageCheckpoint{
				ID: types.GenerateUUIDWithPrefix("muc"), CustomerID: "cust_1", MeterID: "meter_1",
				AggregationType: types.AggregationSum, Total: decimal.NewFromInt(-20), AggregatedUntil: cutoff,
				EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
			}
			saved, err := store.Upsert(ctx, c)
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
	var firstID string
	for id := range ids {
		if firstID == "" {
			firstID = id
		}
		require.Equal(t, firstID, id)
	}
	require.NotEmpty(t, firstID)
}

func TestInMemoryMeterUsageCheckpointRejectsScopeMismatch(t *testing.T) {
	ctx := types.SetEnvironmentID(testCtx("tenant_1"), "env_1")
	store := NewInMemoryMeterUsageCheckpointStore()
	for _, tc := range []struct{ tenant, environment string }{{"tenant_2", "env_1"}, {"tenant_1", "env_2"}} {
		_, err := store.Upsert(ctx, &meterusagecheckpoint.MeterUsageCheckpoint{
			ID: "muc_1", CustomerID: "cust_1", MeterID: "meter_1", AggregationType: types.AggregationSum,
			AggregatedUntil: time.Now().UTC(), EnvironmentID: tc.environment, BaseModel: types.BaseModel{TenantID: tc.tenant},
		})
		require.Error(t, err)
	}
}
