package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// RemoveLineItemSuite tests the soft-delete RemoveLineItem service method.
type RemoveLineItemSuite struct {
	testutil.BaseServiceTestSuite
	service InvoiceService
}

func TestRemoveLineItem(t *testing.T) {
	suite.Run(t, new(RemoveLineItemSuite))
}

func (s *RemoveLineItemSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewInvoiceService(ServiceParams{
		Logger:              s.GetLogger(),
		Config:              s.GetConfig(),
		DB:                  s.GetDB(),
		InvoiceRepo:         s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo: s.GetStores().InvoiceLineItemRepo,
	})
}

// createDraftInvoiceWithLineItem creates a draft invoice with a single published
// line item, and returns both.
func (s *RemoveLineItemSuite) createDraftInvoiceWithLineItem(ctx context.Context, amount, quantity decimal.Decimal) (*invoice.Invoice, *invoice.InvoiceLineItem) {
	inv := &invoice.Invoice{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:    "cust_test",
		InvoiceType:   types.InvoiceTypeSubscription,
		InvoiceStatus: types.InvoiceStatusDraft,
		Currency:      "usd",
		Subtotal:      amount,
		Total:         amount,
		AmountDue:     amount,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(ctx, inv))

	li := &invoice.InvoiceLineItem{
		ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:   inv.ID,
		CustomerID:  inv.CustomerID,
		DisplayName: lo.ToPtr("Original Name"),
		Amount:      amount,
		Quantity:    quantity,
		Currency:    "usd",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, li))

	return inv, li
}

func (s *RemoveLineItemSuite) TestSoftDeletesLineItem() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	resp, err := s.service.RemoveLineItem(ctx, inv.ID, li.ID)
	s.NoError(err)
	s.NotNil(resp)

	// Row still exists (soft delete, not physically gone) with status=deleted.
	row, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, li.ID)
	s.NoError(err)
	s.Equal(types.StatusDeleted, row.Status)

	// Excluded from ListByInvoiceID's default (published-only) filter.
	published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Len(published, 0)

	// Response reflects the now-empty line item set.
	s.Len(resp.LineItems, 0)

	// Invoice flagged as manually edited.
	updatedInv, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.True(updatedInv.IsManuallyEdited)
}

func (s *RemoveLineItemSuite) TestRecalculatesTotalsExcludingRemovedItem() {
	ctx := s.GetContext()
	inv, li1 := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	li2 := &invoice.InvoiceLineItem{
		ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:   inv.ID,
		CustomerID:  inv.CustomerID,
		DisplayName: lo.ToPtr("Second Item"),
		Amount:      decimal.NewFromInt(50),
		Quantity:    decimal.NewFromInt(1),
		Currency:    "usd",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, li2))

	resp, err := s.service.RemoveLineItem(ctx, inv.ID, li1.ID)
	s.NoError(err)

	s.True(resp.Subtotal.Equal(decimal.NewFromInt(50)))
	s.True(resp.Total.Equal(decimal.NewFromInt(50)))
	s.True(resp.AmountDue.Equal(decimal.NewFromInt(50)))
	s.Require().Len(resp.LineItems, 1)
	s.Equal(li2.ID, resp.LineItems[0].ID)
}

func (s *RemoveLineItemSuite) TestRejectsRemoveOnFinalizedInvoice() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	inv.InvoiceStatus = types.InvoiceStatusFinalized
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

	_, err := s.service.RemoveLineItem(ctx, inv.ID, li.ID)
	s.Error(err)
	s.True(ierr.IsValidation(err))

	// No change made: line item still published.
	unchanged, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, li.ID)
	s.NoError(err)
	s.Equal(types.StatusPublished, unchanged.Status)
}

func (s *RemoveLineItemSuite) TestRejectsLineItemFromDifferentInvoice() {
	ctx := s.GetContext()
	inv1, _ := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))
	inv2, li2 := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(200), decimal.NewFromInt(5))
	_ = inv2

	_, err := s.service.RemoveLineItem(ctx, inv1.ID, li2.ID)
	s.Error(err)
	s.True(ierr.IsNotFound(err))

	// Untouched: line item from inv2 remains published.
	unchanged, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, li2.ID)
	s.NoError(err)
	s.Equal(types.StatusPublished, unchanged.Status)
}

func (s *RemoveLineItemSuite) TestRejectsRemoveOnAlreadyDeletedLineItem() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	_, err := s.service.RemoveLineItem(ctx, inv.ID, li.ID)
	s.Require().NoError(err)

	// Removing the same (now-deleted) line item again must be rejected, not silently succeed.
	_, err = s.service.RemoveLineItem(ctx, inv.ID, li.ID)
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *RemoveLineItemSuite) TestRejectsRemoveOnNonExistentLineItem() {
	ctx := s.GetContext()
	inv, _ := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	_, err := s.service.RemoveLineItem(ctx, inv.ID, "li_does_not_exist")
	s.Error(err)
	s.True(ierr.IsNotFound(err))
}
