package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/usagerecord"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestInMemoryUsageRecordStore(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, "tenant_1")
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env_1")

	store := NewInMemoryUsageRecordStore()
	periodStart := time.Now().UTC().Add(-10 * time.Hour)
	periodEnd := time.Now().UTC().Add(-4 * time.Hour)

	rec := &usagerecord.UsageRecord{
		ID:             "ur_1",
		CustomerID:     "cust_1",
		SubscriptionID: "sub_1",
		PlanID:         "plan_1",
		Amount:         decimal.NewFromInt(10),
		Currency:       "usd",
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		Syncs:          map[usagerecord.Marketplace]usagerecord.MarketplaceSyncEntry{},
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}

	// Create + duplicate-period guard
	require.NoError(t, store.Create(ctx, rec))
	dup := *rec
	dup.ID = "ur_2"
	require.Error(t, store.Create(ctx, &dup), "same subscription+period should be rejected")

	// ExistsForPeriod
	exists, err := store.ExistsForPeriod(ctx, "sub_1", periodStart, periodEnd)
	require.NoError(t, err)
	require.True(t, exists)

	notExists, err := store.ExistsForPeriod(ctx, "sub_1", periodStart, periodEnd.Add(time.Hour))
	require.NoError(t, err)
	require.False(t, notExists)

	// ListUnsynced
	unsynced, err := store.ListUnsynced(ctx, "tenant_1", "env_1")
	require.NoError(t, err)
	require.Len(t, unsynced, 1)
	require.Equal(t, "ur_1", unsynced[0].ID)

	// UpdateSyncResult
	err = store.UpdateSyncResult(ctx, "ur_1", usagerecord.MarketplaceAWS, usagerecord.MarketplaceSyncEntry{
		ConnectionID: "conn_1",
	}, true)
	require.NoError(t, err)

	unsynced, err = store.ListUnsynced(ctx, "tenant_1", "env_1")
	require.NoError(t, err)
	require.Len(t, unsynced, 0, "record should no longer be unsynced")

	store.Clear()
	unsynced, err = store.ListUnsynced(ctx, "tenant_1", "env_1")
	require.NoError(t, err)
	require.Len(t, unsynced, 0)
}
