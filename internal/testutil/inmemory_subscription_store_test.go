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

// newFilterTestSubscription builds a minimal subscription for filter tests.
func newFilterTestSubscription(ctx context.Context, id string, status types.SubscriptionStatus) *subscription.Subscription {
	now := time.Now().UTC()
	return &subscription.Subscription{
		ID:                 id,
		CustomerID:         "cust_filter",
		PlanID:             "plan_filter",
		Currency:           "usd",
		SubscriptionStatus: status,
		EnvironmentID:      testEnvSubStore,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		BillingAnchor:      now,
		StartDate:          now,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 1, 0),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
}

func TestInMemorySubscriptionStore_ListFiltering(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), testEnvSubStore)

	active := newFilterTestSubscription(ctx, "sub_filter_active", types.SubscriptionStatusActive)
	trialing := newFilterTestSubscription(ctx, "sub_filter_trialing", types.SubscriptionStatusTrialing)
	cancelled := newFilterTestSubscription(ctx, "sub_filter_cancelled", types.SubscriptionStatusCancelled)

	testCases := []struct {
		name        string
		filter      *types.SubscriptionFilter
		expectedIDs []string
	}{
		{
			name: "status_not_in_excludes_cancelled_subscriptions",
			filter: &types.SubscriptionFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				SubscriptionStatus: []types.SubscriptionStatus{
					types.SubscriptionStatusActive,
					types.SubscriptionStatusTrialing,
					types.SubscriptionStatusCancelled,
				},
				SubscriptionStatusNotIn: []types.SubscriptionStatus{types.SubscriptionStatusCancelled},
			},
			expectedIDs: []string{"sub_filter_active", "sub_filter_trialing"},
		},
		{
			name: "status_not_in_combines_with_default_active_status",
			filter: &types.SubscriptionFilter{
				QueryFilter:             types.NewNoLimitQueryFilter(),
				SubscriptionStatusNotIn: []types.SubscriptionStatus{types.SubscriptionStatusActive},
			},
			expectedIDs: []string{},
		},
		{
			name: "subscription_ids_filters_to_requested_ids",
			filter: &types.SubscriptionFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				SubscriptionIDs: []string{
					"sub_filter_active",
					"sub_missing",
				},
			},
			expectedIDs: []string{"sub_filter_active"},
		},
		{
			name: "subscription_ids_respects_explicit_status_filter",
			filter: &types.SubscriptionFilter{
				QueryFilter:        types.NewNoLimitQueryFilter(),
				SubscriptionIDs:    []string{"sub_filter_active", "sub_filter_cancelled"},
				SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusCancelled},
			},
			expectedIDs: []string{"sub_filter_cancelled"},
		},
		{
			name: "no_status_filter_defaults_to_active_only",
			filter: &types.SubscriptionFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
			},
			expectedIDs: []string{"sub_filter_active"},
		},
		{
			name: "explicit_status_filter_overrides_default_active",
			filter: &types.SubscriptionFilter{
				QueryFilter:        types.NewNoLimitQueryFilter(),
				SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusCancelled},
			},
			expectedIDs: []string{"sub_filter_cancelled"},
		},
		{
			name: "customer_ids_filter_matches_customer",
			filter: &types.SubscriptionFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				CustomerIDs: []string{"cust_filter"},
			},
			expectedIDs: []string{"sub_filter_active"},
		},
		{
			name: "customer_ids_filter_excludes_other_customers",
			filter: &types.SubscriptionFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				CustomerIDs: []string{"cust_other"},
			},
			expectedIDs: []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemorySubscriptionStore()
			require.NoError(t, store.Create(ctx, active))
			require.NoError(t, store.Create(ctx, trialing))
			require.NoError(t, store.Create(ctx, cancelled))

			got, err := store.List(ctx, tc.filter)
			require.NoError(t, err)

			gotIDs := make([]string, 0, len(got))
			for _, sub := range got {
				gotIDs = append(gotIDs, sub.ID)
			}
			require.ElementsMatch(t, tc.expectedIDs, gotIDs)
		})
	}
}
