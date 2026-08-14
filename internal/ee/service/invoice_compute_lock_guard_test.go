package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ComputeInvoiceLockGuardSuite verifies a manually-edited draft invoice is a full
// no-op in ComputeInvoice, while a non-edited draft still computes normally.
type ComputeInvoiceLockGuardSuite struct {
	testutil.BaseServiceTestSuite
	service InvoiceService
}

func TestComputeInvoiceLockGuard(t *testing.T) {
	suite.Run(t, new(ComputeInvoiceLockGuardSuite))
}

func (s *ComputeInvoiceLockGuardSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewInvoiceService(ServiceParams{
		Logger:              s.GetLogger(),
		Config:              s.GetConfig(),
		DB:                  s.GetDB(),
		InvoiceRepo:         s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo: s.GetStores().InvoiceLineItemRepo,
		WebhookPublisher:    s.GetWebhookPublisher(),
	})
}

// createDraftInvoice creates a draft, non-subscription-linked "subscription type" invoice
// (InvoiceType = Subscription, SubscriptionID = nil) with one published line item.
//
// Using InvoiceType Subscription with a nil SubscriptionID keeps ComputeInvoice on the
// cheap path: the pre-lock billing-service branch is skipped (it requires SubscriptionID),
// and post-lock only coupons are applied (credits/taxes are deferred to finalization for
// subscription invoices), so this test needs no wallet/tax/coupon repos wired up.
func (s *ComputeInvoiceLockGuardSuite) createDraftInvoice(ctx context.Context, isManuallyEdited bool) *invoice.Invoice {
	inv := &invoice.Invoice{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:       "cust_test",
		InvoiceType:      types.InvoiceTypeSubscription,
		InvoiceStatus:    types.InvoiceStatusDraft,
		Currency:         "usd",
		Subtotal:         decimal.NewFromInt(100),
		Total:            decimal.NewFromInt(100),
		AmountDue:        decimal.NewFromInt(100),
		IsManuallyEdited: isManuallyEdited,
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
				DisplayName: lo.ToPtr("Original Item"),
				Amount:      decimal.NewFromInt(100),
				Quantity:    decimal.NewFromInt(1),
				Currency:    "usd",
				BaseModel:   types.GetDefaultBaseModel(ctx),
			},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(ctx, inv))
	return inv
}

// computeReqWithDifferentLineItem builds a compute request whose line items/totals differ
// from createDraftInvoice's seed data, so a successful (non-guarded) compute is observable.
func computeReqWithDifferentLineItem() *dto.InvoiceComputeRequest {
	return &dto.InvoiceComputeRequest{
		LineItems: []dto.CreateInvoiceLineItemRequest{
			{
				DisplayName: lo.ToPtr("Recomputed Item"),
				Amount:      decimal.NewFromInt(250),
				Quantity:    decimal.NewFromInt(1),
			},
		},
		Subtotal:  decimal.NewFromInt(250),
		Total:     decimal.NewFromInt(250),
		AmountDue: decimal.NewFromInt(250),
	}
}

func (s *ComputeInvoiceLockGuardSuite) TestSkipsComputeWhenManuallyEdited() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, true)

	result, skipped, err := s.service.ComputeInvoice(ctx, inv.ID, computeReqWithDifferentLineItem())
	s.NoError(err)
	s.False(skipped)
	s.Require().NotNil(result)

	// Nothing about the invoice changed: totals, line items, and status are untouched.
	s.True(result.Subtotal.Equal(decimal.NewFromInt(100)))
	s.True(result.Total.Equal(decimal.NewFromInt(100)))
	s.True(result.AmountDue.Equal(decimal.NewFromInt(100)))
	s.Equal(types.InvoiceStatusDraft, result.InvoiceStatus)
	s.Nil(result.LastComputedAt)
	s.Require().Len(result.LineItems, 1)
	s.Equal("Original Item", lo.FromPtr(result.LineItems[0].DisplayName))
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(100)))

	// The DB row itself was never written to.
	stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.True(stored.Subtotal.Equal(decimal.NewFromInt(100)))
	s.Require().Len(stored.LineItems, 1)
	s.Equal("Original Item", lo.FromPtr(stored.LineItems[0].DisplayName))
	s.True(stored.IsManuallyEdited)

	// The no-op must not fire the "invoice updated" system event.
	s.Empty(s.GetPublishedWebhooks())
}

// TestComputesNormallyWhenNotManuallyEdited is the regression check: a draft invoice
// that has never been manually edited must still be recomputed exactly as before this
// guard was introduced.
func (s *ComputeInvoiceLockGuardSuite) TestComputesNormallyWhenNotManuallyEdited() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, false)

	result, skipped, err := s.service.ComputeInvoice(ctx, inv.ID, computeReqWithDifferentLineItem())
	s.NoError(err)
	s.False(skipped)
	s.Require().NotNil(result)

	s.True(result.Subtotal.Equal(decimal.NewFromInt(250)))
	s.True(result.Total.Equal(decimal.NewFromInt(250)))
	s.True(result.AmountDue.Equal(decimal.NewFromInt(250)))
	s.NotNil(result.LastComputedAt)
	s.Require().Len(result.LineItems, 1)
	s.Equal("Recomputed Item", lo.FromPtr(result.LineItems[0].DisplayName))
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(250)))

	stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.True(stored.Subtotal.Equal(decimal.NewFromInt(250)))
	s.Require().Len(stored.LineItems, 1)
	s.Equal("Recomputed Item", lo.FromPtr(stored.LineItems[0].DisplayName))
	s.False(stored.IsManuallyEdited)

	// Normal compute still notifies downstream listeners.
	s.Len(s.GetPublishedWebhooks(), 1)
}
