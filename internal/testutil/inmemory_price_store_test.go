package testutil

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestPrice builds a published plan-scoped price in the default test
// tenant/environment. endDate nil means the price never expires.
func newTestPrice(id, planID string, endDate *time.Time) *price.Price {
	now := time.Now().UTC()
	return &price.Price{
		ID:         id,
		Amount:     decimal.NewFromInt(10),
		Currency:   "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:   planID,
		EndDate:    endDate,
		BaseModel: types.BaseModel{
			TenantID:  types.DefaultTenantID,
			Status:    types.StatusPublished,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}

// TestInMemoryPriceStore_List_AllowExpiredPrices verifies that the in-memory
// store applies PriceFilter.AllowExpiredPrices the same way the real
// repository does (internal/repository/ent/price.go applyEntityQueryOptions:
// when AllowExpiredPrices is false, only prices with EndDate IS NULL or
// EndDate > now are returned; when true, no end-date predicate is applied).
func TestInMemoryPriceStore_List_AllowExpiredPrices(t *testing.T) {
	ctx := SetupContext()
	now := time.Now().UTC()
	pastEnd := now.Add(-24 * time.Hour)
	futureEnd := now.Add(24 * time.Hour)

	setupStore := func(t *testing.T) *InMemoryPriceStore {
		t.Helper()
		store := NewInMemoryPriceStore()
		require.NoError(t, store.Create(ctx, newTestPrice("price-active-no-end", "plan-1", nil)))
		require.NoError(t, store.Create(ctx, newTestPrice("price-active-future-end", "plan-1", &futureEnd)))
		require.NoError(t, store.Create(ctx, newTestPrice("price-expired", "plan-1", &pastEnd)))
		return store
	}

	testCases := []struct {
		name        string
		filter      *types.PriceFilter
		expectedIDs []string
	}{
		{
			name:        "default_filter_excludes_expired_prices",
			filter:      types.NewNoLimitPriceFilter(),
			expectedIDs: []string{"price-active-no-end", "price-active-future-end"},
		},
		{
			name:        "allow_expired_prices_includes_expired_prices",
			filter:      types.NewNoLimitPriceFilter().WithAllowExpiredPrices(true),
			expectedIDs: []string{"price-active-no-end", "price-active-future-end", "price-expired"},
		},
		{
			name: "allow_expired_combined_with_entity_ids",
			filter: types.NewNoLimitPriceFilter().
				WithEntityIDs([]string{"plan-1"}).
				WithAllowExpiredPrices(true),
			expectedIDs: []string{"price-active-no-end", "price-active-future-end", "price-expired"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := setupStore(t)

			prices, err := store.List(ctx, tc.filter)
			require.NoError(t, err)

			gotIDs := lo.Map(prices, func(p *price.Price, _ int) string { return p.ID })
			assert.ElementsMatch(t, tc.expectedIDs, gotIDs)

			count, err := store.Count(ctx, tc.filter)
			require.NoError(t, err)
			assert.Equal(t, len(tc.expectedIDs), count)
		})
	}
}

// TestInMemoryPriceStore_GetByPlanID_IncludesExpiredPrices pins the fidelity
// of GetByPlanID: the real repository (internal/repository/ent/price.go
// GetByPlanID) filters only on entity ID + published status and applies no
// end-date predicate, so expired prices must be returned.
func TestInMemoryPriceStore_GetByPlanID_IncludesExpiredPrices(t *testing.T) {
	ctx := SetupContext()
	pastEnd := time.Now().UTC().Add(-24 * time.Hour)

	store := NewInMemoryPriceStore()
	require.NoError(t, store.Create(ctx, newTestPrice("price-active", "plan-1", nil)))
	require.NoError(t, store.Create(ctx, newTestPrice("price-expired", "plan-1", &pastEnd)))
	require.NoError(t, store.Create(ctx, newTestPrice("price-other-plan", "plan-2", nil)))

	prices, err := store.GetByPlanID(ctx, "plan-1")
	require.NoError(t, err)

	gotIDs := lo.Map(prices, func(p *price.Price, _ int) string { return p.ID })
	assert.ElementsMatch(t, []string{"price-active", "price-expired"}, gotIDs)
}

// newTestSubscriptionPrice builds a published subscription-scoped price —
// subscription-scoped prices store the subscription ID in entity_id.
func newTestSubscriptionPrice(id, subscriptionID string) *price.Price {
	p := newTestPrice(id, subscriptionID, nil)
	p.EntityType = types.PRICE_ENTITY_TYPE_SUBSCRIPTION
	return p
}

// TestInMemoryPriceStore_List_SubscriptionIDFilter mirrors the real Ent repo
// (internal/repository/ent/price.go applyEntityQueryOptions): SubscriptionID
// filters to subscription-scoped prices whose entity_id matches — prices of
// other subscriptions or plans must never leak into the result.
func TestInMemoryPriceStore_List_SubscriptionIDFilter(t *testing.T) {
	ctx := SetupContext()

	setupStore := func(t *testing.T) *InMemoryPriceStore {
		t.Helper()
		store := NewInMemoryPriceStore()
		require.NoError(t, store.Create(ctx, newTestSubscriptionPrice("price-sub-1a", "sub-1")))
		require.NoError(t, store.Create(ctx, newTestSubscriptionPrice("price-sub-1b", "sub-1")))
		require.NoError(t, store.Create(ctx, newTestSubscriptionPrice("price-sub-2", "sub-2")))
		require.NoError(t, store.Create(ctx, newTestPrice("price-plan", "plan-1", nil)))
		return store
	}

	testCases := []struct {
		name        string
		filter      *types.PriceFilter
		expectedIDs []string
	}{
		{
			name:        "returns_only_the_requested_subscriptions_prices",
			filter:      types.NewNoLimitPriceFilter().WithSubscriptionID("sub-1"),
			expectedIDs: []string{"price-sub-1a", "price-sub-1b"},
		},
		{
			name: "combined_with_entity_type_subscription",
			filter: types.NewNoLimitPriceFilter().
				WithSubscriptionID("sub-2").
				WithEntityType(types.PRICE_ENTITY_TYPE_SUBSCRIPTION),
			expectedIDs: []string{"price-sub-2"},
		},
		{
			name:        "unknown_subscription_returns_nothing",
			filter:      types.NewNoLimitPriceFilter().WithSubscriptionID("sub-does-not-exist"),
			expectedIDs: []string{},
		},
		{
			name:        "no_subscription_id_returns_everything",
			filter:      types.NewNoLimitPriceFilter(),
			expectedIDs: []string{"price-sub-1a", "price-sub-1b", "price-sub-2", "price-plan"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := setupStore(t)

			prices, err := store.List(ctx, tc.filter)
			require.NoError(t, err)

			gotIDs := lo.Map(prices, func(p *price.Price, _ int) string { return p.ID })
			assert.ElementsMatch(t, tc.expectedIDs, gotIDs)
		})
	}
}
