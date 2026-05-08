package dto

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func baseCreateSubscriptionRequest() CreateSubscriptionRequest {
	return CreateSubscriptionRequest{
		CustomerID:      "cust_test",
		PlanID:          "plan_test",
		Currency:        "usd",
		BillingPeriod:   types.BILLING_PERIOD_MONTHLY,
		BillingCycle:    types.BillingCycleAnniversary,
		StartDate:       nil,
		EndDate:         nil,
		BillingAnchor:   nil,
		PaymentBehavior: nil,
	}
}

func TestCreateSubscriptionRequestValidate_BillingAnchorRequiresAnniversaryBillingCycle(t *testing.T) {
	anchor := time.Now().UTC()

	t.Run("fails when billing_cycle is calendar", func(t *testing.T) {
		req := baseCreateSubscriptionRequest()
		req.BillingCycle = types.BillingCycleCalendar
		req.BillingAnchor = &anchor

		err := req.Validate()
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}

		if !strings.Contains(err.Error(), "billing_anchor") {
			t.Fatalf("expected error to mention billing_anchor, got: %v", err)
		}
	})

	t.Run("passes when billing_cycle is anniversary", func(t *testing.T) {
		req := baseCreateSubscriptionRequest()
		req.BillingCycle = types.BillingCycleAnniversary
		req.BillingAnchor = &anchor

		err := req.Validate()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})
}

func strPtr(s string) *string { return &s }

func TestSubscriptionInheritanceConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *SubscriptionInheritanceConfig
		wantErr bool
	}{
		{
			name: "nil config is valid",
			cfg:  nil,
		},
		{
			name: "delegated requires invoicing_customer_external_id",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior: types.SubscriptionTypeDelegated,
			},
			wantErr: true,
		},
		{
			name: "delegated with parent_subscription_id is invalid",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior:           types.SubscriptionTypeDelegated,
				InvoicingCustomerExternalID: strPtr("cust_ext"),
				ParentSubscriptionID:        "sub_123",
			},
			wantErr: true,
		},
		{
			name: "grouped_invoicing requires parent_subscription_id",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior: types.SubscriptionTypeGroupedInvoicing,
			},
			wantErr: true,
		},
		{
			name: "grouped_invoicing valid",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior:    types.SubscriptionTypeGroupedInvoicing,
				ParentSubscriptionID: "sub_parent_123",
			},
		},
		{
			name: "inherited requires parent_subscription_id",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior: types.SubscriptionTypeInherited,
			},
			wantErr: true,
		},
		{
			name: "inherited rejects invoicing_customer_external_id",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior:           types.SubscriptionTypeInherited,
				ParentSubscriptionID:        "sub_parent_123",
				InvoicingCustomerExternalID: strPtr("cust_ext"),
			},
			wantErr: true,
		},
		{
			name: "parent rejects parent_subscription_id",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior:    types.SubscriptionTypeParent,
				ParentSubscriptionID: "sub_123",
			},
			wantErr: true,
		},
		{
			name: "standalone rejects parent_subscription_id",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior:    types.SubscriptionTypeStandalone,
				ParentSubscriptionID: "sub_123",
			},
			wantErr: true,
		},
		{
			name: "standalone rejects invoicing_customer_external_id",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior:           types.SubscriptionTypeStandalone,
				InvoicingCustomerExternalID: strPtr("cust"),
			},
			wantErr: true,
		},
		{
			name: "legacy path nil behavior with parent_id and external_ids is invalid",
			cfg: &SubscriptionInheritanceConfig{
				ParentSubscriptionID:                     "sub_123",
				ExternalCustomerIDsToInheritSubscription: []string{"cust1"},
			},
			wantErr: true,
		},
		{
			name: "legacy path nil behavior with invoicing_id and external_ids is invalid",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingCustomerExternalID:              strPtr("cust_inv"),
				ExternalCustomerIDsToInheritSubscription: []string{"cust1"},
			},
			wantErr: true,
		},
		{
			name: "legacy path nil behavior with only parent_id is valid",
			cfg: &SubscriptionInheritanceConfig{
				ParentSubscriptionID: "sub_123",
			},
		},
		{
			name: "standalone rejects sub_ids_for_grouped_invoicing",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior:         types.SubscriptionTypeStandalone,
				SubIDsForGroupedInvoicing: []string{"sub_123"},
			},
			wantErr: true,
		},
		{
			name: "unknown invoicing_behavior returns error",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior: types.SubscriptionType("invalid_behavior"),
			},
			wantErr: true,
		},
		{
			name: "delegated rejects sub_ids_for_grouped_invoicing",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior:           types.SubscriptionTypeDelegated,
				InvoicingCustomerExternalID: strPtr("cust_ext"),
				SubIDsForGroupedInvoicing:   []string{"sub_123"},
			},
			wantErr: true,
		},
		{
			name: "delegated rejects external_customer_ids_to_inherit_subscription",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior:                        types.SubscriptionTypeDelegated,
				InvoicingCustomerExternalID:              strPtr("cust_ext"),
				ExternalCustomerIDsToInheritSubscription: []string{"cust1"},
			},
			wantErr: true,
		},
		{
			name: "inherited rejects sub_ids_for_grouped_invoicing",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior:         types.SubscriptionTypeInherited,
				ParentSubscriptionID:      "sub_parent",
				SubIDsForGroupedInvoicing: []string{"sub_123"},
			},
			wantErr: true,
		},
		{
			name: "inherited rejects external_customer_ids_to_inherit_subscription",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior:                        types.SubscriptionTypeInherited,
				ParentSubscriptionID:                     "sub_parent",
				ExternalCustomerIDsToInheritSubscription: []string{"cust1"},
			},
			wantErr: true,
		},
		{
			name: "grouped_invoicing rejects sub_ids_for_grouped_invoicing",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior:         types.SubscriptionTypeGroupedInvoicing,
				ParentSubscriptionID:      "sub_parent",
				SubIDsForGroupedInvoicing: []string{"sub_123"},
			},
			wantErr: true,
		},
		{
			name: "grouped_invoicing rejects external_customer_ids_to_inherit_subscription",
			cfg: &SubscriptionInheritanceConfig{
				InvoicingBehavior:                        types.SubscriptionTypeGroupedInvoicing,
				ParentSubscriptionID:                     "sub_parent",
				ExternalCustomerIDsToInheritSubscription: []string{"cust1"},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCreateSubscriptionRequestValidate_BillingAnchorOnOrAfterStartDate(t *testing.T) {
	start := time.Date(2024, 1, 10, 10, 0, 0, 0, time.UTC)

	t.Run("passes when billing_anchor equals start_date", func(t *testing.T) {
		req := baseCreateSubscriptionRequest()
		req.StartDate = &start
		req.BillingCycle = types.BillingCycleAnniversary
		anchor := time.Date(2024, 1, 10, 10, 0, 0, 0, time.UTC)
		req.BillingAnchor = &anchor

		err := req.Validate()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("passes when billing_anchor is after start_date", func(t *testing.T) {
		req := baseCreateSubscriptionRequest()
		req.StartDate = &start
		req.BillingCycle = types.BillingCycleAnniversary
		anchor := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
		req.BillingAnchor = &anchor

		err := req.Validate()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})
}
