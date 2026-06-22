package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

const testEnvSubStore = "env_test_sub_thresh"

func TestInMemorySubscriptionStore_GetSubscriptionsWithAutoInvoiceThreshold_StandaloneOnly(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), testEnvSubStore)
	store := NewInMemorySubscriptionStore()
	base := types.GetDefaultBaseModel(ctx)
	now := time.Now().UTC()
	th := decimal.RequireFromString("100")

	standaloneEligible := &subscription.Subscription{
		ID:                   "sub_thresh_standalone",
		CustomerID:           "cust_1",
		PlanID:               "plan_1",
		Currency:             "usd",
		SubscriptionStatus:   types.SubscriptionStatusActive,
		SubscriptionType:     types.SubscriptionTypeStandalone,
		EnvironmentID:        testEnvSubStore,
		AutoInvoiceThreshold: &th,
		BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:   1,
		BillingCycle:         types.BillingCycleAnniversary,
		BillingAnchor:        now,
		StartDate:            now,
		CurrentPeriodStart:   now,
		CurrentPeriodEnd:     now.AddDate(0, 1, 0),
		BaseModel:            base,
	}

	parentWithThreshold := &subscription.Subscription{
		ID:                   "sub_thresh_parent",
		CustomerID:           "cust_2",
		PlanID:               "plan_1",
		Currency:             "usd",
		SubscriptionStatus:   types.SubscriptionStatusActive,
		SubscriptionType:     types.SubscriptionTypeParent,
		EnvironmentID:        testEnvSubStore,
		AutoInvoiceThreshold: &th,
		BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:   1,
		BillingCycle:         types.BillingCycleAnniversary,
		BillingAnchor:        now,
		StartDate:            now,
		CurrentPeriodStart:   now,
		CurrentPeriodEnd:     now.AddDate(0, 1, 0),
		BaseModel:            base,
	}

	require.NoError(t, store.Create(ctx, standaloneEligible))
	require.NoError(t, store.Create(ctx, parentWithThreshold))

	got, err := store.GetSubscriptionsWithAutoInvoiceThreshold(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, standaloneEligible.ID, got[0].ID)
}
