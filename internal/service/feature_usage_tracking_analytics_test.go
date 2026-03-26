package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- resolveEffectiveExternalIDs ---

func TestResolveEffectiveExternalIDs(t *testing.T) {
	tests := []struct {
		name     string
		req      *dto.GetUsageAnalyticsRequest
		expected []string
	}{
		{
			name:     "only ExternalCustomerID",
			req:      &dto.GetUsageAnalyticsRequest{ExternalCustomerID: "cust_a"},
			expected: []string{"cust_a"},
		},
		{
			name:     "only ExternalCustomerIDs",
			req:      &dto.GetUsageAnalyticsRequest{ExternalCustomerIDs: []string{"cust_b", "cust_c"}},
			expected: []string{"cust_b", "cust_c"},
		},
		{
			name: "both fields, no overlap",
			req: &dto.GetUsageAnalyticsRequest{
				ExternalCustomerID:  "cust_a",
				ExternalCustomerIDs: []string{"cust_b", "cust_c"},
			},
			expected: []string{"cust_a", "cust_b", "cust_c"},
		},
		{
			name: "both fields, with duplicate — each unique ID appears once",
			req: &dto.GetUsageAnalyticsRequest{
				ExternalCustomerID:  "cust_a",
				ExternalCustomerIDs: []string{"cust_a", "cust_b"},
			},
			expected: []string{"cust_a", "cust_b"},
		},
		{
			name:     "both empty returns nil",
			req:      &dto.GetUsageAnalyticsRequest{},
			expected: nil,
		},
		{
			name:     "empty string in ExternalCustomerID is ignored",
			req:      &dto.GetUsageAnalyticsRequest{ExternalCustomerID: "", ExternalCustomerIDs: []string{"cust_x"}},
			expected: []string{"cust_x"},
		},
		{
			name:     "duplicates within ExternalCustomerIDs are deduplicated",
			req:      &dto.GetUsageAnalyticsRequest{ExternalCustomerIDs: []string{"cust_a", "cust_a", "cust_b"}},
			expected: []string{"cust_a", "cust_b"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveEffectiveExternalIDs(tc.req)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// --- validateAnalyticsRequest ---

func TestValidateAnalyticsRequest(t *testing.T) {
	svc := &featureUsageTrackingService{}

	t.Run("both fields empty returns validation error", func(t *testing.T) {
		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{})
		require.Error(t, err)
		assert.True(t, ierr.IsValidation(err))
	})

	t.Run("only ExternalCustomerID set is valid", func(t *testing.T) {
		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{ExternalCustomerID: "cust_a"})
		assert.NoError(t, err)
	})

	t.Run("only ExternalCustomerIDs set is valid", func(t *testing.T) {
		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{ExternalCustomerIDs: []string{"cust_b"}})
		assert.NoError(t, err)
	})

	t.Run("both fields set is valid", func(t *testing.T) {
		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{
			ExternalCustomerID:  "cust_a",
			ExternalCustomerIDs: []string{"cust_b"},
		})
		assert.NoError(t, err)
	})

	t.Run("ExternalCustomerIDs with only empty strings is a validation error", func(t *testing.T) {
		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{
			ExternalCustomerIDs: []string{"", ""},
		})
		require.Error(t, err)
		assert.True(t, ierr.IsValidation(err))
	})
}

// Note: scenarios requiring real DB or mocked repos (currency mismatch across customers,
// all-customers-fail → empty response, IncludeChildren expansion) are covered by
// integration tests in the standard test suite (testutil.SetupTestDB). They are not
// duplicated here as unit tests because fetchAnalyticsData, fetchCustomer, and
// fetchChildCustomers all require repo implementations.

// --- resolveHierarchyLineItems ---

func TestResolveHierarchyLineItems(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env_test")
	ctx = context.WithValue(ctx, types.CtxUserID, types.DefaultUserID)
	ctx = context.WithValue(ctx, types.CtxRequestID, types.GenerateUUID())

	newSub := func(id string, subType types.SubscriptionType, parentSubID *string) *subscription.Subscription {
		return &subscription.Subscription{
			ID:                   id,
			SubscriptionType:     subType,
			ParentSubscriptionID: parentSubID,
			CustomerID:           "cust_child",
			SubscriptionStatus:   types.SubscriptionStatusActive,
			BaseModel:            types.GetDefaultBaseModel(ctx),
		}
	}

	newLineItem := func(id, subID, meterID string) *subscription.SubscriptionLineItem {
		return &subscription.SubscriptionLineItem{
			ID:             id,
			SubscriptionID: subID,
			MeterID:        meterID,
			PriceType:      types.PRICE_TYPE_USAGE,
			StartDate:      time.Now().UTC(),
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}
	}

	t.Run("no subscriptions — returns empty slice", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		lineItemStore := testutil.NewInMemorySubscriptionLineItemStore()
		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{
				SubRepo:                  subStore,
				SubscriptionLineItemRepo: lineItemStore,
			},
		}
		items, err := svc.resolveHierarchyLineItems(ctx, "cust_child", []string{"meter_1"}, nil)
		require.NoError(t, err)
		assert.Empty(t, items)
	})

	t.Run("standalone subscription for customer — returns empty slice", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		lineItemStore := testutil.NewInMemorySubscriptionLineItemStore()
		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{
				SubRepo:                  subStore,
				SubscriptionLineItemRepo: lineItemStore,
			},
		}
		standalone := newSub("sub_standalone", types.SubscriptionTypeStandalone, nil)
		standalone.CustomerID = "cust_child"
		require.NoError(t, subStore.Create(ctx, standalone))

		items, err := svc.resolveHierarchyLineItems(ctx, "cust_child", []string{"meter_1"}, nil)
		require.NoError(t, err)
		assert.Empty(t, items)
	})

	t.Run("inherited subscription with parent line items — returns matching meter line items", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		lineItemStore := testutil.NewInMemorySubscriptionLineItemStore()
		parentSub := newSub("sub_parent_1", types.SubscriptionTypeParent, nil)
		parentSub.CustomerID = "cust_parent"
		require.NoError(t, subStore.Create(ctx, parentSub))

		inherited := newSub("sub_inherited_1", types.SubscriptionTypeInherited, lo.ToPtr("sub_parent_1"))
		inherited.CustomerID = "cust_child"
		require.NoError(t, subStore.Create(ctx, inherited))

		require.NoError(t, lineItemStore.Create(ctx, newLineItem("li_match", "sub_parent_1", "meter_1")))
		require.NoError(t, lineItemStore.Create(ctx, newLineItem("li_other", "sub_parent_1", "meter_2")))

		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{
				SubRepo:                  subStore,
				SubscriptionLineItemRepo: lineItemStore,
			},
		}
		items, err := svc.resolveHierarchyLineItems(ctx, "cust_child", []string{"meter_1"}, nil)
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, "li_match", items[0].ID)
	})

	t.Run("two inherited subs pointing to same parent — line items returned once from parent scope", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		lineItemStore := testutil.NewInMemorySubscriptionLineItemStore()
		parentSub := newSub("sub_parent_shared", types.SubscriptionTypeParent, nil)
		parentSub.CustomerID = "cust_parent"
		require.NoError(t, subStore.Create(ctx, parentSub))

		inhA := newSub("sub_inh_a", types.SubscriptionTypeInherited, lo.ToPtr("sub_parent_shared"))
		inhA.CustomerID = "cust_child"
		inhB := newSub("sub_inh_b", types.SubscriptionTypeInherited, lo.ToPtr("sub_parent_shared"))
		inhB.CustomerID = "cust_child"
		require.NoError(t, subStore.Create(ctx, inhA))
		require.NoError(t, subStore.Create(ctx, inhB))

		require.NoError(t, lineItemStore.Create(ctx, newLineItem("li_shared", "sub_parent_shared", "meter_1")))

		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{
				SubRepo:                  subStore,
				SubscriptionLineItemRepo: lineItemStore,
			},
		}
		items, err := svc.resolveHierarchyLineItems(ctx, "cust_child", []string{"meter_1"}, nil)
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, "li_shared", items[0].ID)
	})

	t.Run("inherited subscription with missing parent pointer — returns empty", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		lineItemStore := testutil.NewInMemorySubscriptionLineItemStore()
		inh := newSub("sub_inh_missing_parent", types.SubscriptionTypeInherited, nil)
		inh.CustomerID = "cust_child"
		require.NoError(t, subStore.Create(ctx, inh))
		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{
				SubRepo:                  subStore,
				SubscriptionLineItemRepo: lineItemStore,
			},
		}
		items, err := svc.resolveHierarchyLineItems(ctx, "cust_child", []string{"meter_1"}, nil)
		require.NoError(t, err)
		assert.Empty(t, items)
	})

}
