package dto

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func TestLineItemCommitmentConfig_Validate_OverageFactor(t *testing.T) {
	amount := decimal.NewFromInt(100)

	t.Run("accepts overage factor of exactly 1.0", func(t *testing.T) {
		c := &LineItemCommitmentConfig{
			CommitmentAmount: &amount,
			CommitmentType:   types.COMMITMENT_TYPE_AMOUNT,
			OverageFactor:    lo.ToPtr(decimal.NewFromInt(1)),
		}
		if err := c.Validate(); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("rejects overage factor below 1.0", func(t *testing.T) {
		c := &LineItemCommitmentConfig{
			CommitmentAmount: &amount,
			CommitmentType:   types.COMMITMENT_TYPE_AMOUNT,
			OverageFactor:    lo.ToPtr(decimal.NewFromFloat(0.5)),
		}
		err := c.Validate()
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		if !strings.Contains(err.Error(), "overage_factor must be at least 1.0") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

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

// TestCreateSubscriptionRequestValidate_BillingAnchorWithinOneInterval covers the bound that
// keeps the first billing period from exceeding one interval.
//
// Only QUARTERLY, HALF_YEARLY and ANNUAL are bounded, because only those consume the anchor as
// an absolute instant and return it verbatim as the first period end. MONTHLY, WEEKLY and DAILY
// read the anchor as a recurring pattern (day-of-month, weekday, clock), so a far-future anchor
// cannot stretch their first period and must keep validating.
func TestCreateSubscriptionRequestValidate_BillingAnchorWithinOneInterval(t *testing.T) {
	start := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		period      types.BillingPeriod
		periodCount int
		anchor      time.Time
		wantErr     bool
	}{
		{
			name:    "annual accepts an anchor inside the first year",
			period:  types.BILLING_PERIOD_ANNUAL,
			anchor:  time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "annual accepts an anchor exactly one interval out",
			period:  types.BILLING_PERIOD_ANNUAL,
			anchor:  time.Date(2027, 3, 2, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "annual rejects an anchor beyond one year",
			period:  types.BILLING_PERIOD_ANNUAL,
			anchor:  time.Date(2027, 4, 2, 0, 0, 0, 0, time.UTC),
			wantErr: true,
		},
		{
			name:    "annual accepts an anchor before start_date, which carries only month and day",
			period:  types.BILLING_PERIOD_ANNUAL,
			anchor:  time.Date(2022, 3, 2, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:        "annual bound scales with billing_period_count",
			period:      types.BILLING_PERIOD_ANNUAL,
			periodCount: 2,
			anchor:      time.Date(2027, 9, 2, 0, 0, 0, 0, time.UTC),
			wantErr:     false,
		},
		{
			name:    "quarterly accepts an anchor inside the first quarter",
			period:  types.BILLING_PERIOD_QUARTER,
			anchor:  time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			// Stripe rejects this same request with "billing_cycle_anchor cannot be later
			// than next natural billing date"; without the bound we produced an
			// eight-month first period on a quarterly plan.
			name:    "quarterly rejects an anchor eight months out",
			period:  types.BILLING_PERIOD_QUARTER,
			anchor:  time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
			wantErr: true,
		},
		{
			name:    "half yearly accepts an anchor inside the first half year",
			period:  types.BILLING_PERIOD_HALF_YEAR,
			anchor:  time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "half yearly rejects an anchor beyond six months",
			period:  types.BILLING_PERIOD_HALF_YEAR,
			anchor:  time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
			wantErr: true,
		},
		{
			name:    "monthly is unbounded because the anchor supplies only day-of-month",
			period:  types.BILLING_PERIOD_MONTHLY,
			anchor:  time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "weekly is unbounded because the anchor supplies only weekday and clock",
			period:  types.BILLING_PERIOD_WEEKLY,
			anchor:  time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "daily is unbounded because the anchor supplies only clock",
			period:  types.BILLING_PERIOD_DAILY,
			anchor:  time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := baseCreateSubscriptionRequest()
			req.BillingCycle = types.BillingCycleAnniversary
			req.BillingPeriod = tt.period
			req.BillingPeriodCount = tt.periodCount
			req.StartDate = &start
			anchor := tt.anchor
			req.BillingAnchor = &anchor

			err := req.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected validation error, got nil")
				}
				if !strings.Contains(err.Error(), "more than one billing period") {
					t.Fatalf("expected the one-interval bound to reject this, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
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
