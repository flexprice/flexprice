package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// AddLineItemSuite tests the AddLineItem service method (T-09).
type AddLineItemSuite struct {
	testutil.BaseServiceTestSuite
	service InvoiceService
}

func TestAddLineItem(t *testing.T) {
	suite.Run(t, new(AddLineItemSuite))
}

func (s *AddLineItemSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewInvoiceService(ServiceParams{
		Logger:              s.GetLogger(),
		Config:              s.GetConfig(),
		DB:                  s.GetDB(),
		InvoiceRepo:         s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo: s.GetStores().InvoiceLineItemRepo,
	})
}

// createDraftInvoice creates a draft invoice with no line items and returns it.
func (s *AddLineItemSuite) createDraftInvoice(ctx context.Context, amount decimal.Decimal) *invoice.Invoice {
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
	return inv
}

func (s *AddLineItemSuite) TestAddsNewPublishedLineItem() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.Zero)

	resp, err := s.service.AddLineItem(ctx, inv.ID, dto.AddLineItemRequest{
		DisplayName: "New Item",
		Amount:      decimal.NewFromInt(100),
		Quantity:    decimal.NewFromInt(2),
	})
	s.NoError(err)
	s.NotNil(resp)

	published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Require().Len(published, 1)
	newRow := published[0]

	s.Equal("New Item", lo.FromPtr(newRow.DisplayName))
	s.True(newRow.Amount.Equal(decimal.NewFromInt(100)))
	s.True(newRow.Quantity.Equal(decimal.NewFromInt(2)))
	s.Equal(inv.Currency, newRow.Currency)
	s.Equal(inv.CustomerID, newRow.CustomerID)
	s.Equal(inv.ID, newRow.InvoiceID)
	s.Nil(newRow.ParentLineItemID)
	s.Equal(types.StatusPublished, newRow.Status)
}

func (s *AddLineItemSuite) TestRecalculatesTotalsAndFlagsManuallyEdited() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.NewFromInt(50))

	// Seed one existing published line item so the new total is a sum, not just the addition.
	existing := &invoice.InvoiceLineItem{
		ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:   inv.ID,
		CustomerID:  inv.CustomerID,
		DisplayName: lo.ToPtr("Existing Item"),
		Amount:      decimal.NewFromInt(50),
		Quantity:    decimal.NewFromInt(1),
		Currency:    inv.Currency,
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, existing))

	resp, err := s.service.AddLineItem(ctx, inv.ID, dto.AddLineItemRequest{
		DisplayName: "New Item",
		Amount:      decimal.NewFromInt(100),
		Quantity:    decimal.NewFromInt(2),
	})
	s.NoError(err)

	s.True(resp.Subtotal.Equal(decimal.NewFromInt(150)))
	s.True(resp.Total.Equal(decimal.NewFromInt(150)))
	s.True(resp.AmountDue.Equal(decimal.NewFromInt(150)))
	s.Require().Len(resp.LineItems, 2)

	updatedInv, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.True(updatedInv.IsManuallyEdited)
}

func (s *AddLineItemSuite) TestRejectsOnNonDraftInvoice() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.Zero)
	inv.InvoiceStatus = types.InvoiceStatusFinalized
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

	_, err := s.service.AddLineItem(ctx, inv.ID, dto.AddLineItemRequest{
		DisplayName: "Should Not Be Added",
		Amount:      decimal.NewFromInt(100),
		Quantity:    decimal.NewFromInt(1),
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))

	published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Len(published, 0)
}

func (s *AddLineItemSuite) TestRejectsNegativeAmount() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.Zero)

	_, err := s.service.AddLineItem(ctx, inv.ID, dto.AddLineItemRequest{
		DisplayName: "Bad Item",
		Amount:      decimal.NewFromInt(-10),
		Quantity:    decimal.NewFromInt(1),
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *AddLineItemSuite) TestRejectsNegativeQuantity() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.Zero)

	_, err := s.service.AddLineItem(ctx, inv.ID, dto.AddLineItemRequest{
		DisplayName: "Bad Item",
		Amount:      decimal.NewFromInt(10),
		Quantity:    decimal.NewFromInt(-1),
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}
