package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestInMemoryCommitmentStore_ActiveWindowAndIsolation(t *testing.T) {
	store := NewInMemoryCommitmentStore()
	ctx := context.WithValue(context.Background(), types.CtxTenantID, "t1")
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env1")
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	mk := func(id, sub string, start, end time.Time) *commitment.Commitment {
		return &commitment.Commitment{
			ID:               id,
			SubscriptionID:   sub,
			CustomerID:       "cus",
			Name:             id,
			InvoiceLabel:     id,
			Currency:         "usd",
			ScopeEntityType:  types.CommitmentScopePrice,
			ScopeEntityID:    "price_1",
			Type:             types.COMMITMENT_TYPE_AMOUNT,
			Value:            decimal.NewFromInt(100),
			Measurement:      types.CommitmentMeasurementBillingPeriod,
			Timezone:         "UTC",
			Binding:          true,
			StartDate:        start,
			EndDate:          lo.ToPtr(end),
			CommitmentStatus: types.CommitmentStatusActive,
			EnvironmentID:    "env1",
			BaseModel: types.BaseModel{
				TenantID:  "t1",
				Status:    types.StatusPublished,
				CreatedAt: start,
			},
		}
	}

	_, err := store.Create(ctx, mk("cmt_a", "sub_1", jan, jan.AddDate(1, 0, 0)))
	require.NoError(t, err)

	_, err = store.Create(ctx, mk("cmt_b", "sub_1", jan.AddDate(2, 0, 0), jan.AddDate(3, 0, 0)))
	require.NoError(t, err)

	openEnded := mk("cmt_open", "sub_1", jan.AddDate(0, 1, 0), time.Time{})
	openEnded.EndDate = nil
	_, err = store.Create(ctx, openEnded)
	require.NoError(t, err)

	archived := mk("cmt_archived", "sub_1", jan, jan.AddDate(1, 0, 0))
	archived.Status = types.StatusArchived
	_, err = store.Create(ctx, archived)
	require.NoError(t, err)

	f := commitment.NewNoLimitFilter()
	f.SubscriptionIDs = []string{"sub_1"}
	from, to := jan.AddDate(0, 3, 0), jan.AddDate(0, 4, 0)
	f.ActiveFrom, f.ActiveTo = &from, &to

	got, err := store.List(ctx, f)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"cmt_a", "cmt_open"}, lo.Map(got, func(c *commitment.Commitment, _ int) string { return c.ID }))

	otherEnv := context.WithValue(ctx, types.CtxEnvironmentID, "env2")
	got, err = store.List(otherEnv, f)
	require.NoError(t, err)
	require.Empty(t, got)

	created := commitment.NewNoLimitFilter()
	createdFrom := jan.AddDate(1, 0, 0)
	created.TimeRangeFilter = &types.TimeRangeFilter{StartTime: &createdFrom}

	got, err = store.List(ctx, created)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "cmt_b", got[0].ID)

	got, err = store.List(ctx, nil)
	require.NoError(t, err)
	require.Len(t, got, 3)

	count, err := store.Count(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, 3, count)
}

func TestInMemoryCommitmentStore_Update(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.CtxTenantID, "t1")
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env1")
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	newCommitment := func() *commitment.Commitment {
		return &commitment.Commitment{
			ID:               "cmt_1",
			SubscriptionID:   "sub_1",
			CustomerID:       "cus",
			Name:             "Original",
			InvoiceLabel:     "Original",
			Currency:         "usd",
			ScopeEntityType:  types.CommitmentScopePrice,
			ScopeEntityID:    "price_1",
			Type:             types.COMMITMENT_TYPE_AMOUNT,
			Value:            decimal.NewFromInt(100),
			Measurement:      types.CommitmentMeasurementBillingPeriod,
			Timezone:         "UTC",
			StartDate:        jan,
			EndDate:          lo.ToPtr(jan.AddDate(1, 0, 0)),
			CommitmentStatus: types.CommitmentStatusActive,
			EnvironmentID:    "env1",
			BaseModel: types.BaseModel{
				TenantID:  "t1",
				Status:    types.StatusPublished,
				CreatedAt: jan,
			},
		}
	}

	t.Run("other environment is not found", func(t *testing.T) {
		store := NewInMemoryCommitmentStore()

		_, err := store.Create(ctx, newCommitment())
		require.NoError(t, err)

		change := newCommitment()
		change.Name = "Renamed"

		otherEnv := context.WithValue(ctx, types.CtxEnvironmentID, "env2")
		_, err = store.Update(otherEnv, change)
		require.True(t, errors.IsNotFound(err), "got %v", err)

		got, err := store.Get(ctx, "cmt_1")
		require.NoError(t, err)
		require.Equal(t, "Original", got.Name)
	})

	t.Run("only mutable fields change", func(t *testing.T) {
		store := NewInMemoryCommitmentStore()

		_, err := store.Create(ctx, newCommitment())
		require.NoError(t, err)

		change := newCommitment()
		change.Name = "Renamed"
		change.Value = decimal.NewFromInt(999)
		change.EndDate = lo.ToPtr(jan.AddDate(5, 0, 0))
		change.SubscriptionID = "sub_other"
		change.CommitmentStatus = ""

		updated, err := store.Update(ctx, change)
		require.NoError(t, err)
		require.Equal(t, "Renamed", updated.Name)

		got, err := store.Get(ctx, "cmt_1")
		require.NoError(t, err)
		require.Equal(t, "Renamed", got.Name)
		require.True(t, got.Value.Equal(decimal.NewFromInt(100)))
		require.Equal(t, jan.AddDate(1, 0, 0), *got.EndDate)
		require.Equal(t, "sub_1", got.SubscriptionID)
		require.Equal(t, types.CommitmentStatusActive, got.CommitmentStatus)
	})
}
