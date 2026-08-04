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

// UpdateLineItemSuite tests the archive-and-replace UpdateLineItem service method (T-08).
type UpdateLineItemSuite struct {
	testutil.BaseServiceTestSuite
	service InvoiceService
}

func TestUpdateLineItem(t *testing.T) {
	suite.Run(t, new(UpdateLineItemSuite))
}

func (s *UpdateLineItemSuite) SetupTest() {
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
// line item carrying pricing-context fields, and returns both.
func (s *UpdateLineItemSuite) createDraftInvoiceWithLineItem(ctx context.Context, amount, quantity decimal.Decimal) (*invoice.Invoice, *invoice.InvoiceLineItem) {
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
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:              inv.ID,
		CustomerID:             inv.CustomerID,
		PriceID:                lo.ToPtr("price_123"),
		MeterID:                lo.ToPtr("meter_123"),
		SubscriptionLineItemID: lo.ToPtr("sub_li_123"),
		DisplayName:            lo.ToPtr("Original Name"),
		Amount:                 amount,
		Quantity:               quantity,
		Currency:               "usd",
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, li))

	return inv, li
}

func (s *UpdateLineItemSuite) TestArchivesOldCreatesNew() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	newName := "New Name"
	resp, err := s.service.UpdateLineItem(ctx, inv.ID, li.ID, dto.UpdateLineItemRequest{
		DisplayName: &newName,
	})
	s.NoError(err)
	s.NotNil(resp)

	// Old row archived.
	oldRow, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, li.ID)
	s.NoError(err)
	s.Equal(types.StatusArchived, oldRow.Status)

	// Exactly one published line item remains, and it isn't the old row.
	published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Len(published, 1)
	newRow := published[0]
	s.NotEqual(li.ID, newRow.ID)

	// Pricing-context fields carried forward unchanged.
	s.Equal(lo.FromPtr(li.PriceID), lo.FromPtr(newRow.PriceID))
	s.Equal(lo.FromPtr(li.MeterID), lo.FromPtr(newRow.MeterID))
	s.Equal(lo.FromPtr(li.SubscriptionLineItemID), lo.FromPtr(newRow.SubscriptionLineItemID))

	// Edited field applied; untouched fields unchanged.
	s.Equal(newName, lo.FromPtr(newRow.DisplayName))
	s.True(newRow.Amount.Equal(li.Amount))
	s.True(newRow.Quantity.Equal(li.Quantity))

	// New row's parent points at the archived old row.
	s.Require().NotNil(newRow.ParentLineItemID)
	s.Equal(li.ID, *newRow.ParentLineItemID)

	// Invoice flagged as manually edited.
	updatedInv, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.True(updatedInv.IsManuallyEdited)
}

func (s *UpdateLineItemSuite) TestChainsLineageAcrossMultipleEdits() {
	ctx := s.GetContext()
	inv, v1 := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	name2 := "v2"
	_, err := s.service.UpdateLineItem(ctx, inv.ID, v1.ID, dto.UpdateLineItemRequest{DisplayName: &name2})
	s.NoError(err)

	published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Require().Len(published, 1)
	v2 := published[0]
	s.Require().NotNil(v2.ParentLineItemID)
	s.Equal(v1.ID, *v2.ParentLineItemID)

	name3 := "v3"
	_, err = s.service.UpdateLineItem(ctx, inv.ID, v2.ID, dto.UpdateLineItemRequest{DisplayName: &name3})
	s.NoError(err)

	published, err = s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Require().Len(published, 1)
	v3 := published[0]

	// v3 points at v2, NOT the original v1 - lineage is a linked list, not a shortcut.
	s.Require().NotNil(v3.ParentLineItemID)
	s.Equal(v2.ID, *v3.ParentLineItemID)
	s.NotEqual(v1.ID, *v3.ParentLineItemID)

	// Walking the chain reaches v2 then v1.
	midRow, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, *v3.ParentLineItemID)
	s.NoError(err)
	s.Equal(v2.ID, midRow.ID)
	s.Require().NotNil(midRow.ParentLineItemID)
	s.Equal(v1.ID, *midRow.ParentLineItemID)

	// Both v1 and v2 are archived; only v3 is published.
	s.Equal(types.StatusArchived, midRow.Status)
	origRow, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, v1.ID)
	s.NoError(err)
	s.Equal(types.StatusArchived, origRow.Status)
}

func (s *UpdateLineItemSuite) TestQuantityAmountIndependent() {
	ctx := s.GetContext()

	s.Run("editing quantity leaves amount unchanged", func() {
		inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

		newQty := decimal.NewFromInt(20)
		_, err := s.service.UpdateLineItem(ctx, inv.ID, li.ID, dto.UpdateLineItemRequest{Quantity: &newQty})
		s.NoError(err)

		published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
		s.NoError(err)
		s.Require().Len(published, 1)
		s.True(published[0].Quantity.Equal(newQty))
		s.True(published[0].Amount.Equal(decimal.NewFromInt(100)))
	})

	s.Run("editing amount leaves quantity unchanged", func() {
		inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

		newAmount := decimal.NewFromInt(250)
		_, err := s.service.UpdateLineItem(ctx, inv.ID, li.ID, dto.UpdateLineItemRequest{Amount: &newAmount})
		s.NoError(err)

		published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
		s.NoError(err)
		s.Require().Len(published, 1)
		s.True(published[0].Amount.Equal(newAmount))
		s.True(published[0].Quantity.Equal(decimal.NewFromInt(10)))
	})
}

func (s *UpdateLineItemSuite) TestRejectsEditOnFinalizedInvoice() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	inv.InvoiceStatus = types.InvoiceStatusFinalized
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

	newName := "Should Not Apply"
	_, err := s.service.UpdateLineItem(ctx, inv.ID, li.ID, dto.UpdateLineItemRequest{DisplayName: &newName})
	s.Error(err)
	s.True(ierr.IsValidation(err))

	// No change made: line item still published with original values.
	unchanged, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, li.ID)
	s.NoError(err)
	s.Equal(types.StatusPublished, unchanged.Status)
	s.Equal("Original Name", lo.FromPtr(unchanged.DisplayName))
}

func (s *UpdateLineItemSuite) TestRecalculatesTotals() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	newAmount := decimal.NewFromInt(150)
	resp, err := s.service.UpdateLineItem(ctx, inv.ID, li.ID, dto.UpdateLineItemRequest{Amount: &newAmount})
	s.NoError(err)
	s.True(resp.Subtotal.Equal(decimal.NewFromInt(150)))
	s.True(resp.Total.Equal(decimal.NewFromInt(150)))
	s.True(resp.AmountDue.Equal(decimal.NewFromInt(150)))

	// Response reflects the current (post-edit) line items, not the archived one.
	s.Require().Len(resp.LineItems, 1)
	s.True(resp.LineItems[0].Amount.Equal(newAmount))
}

func (s *UpdateLineItemSuite) TestRejectsNegativeAmount() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	negativeAmount := decimal.NewFromInt(-10)
	_, err := s.service.UpdateLineItem(ctx, inv.ID, li.ID, dto.UpdateLineItemRequest{Amount: &negativeAmount})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *UpdateLineItemSuite) TestRejectsLineItemFromDifferentInvoice() {
	ctx := s.GetContext()
	inv1, _ := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))
	inv2, li2 := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(200), decimal.NewFromInt(5))
	_ = inv2

	newName := "Should Not Apply"
	_, err := s.service.UpdateLineItem(ctx, inv1.ID, li2.ID, dto.UpdateLineItemRequest{DisplayName: &newName})
	s.Error(err)
	s.True(ierr.IsNotFound(err))
}

func (s *UpdateLineItemSuite) TestRejectsEditOnAlreadyArchivedLineItem() {
	ctx := s.GetContext()
	inv, v1 := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	name2 := "v2"
	_, err := s.service.UpdateLineItem(ctx, inv.ID, v1.ID, dto.UpdateLineItemRequest{DisplayName: &name2})
	s.Require().NoError(err)

	// v1 is now archived - editing it again (instead of its replacement) must be rejected,
	// otherwise the lineage chain would branch instead of extending linearly (CR-06a).
	name3 := "v3-via-stale-id"
	_, err = s.service.UpdateLineItem(ctx, inv.ID, v1.ID, dto.UpdateLineItemRequest{DisplayName: &name3})
	s.Error(err)
	s.True(ierr.IsValidation(err))

	// Exactly one published row remains, unaffected by the rejected call.
	published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Require().Len(published, 1)
	s.Equal(name2, lo.FromPtr(published[0].DisplayName))
}
