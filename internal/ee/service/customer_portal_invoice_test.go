package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// validateInvoiceIsPayable is the whole portal-side guard: everything past it takes
// real money, so each refusal is pinned here rather than left to the gateway.
func TestValidateInvoiceIsPayable(t *testing.T) {
	payable := func(mut func(*dto.InvoiceResponse)) *dto.InvoiceResponse {
		inv := &dto.InvoiceResponse{
			Invoice: invoice.Invoice{
				ID:              "inv_1",
				InvoiceStatus:   types.InvoiceStatusFinalized,
				PaymentStatus:   types.PaymentStatusPending,
				AmountRemaining: decimal.NewFromInt(25),
				Currency:        "usd",
				BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
			},
		}
		if mut != nil {
			mut(inv)
		}
		return inv
	}

	tests := []struct {
		name    string
		invoice *dto.InvoiceResponse
		wantErr bool
	}{
		{name: "finalized and unpaid", invoice: payable(nil)},
		{
			name:    "failed payment may be retried",
			invoice: payable(func(i *dto.InvoiceResponse) { i.PaymentStatus = types.PaymentStatusFailed }),
		},
		{
			name:    "draft is not payable",
			invoice: payable(func(i *dto.InvoiceResponse) { i.InvoiceStatus = types.InvoiceStatusDraft }),
			wantErr: true,
		},
		{
			name:    "voided is not payable",
			invoice: payable(func(i *dto.InvoiceResponse) { i.InvoiceStatus = types.InvoiceStatusVoided }),
			wantErr: true,
		},
		{
			name:    "already succeeded",
			invoice: payable(func(i *dto.InvoiceResponse) { i.PaymentStatus = types.PaymentStatusSucceeded }),
			wantErr: true,
		},
		{
			name:    "nothing remaining",
			invoice: payable(func(i *dto.InvoiceResponse) { i.AmountRemaining = decimal.Zero }),
			wantErr: true,
		},
		{
			// Activation is handled separately, by subscription status — a
			// subscription_create invoice is not refused on its billing reason alone.
			name: "subscription create invoice passes the payable checks",
			invoice: payable(func(i *dto.InvoiceResponse) {
				i.BillingReason = string(types.InvoiceBillingReasonSubscriptionCreate)
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInvoiceIsPayable(tt.invoice)
			if tt.wantErr && err == nil {
				t.Fatal("expected the invoice to be refused, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected the invoice to be payable, got %v", err)
			}
		})
	}
}

func TestPaymentActionFrom(t *testing.T) {
	if got := paymentActionFrom(&dto.PaymentResponse{}); got != nil {
		t.Fatalf("no gateway metadata should yield no action, got %+v", got)
	}
	if got := paymentActionFrom(&dto.PaymentResponse{
		GatewayMetadata: types.Metadata{"gateway": "chargebee"},
	}); got != nil {
		t.Fatalf("metadata without a url should yield no action, got %+v", got)
	}

	got := paymentActionFrom(&dto.PaymentResponse{
		GatewayMetadata: types.Metadata{"payment_url": "https://pay.example/1"},
	})
	if got == nil || got.URL != "https://pay.example/1" {
		t.Fatalf("expected the payment url to surface, got %+v", got)
	}
	if got.Type != types.PaymentActionTypePaymentLink {
		t.Errorf("Type = %q, want payment_link", got.Type)
	}
}

// The activation guard must be narrow: only an invoice whose payment is meant to
// activate a subscription still sitting unactivated is refused.
func (s *PortalWalletSuite) TestPayInvoiceActivationGuard() {
	portal := s.svc.(*customerPortalService)

	tests := []struct {
		name       string
		reason     types.InvoiceBillingReason
		status     types.SubscriptionStatus
		wantRefuse bool
	}{
		{"incomplete subscription is refused", types.InvoiceBillingReasonSubscriptionCreate, types.SubscriptionStatusIncomplete, true},
		{"active subscription is payable", types.InvoiceBillingReasonSubscriptionCreate, types.SubscriptionStatusActive, false},
		{"renewal is payable", types.InvoiceBillingReasonSubscriptionCycle, types.SubscriptionStatusActive, false},
		{"trial end before activation is refused", types.InvoiceBillingReasonSubscriptionTrialEnd, types.SubscriptionStatusTrialing, true},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			subID := "sub_" + string(tt.reason) + "_" + string(tt.status)
			s.NoError(s.GetStores().SubscriptionRepo.Create(s.ctx, &subscription.Subscription{
				ID:                 subID,
				CustomerID:         "cust_portal",
				SubscriptionStatus: tt.status,
				Currency:           "usd",
				BaseModel:          types.GetDefaultBaseModel(s.ctx),
			}))

			inv := &dto.InvoiceResponse{Invoice: invoice.Invoice{
				ID:             "inv_" + subID,
				SubscriptionID: lo.ToPtr(subID),
				BillingReason:  string(tt.reason),
			}}

			err := portal.shouldAllowToPayInvoice(s.ctx, inv)
			if tt.wantRefuse {
				s.Error(err, "payment would settle the invoice and leave the subscription behind")
				return
			}
			s.NoError(err)
		})
	}
}

// A one-off invoice has no subscription to activate and must never be refused.
func (s *PortalWalletSuite) TestPayInvoiceActivationGuardIgnoresOneOffInvoices() {
	portal := s.svc.(*customerPortalService)

	err := portal.shouldAllowToPayInvoice(s.ctx, &dto.InvoiceResponse{
		Invoice: invoice.Invoice{ID: "inv_oneoff", BillingReason: string(types.InvoiceBillingReasonSubscriptionCreate)},
	})
	s.NoError(err)
}
