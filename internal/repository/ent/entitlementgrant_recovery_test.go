package ent

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestEntitlementGrantSnapshotClearsCrossing(t *testing.T) {
	client := newRealPostgresTestClient(t)
	repo := NewEntitlementGrantRepository(client, logger.NewNoopLogger())
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.GenerateUUIDWithPrefix("tenant")), "env_grant_recovery")
	start := time.Now().UTC().Truncate(time.Second)
	crossed := start.Add(time.Hour)
	g := &entitlementgrant.EntitlementGrant{
		ID: types.GenerateUUIDWithPrefix("eg"), EntitlementConfigID: "ec_recovery", CustomerID: "cust_recovery", SubscriptionID: "sub_recovery",
		ScopeEntityType: types.EntitlementGrantScopeFeature, ScopeEntityID: "feat_recovery", Measure: types.EntitlementGrantMeasureQuantity,
		Quota: decimal.NewFromInt(100), Usage: decimal.NewFromInt(120), ValidFrom: start, ValidTo: start.AddDate(0, 1, 0),
		GrantStatus: types.EntitlementGrantStatusExhausted, QuotaCrossedAt: &crossed, LastComputedAt: &crossed,
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	_, err := repo.Create(ctx, g)
	require.NoError(t, err)
	recoveredAt := crossed.Add(time.Hour)
	recovered := entitlementgrant.NewEntitlementGrantBuilder(g).
		WithUsage(decimal.NewFromInt(80)).WithGrantStatus(types.EntitlementGrantStatusActive).
		WithQuotaCrossedAt(nil).WithLastComputedAt(&recoveredAt).Build()
	require.NoError(t, repo.UpdateSnapshot(ctx, recovered))
	stored, err := repo.Get(ctx, g.ID)
	require.NoError(t, err)
	require.Nil(t, stored.QuotaCrossedAt, "nil must clear the persisted column")
	require.Equal(t, types.EntitlementGrantStatusActive, stored.GrantStatus)
	require.True(t, stored.Usage.Equal(decimal.NewFromInt(80)))
	nextCrossing := recoveredAt.Add(time.Hour)
	exhausted := entitlementgrant.NewEntitlementGrantBuilder(stored).
		WithUsage(decimal.NewFromInt(110)).WithGrantStatus(types.EntitlementGrantStatusExhausted).
		WithQuotaCrossedAt(&nextCrossing).WithLastComputedAt(&nextCrossing).Build()
	require.NoError(t, repo.UpdateSnapshot(ctx, exhausted))
	stored, err = repo.Get(ctx, g.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.QuotaCrossedAt)
	require.True(t, stored.QuotaCrossedAt.Equal(nextCrossing))
}
