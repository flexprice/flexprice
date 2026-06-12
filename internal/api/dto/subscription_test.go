package dto

import (
	"encoding/json"
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

func TestCancelSubscriptionRequest_UnmarshalJSON_InvoicePolicySpellings(t *testing.T) {
	tests := []struct {
		name string
		body string
		want types.CancelImmediatelyInvoicePolicy
	}{
		{
			name: "corrected_key_is_accepted",
			body: `{"cancellation_type":"immediate","cancel_immediately_invoice_policy":"generate_invoice"}`,
			want: types.CancelImmediatelyInvoicePolicyGenerateInvoice,
		},
		{
			name: "legacy_misspelled_key_is_accepted",
			body: `{"cancellation_type":"immediate","cancel_immediately_inovice_policy":"generate_invoice"}`,
			want: types.CancelImmediatelyInvoicePolicyGenerateInvoice,
		},
		{
			name: "corrected_key_wins_when_both_present",
			body: `{"cancellation_type":"immediate","cancel_immediately_invoice_policy":"generate_invoice","cancel_immediately_inovice_policy":"skip"}`,
			want: types.CancelImmediatelyInvoicePolicyGenerateInvoice,
		},
		{
			name: "absent_key_leaves_policy_empty",
			body: `{"cancellation_type":"immediate"}`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req CancelSubscriptionRequest
			if err := json.Unmarshal([]byte(tt.body), &req); err != nil {
				t.Fatalf("unexpected unmarshal error: %v", err)
			}
			if req.CancelImmediatelyInvoicePolicy != tt.want {
				t.Fatalf("expected policy %q, got %q", tt.want, req.CancelImmediatelyInvoicePolicy)
			}
			if req.CancellationType != types.CancellationTypeImmediate {
				t.Fatalf("expected cancellation_type to be preserved, got %q", req.CancellationType)
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
