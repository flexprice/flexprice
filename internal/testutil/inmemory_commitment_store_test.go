package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/types"
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
			ID: id, SubscriptionID: sub, CustomerID: "cus", Name: id, InvoiceLabel: id, Currency: "usd",
			ScopeEntityType: types.CommitmentScopePrice, ScopeEntityID: "price_1", Type: types.COMMITMENT_TYPE_AMOUNT,
			Value: decimal.NewFromInt(100), Measurement: types.CommitmentMeasurementBillingPeriod, Timezone: "UTC",
			Binding: true, StartDate: start, EndDate: end, CommitmentStatus: types.CommitmentStatusActive,
			EnvironmentID: "env1", BaseModel: types.BaseModel{TenantID: "t1", Status: types.StatusPublished},
		}
	}
	_, err := store.Create(ctx, mk("cmt_a", "sub_1", jan, jan.AddDate(1, 0, 0)))
	require.NoError(t, err)
	_, err = store.Create(ctx, mk("cmt_b", "sub_1", jan.AddDate(2, 0, 0), jan.AddDate(3, 0, 0)))
	require.NoError(t, err)

	f := types.NewNoLimitCommitmentFilter()
	f.SubscriptionIDs = []string{"sub_1"}
	from, to := jan.AddDate(0, 3, 0), jan.AddDate(0, 4, 0)
	f.ActiveFrom, f.ActiveTo = &from, &to
	got, err := store.List(ctx, f)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "cmt_a", got[0].ID)

	otherEnv := context.WithValue(ctx, types.CtxEnvironmentID, "env2")
	got, err = store.List(otherEnv, f)
	require.NoError(t, err)
	require.Empty(t, got)
}
