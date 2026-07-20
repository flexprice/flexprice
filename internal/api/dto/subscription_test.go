package dto

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
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

func TestCancelSubscriptionRequest_Validate_BackdatedImmediate(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-5 * 24 * time.Hour)
	future := now.Add(5 * 24 * time.Hour)

	tests := []struct {
		name    string
		req     CancelSubscriptionRequest
		wantErr bool
	}{
		{
			name: "immediate_no_cancel_at_is_valid",
			req: CancelSubscriptionRequest{
				CancellationType:  types.CancellationTypeImmediate,
				ProrationBehavior: types.ProrationBehaviorNone,
			},
			wantErr: false,
		},
		{
			name: "immediate_past_cancel_at_is_valid",
			req: CancelSubscriptionRequest{
				CancellationType:  types.CancellationTypeImmediate,
				ProrationBehavior: types.ProrationBehaviorNone,
				CancelAt:          &past,
			},
			wantErr: false,
		},
		{
			name: "immediate_future_cancel_at_is_rejected",
			req: CancelSubscriptionRequest{
				CancellationType:  types.CancellationTypeImmediate,
				ProrationBehavior: types.ProrationBehaviorNone,
				CancelAt:          &future,
			},
			wantErr: true,
		},
		{
			name: "scheduled_date_past_cancel_at_is_valid",
			req: CancelSubscriptionRequest{
				CancellationType:  types.CancellationTypeScheduledDate,
				ProrationBehavior: types.ProrationBehaviorNone,
				CancelAt:          &past,
			},
			wantErr: false,
		},
		{
			name: "scheduled_date_future_cancel_at_is_valid",
			req: CancelSubscriptionRequest{
				CancellationType:  types.CancellationTypeScheduledDate,
				ProrationBehavior: types.ProrationBehaviorNone,
				CancelAt:          &future,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

func TestCreateSubscriptionRequestValidate_AutoInvoiceThreshold(t *testing.T) {
	t.Run("nil passes", func(t *testing.T) {
		req := baseCreateSubscriptionRequest()
		if err := req.Validate(); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("zero passes", func(t *testing.T) {
		req := baseCreateSubscriptionRequest()
		z := decimal.Zero
		req.AutoInvoiceThreshold = &z
		if err := req.Validate(); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("positive passes", func(t *testing.T) {
		req := baseCreateSubscriptionRequest()
		p := decimal.RequireFromString("10")
		req.AutoInvoiceThreshold = &p
		if err := req.Validate(); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("negative fails mentioning auto_invoice_threshold", func(t *testing.T) {
		req := baseCreateSubscriptionRequest()
		n := decimal.NewFromInt(-1)
		req.AutoInvoiceThreshold = &n
		err := req.Validate()
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "auto_invoice_threshold") {
			t.Fatalf("expected error to mention auto_invoice_threshold, got: %v", err)
		}
	})
}

func TestSubscriptionInheritanceConfig_Validate_GroupedInvoicingChildrenToCreate(t *testing.T) {
	t.Run("rejects combining with subscriptions_ids_for_grouped_invoicing", func(t *testing.T) {
		c := &SubscriptionInheritanceConfig{
			GroupedInvoicingChildrenToCreate: []GroupedInvoicingChildRequest{
				{PlanID: "plan_seat", ExternalCustomerID: "ext_seat_1"},
			},
			SubscriptionsIDsForGroupedInvoicing: []string{"sub_existing_1"},
		}

		err := c.Validate()
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		if !strings.Contains(err.Error(), "grouped_invoicing_children_to_create") {
			t.Fatalf("expected error to mention grouped_invoicing_children_to_create, got: %v", err)
		}
	})

	t.Run("passes with only grouped_invoicing_children_to_create set", func(t *testing.T) {
		c := &SubscriptionInheritanceConfig{
			GroupedInvoicingChildrenToCreate: []GroupedInvoicingChildRequest{
				{PlanID: "plan_seat", ExternalCustomerID: "ext_seat_1"},
			},
		}

		err := c.Validate()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("nil config still passes", func(t *testing.T) {
		var c *SubscriptionInheritanceConfig
		if err := c.Validate(); err != nil {
			t.Fatalf("expected no error for nil config, got: %v", err)
		}
	})
}

func TestCreateSubscriptionRequestValidate_GroupedInvoicingChildrenToCreate_RequiredFields(t *testing.T) {
	t.Run("rejects a child missing plan_id", func(t *testing.T) {
		req := baseCreateSubscriptionRequest()
		req.Inheritance = &SubscriptionInheritanceConfig{
			GroupedInvoicingChildrenToCreate: []GroupedInvoicingChildRequest{
				{ExternalCustomerID: "ext_seat_1"},
			},
		}

		err := req.Validate()
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
	})

	t.Run("rejects a child missing external_customer_id", func(t *testing.T) {
		req := baseCreateSubscriptionRequest()
		req.Inheritance = &SubscriptionInheritanceConfig{
			GroupedInvoicingChildrenToCreate: []GroupedInvoicingChildRequest{
				{PlanID: "plan_seat"},
			},
		}

		err := req.Validate()
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
	})

	t.Run("passes with both fields set", func(t *testing.T) {
		req := baseCreateSubscriptionRequest()
		req.Inheritance = &SubscriptionInheritanceConfig{
			GroupedInvoicingChildrenToCreate: []GroupedInvoicingChildRequest{
				{PlanID: "plan_seat", ExternalCustomerID: "ext_seat_1"},
			},
		}

		if err := req.Validate(); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})
}
