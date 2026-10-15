package testutil

import (
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

	checkpoint := func(id string, asOf time.Time, total int64) *meterusagecheckpoint.MeterUsageCheckpoint {
		return &meterusagecheckpoint.MeterUsageCheckpoint{
			ID:         id,
			CustomerID: "cust_1",
			MeterID:    "meter_1",
			Total:      decimal.NewFromInt(total),
			AsOf:       asOf,
			BaseModel:  types.BaseModel{TenantID: "tenant_1"},
		}
	}

	t.Run("no checkpoint returns nil", func(t *testing.T) {
		store := NewInMemoryMeterUsageCheckpointStore()
		got, err := store.GetLatestAtOrBefore(ctx, "cust_1", "meter_1", nov1)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("latest at or before wins", func(t *testing.T) {
		store := NewInMemoryMeterUsageCheckpointStore()
		_, err := store.Upsert(ctx, checkpoint("ck_oct", oct1, 40))
		require.NoError(t, err)
		_, err = store.Upsert(ctx, checkpoint("ck_nov", nov1, -5))
		require.NoError(t, err)

		got, err := store.GetLatestAtOrBefore(ctx, "cust_1", "meter_1", nov1.Add(-time.Second))
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "ck_oct", got.ID)

		got, err = store.GetLatestAtOrBefore(ctx, "cust_1", "meter_1", nov1)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.True(t, decimal.NewFromInt(-5).Equal(got.Total), "negative totals are kept")
	})

	t.Run("upsert on the same as_of replaces the total", func(t *testing.T) {
		store := NewInMemoryMeterUsageCheckpointStore()
		_, err := store.Upsert(ctx, checkpoint("ck_first", nov1, 40))
		require.NoError(t, err)
		saved, err := store.Upsert(ctx, checkpoint("ck_second", nov1, 46))
		require.NoError(t, err)

		assert.Equal(t, "ck_first", saved.ID)
		got, err := store.GetLatestAtOrBefore(ctx, "cust_1", "meter_1", nov1)
		require.NoError(t, err)
		assert.True(t, decimal.NewFromInt(46).Equal(got.Total))
	})

	t.Run("other tenants and meters are not visible", func(t *testing.T) {
		store := NewInMemoryMeterUsageCheckpointStore()
		_, err := store.Upsert(ctx, checkpoint("ck_1", oct1, 40))
		require.NoError(t, err)

		got, err := store.GetLatestAtOrBefore(testCtx("tenant_2"), "cust_1", "meter_1", nov1)
		require.NoError(t, err)
		assert.Nil(t, got)

		got, err = store.GetLatestAtOrBefore(ctx, "cust_1", "meter_2", nov1)
		require.NoError(t, err)
		assert.Nil(t, got)
	})
}
