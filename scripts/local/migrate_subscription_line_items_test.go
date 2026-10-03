package local

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// seedTenant creates one plan, one meter, one published price for that plan, and one
// active subscription (no line items yet) for the given tenant, all sharing the *same*
// plan/price/meter IDs across tenants on purpose - this is the case that would silently
// break if the per-tenant price index in migrateSubscriptionLineItemsForTenants ever
// leaked data across tenants.
func seedTenant(t *testing.T, tenantID string, planRepo plan.Repository, meterRepo meter.Repository, priceRepo price.Repository, subRepo subscription.Repository) *subscription.Subscription {
	t.Helper()
	ctx := types.SetTenantID(context.Background(), tenantID)
	base := types.GetDefaultBaseModel(ctx)
	now := time.Now().UTC()

	// Plan/meter/price IDs are unique per tenant here because the in-memory test store
	// keys items by ID alone, without a tenant-scoped namespace - unlike Postgres, which
	// would happily let two tenants each have their own "plan_1". The tenant-isolation
	// property under test is unaffected: what matters is that each tenant's migration
	// only ever sees and uses its own plan/meter/price rows.
	planID := "plan_1_" + tenantID
	require.NoError(t, planRepo.Create(ctx, &plan.Plan{
		ID:        planID,
		Name:      "Plan for " + tenantID,
		BaseModel: base,
	}))

	require.NoError(t, meterRepo.CreateMeter(ctx, &meter.Meter{
		ID:        "meter_1_" + tenantID,
		EventName: "api_request",
		Name:      "API Requests (" + tenantID + ")",
		BaseModel: base,
	}))

	require.NoError(t, priceRepo.Create(ctx, &price.Price{
		ID:            "price_1_" + tenantID,
		Amount:        decimal.NewFromInt(10),
		Currency:      "usd",
		Type:          types.PRICE_TYPE_USAGE,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		MeterID:       "meter_1_" + tenantID,
		Description:   "API usage",
		EntityType:    types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:      planID,
		BaseModel:     base,
	}))

	sub := &subscription.Subscription{
		ID:                 "sub_1_" + tenantID,
		CustomerID:         "cust_1_" + tenantID,
		PlanID:             planID,
		Currency:           "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		BillingAnchor:      now,
		StartDate:          now,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 1, 0),
		BaseModel:          base,
	}
	require.NoError(t, subRepo.Create(ctx, sub))
	return sub
}

func TestMigrateSubscriptionLineItemsForTenants_IsolatesTenants(t *testing.T) {
	planStore := testutil.NewInMemoryPlanStore()
	meterStore := testutil.NewInMemoryMeterStore()
	priceStore := testutil.NewInMemoryPriceStore()
	subStore := testutil.NewInMemorySubscriptionStore()
	lineItemStore := testutil.NewInMemorySubscriptionLineItemStore()

	const tenantA = "tenant_a"
	const tenantB = "tenant_b"

	seedTenant(t, tenantA, planStore, meterStore, priceStore, subStore)
	seedTenant(t, tenantB, planStore, meterStore, priceStore, subStore)

	filter := types.NewNoLimitSubscriptionFilter()
	filter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusActive}

	err := migrateSubscriptionLineItemsForTenants(
		context.Background(),
		[]string{tenantA, tenantB},
		filter,
		subStore,
		lineItemStore,
		planStore,
		priceStore,
		meterStore,
	)
	require.NoError(t, err)

	for _, tenantID := range []string{tenantA, tenantB} {
		ctx := types.SetTenantID(context.Background(), tenantID)
		items, err := lineItemStore.ListBySubscription(ctx, &subscription.Subscription{ID: "sub_1_" + tenantID})
		require.NoError(t, err)
		require.Len(t, items, 1, "expected exactly one line item for %s", tenantID)

		item := items[0]
		require.Equal(t, "price_1_"+tenantID, item.PriceID, "line item for %s must use that tenant's own price, not another tenant's", tenantID)
		require.Equal(t, "plan_1_"+tenantID, item.EntityID)
		require.Equal(t, tenantID, item.TenantID)
		require.Equal(t, "API Requests ("+tenantID+")", item.MeterDisplayName)
	}
}

func TestMigrateSubscriptionLineItemsForTenants_SkipsSubscriptionsWithExistingLineItems(t *testing.T) {
	planStore := testutil.NewInMemoryPlanStore()
	meterStore := testutil.NewInMemoryMeterStore()
	priceStore := testutil.NewInMemoryPriceStore()
	subStore := testutil.NewInMemorySubscriptionStore()
	lineItemStore := testutil.NewInMemorySubscriptionLineItemStore()

	const tenantID = "tenant_existing"
	sub := seedTenant(t, tenantID, planStore, meterStore, priceStore, subStore)

	ctxSeed := types.SetTenantID(context.Background(), tenantID)
	sub.LineItems = []*subscription.SubscriptionLineItem{{ID: "already_here"}}
	require.NoError(t, subStore.Update(ctxSeed, sub))

	filter := types.NewNoLimitSubscriptionFilter()
	filter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusActive}

	err := migrateSubscriptionLineItemsForTenants(
		context.Background(),
		[]string{tenantID},
		filter,
		subStore,
		lineItemStore,
		planStore,
		priceStore,
		meterStore,
	)
	require.NoError(t, err)

	ctx := types.SetTenantID(context.Background(), tenantID)
	items, err := lineItemStore.ListBySubscription(ctx, sub)
	require.NoError(t, err)
	require.Empty(t, items, "should not create line items for a subscription that already has them")
}
