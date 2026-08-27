package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testEnvironmentID = "env_grant_store_test"

func grantStoreTestContext() context.Context {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, types.DefaultTenantID)
	ctx = types.SetEnvironmentID(ctx, testEnvironmentID)
	return ctx
}

func seedGrant(t *testing.T, store *InMemoryEntitlementGrantStore, mutate func(*entitlementgrant.EntitlementGrant)) *entitlementgrant.EntitlementGrant {
	t.Helper()
	ctx := grantStoreTestContext()
	g := &entitlementgrant.EntitlementGrant{
		ID:                  types.GenerateUUID(),
		EntitlementConfigID: "ent_1",
		CustomerID:          "cust_1",
		SubscriptionID:      "sub_1",
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       "feat_1",
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(1000),
		Usage:               decimal.Zero,
		ValidFrom:           time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		ValidTo:             time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		GrantStatus:         types.EntitlementGrantStatusActive,
		EnvironmentID:       testEnvironmentID,
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	if mutate != nil {
		mutate(g)
	}
	created, err := store.Create(ctx, g)
	require.NoError(t, err)
	return created
}

func TestInMemoryCloseWindow_ShortensValidTo(t *testing.T) {
	store := NewInMemoryEntitlementGrantStore()
	ctx := grantStoreTestContext()
	g := seedGrant(t, store, nil)

	boundary := g.ValidFrom.Add(time.Hour)
	require.NoError(t, store.CloseWindow(ctx, g.ID, boundary))

	after, err := store.Get(ctx, g.ID)
	require.NoError(t, err)
	assert.True(t, after.ValidTo.Equal(boundary),
		"expected valid_to %s, got %s", boundary, after.ValidTo)
}

// Mirrors the repo's `valid_to > $1` guard: a replayed or out-of-order close must
// not hand back coverage that was already given up.
func TestInMemoryCloseWindow_NeverExtends(t *testing.T) {
	store := NewInMemoryEntitlementGrantStore()
	ctx := grantStoreTestContext()
	g := seedGrant(t, store, nil)

	boundary := g.ValidFrom.Add(time.Hour)
	require.NoError(t, store.CloseWindow(ctx, g.ID, boundary))
	require.NoError(t, store.CloseWindow(ctx, g.ID, g.ValidFrom.Add(10*time.Hour)))

	after, err := store.Get(ctx, g.ID)
	require.NoError(t, err)
	assert.True(t, after.ValidTo.Equal(boundary),
		"a later boundary must not extend the window: %s", after.ValidTo)
}

// The closed row still owes a final refresh, so the close must leave the fields the
// evaluator owns — and last_computed_at in particular — untouched.
func TestInMemoryCloseWindow_LeavesSnapshotFieldsAlone(t *testing.T) {
	store := NewInMemoryEntitlementGrantStore()
	ctx := grantStoreTestContext()
	g := seedGrant(t, store, nil)

	computedAt := g.ValidFrom.Add(30 * time.Minute)
	require.NoError(t, store.UpdateSnapshot(ctx, entitlementgrant.NewEntitlementGrantBuilder(g).
		WithUsage(decimal.NewFromInt(120)).
		WithLastComputedAt(&computedAt).
		Build()))

	require.NoError(t, store.CloseWindow(ctx, g.ID, g.ValidFrom.Add(time.Hour)))

	after, err := store.Get(ctx, g.ID)
	require.NoError(t, err)
	assert.True(t, after.Usage.Equal(decimal.NewFromInt(120)), "close must not touch usage")
	assert.True(t, after.Quota.Equal(g.Quota), "close must not touch quota")
	require.NotNil(t, after.LastComputedAt)
	assert.True(t, after.LastComputedAt.Equal(computedAt),
		"close must not stamp last_computed_at, or the row finalizes early")
	assert.True(t, after.LastComputedAt.Before(after.ValidTo),
		"closed row must stay unfinalized so it still owes a final refresh")
}

func TestInMemoryCloseWindow_MissingGrant(t *testing.T) {
	store := NewInMemoryEntitlementGrantStore()
	err := store.CloseWindow(grantStoreTestContext(), "eg_missing", time.Now().UTC())
	assert.Error(t, err)
}
