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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

func TestRecalculateInvoiceTotals(t *testing.T) {
	s := &invoiceService{}

	t.Run("normal case sums line items and derives totals", func(t *testing.T) {
		inv := &invoice.Invoice{
			TotalDiscount:              decimal.NewFromInt(10),
			TotalTax:                   decimal.NewFromInt(5),
			TotalPrepaidCreditsApplied: decimal.NewFromInt(20),
			AmountPaid:                 decimal.NewFromInt(30),
		}
		lineItems := []*invoice.InvoiceLineItem{
			{Amount: decimal.NewFromInt(100)},
			{Amount: decimal.NewFromInt(50)},
		}

		s.recalculateTotalsFromLineItems(inv, lineItems)

		// subtotal = 150
		assert.True(t, decimal.NewFromInt(150).Equal(inv.Subtotal))
		// total = 150 - 20 - 10 + 5 = 125
		assert.True(t, decimal.NewFromInt(125).Equal(inv.Total))
		assert.True(t, decimal.NewFromInt(125).Equal(inv.AmountDue))
		// amount_remaining = 125 - 30 = 95
		assert.True(t, decimal.NewFromInt(95).Equal(inv.AmountRemaining))

		// TotalDiscount / TotalTax must be untouched
		assert.True(t, decimal.NewFromInt(10).Equal(inv.TotalDiscount))
		assert.True(t, decimal.NewFromInt(5).Equal(inv.TotalTax))
	})

	t.Run("floors total at zero when discount and tax exceed subtotal", func(t *testing.T) {
		inv := &invoice.Invoice{
			TotalDiscount:              decimal.NewFromInt(80),
			TotalTax:                   decimal.NewFromInt(0),
			TotalPrepaidCreditsApplied: decimal.NewFromInt(30),
			AmountPaid:                 decimal.NewFromInt(0),
		}
		lineItems := []*invoice.InvoiceLineItem{
			{Amount: decimal.NewFromInt(50)},
		}

		s.recalculateTotalsFromLineItems(inv, lineItems)

		// subtotal = 50; raw total = 50 - 30 - 80 + 0 = -60 -> floored to 0
		assert.True(t, decimal.NewFromInt(50).Equal(inv.Subtotal))
		assert.True(t, decimal.Zero.Equal(inv.Total))
		assert.True(t, decimal.Zero.Equal(inv.AmountDue))
		assert.True(t, decimal.Zero.Equal(inv.AmountRemaining))
	})

	t.Run("empty line items yields zero subtotal", func(t *testing.T) {
		inv := &invoice.Invoice{
			TotalDiscount:              decimal.Zero,
			TotalTax:                   decimal.NewFromInt(3),
			TotalPrepaidCreditsApplied: decimal.Zero,
			AmountPaid:                 decimal.Zero,
		}

		s.recalculateTotalsFromLineItems(inv, []*invoice.InvoiceLineItem{})

		assert.True(t, decimal.Zero.Equal(inv.Subtotal))
		// total = 0 - 0 - 0 + 3 = 3
		assert.True(t, decimal.NewFromInt(3).Equal(inv.Total))
	})
}

type LineItemEditSuite struct {
	testutil.BaseServiceTestSuite
	service InvoiceService
}

func TestLineItemEdit(t *testing.T) {
	suite.Run(t, new(LineItemEditSuite))
}

func (s *LineItemEditSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewInvoiceService(ServiceParams{
		Logger:              s.GetLogger(),
		Config:              s.GetConfig(),
		DB:                  s.GetDB(),
		InvoiceRepo:         s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo: s.GetStores().InvoiceLineItemRepo,
	})
}

func (s *LineItemEditSuite) createDraftInvoice(ctx context.Context, amount decimal.Decimal) *invoice.Invoice {
	inv := &invoice.Invoice{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:    "cust_test",
		InvoiceType:   types.InvoiceTypeSubscription,
		InvoiceStatus: types.InvoiceStatusDraft,
		Currency:      "usd",
		Subtotal:      amount,
		Total:         amount,
		AmountDue:     amount,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(ctx, inv))
	return inv
}

func (s *LineItemEditSuite) createDraftInvoiceWithLineItem(ctx context.Context, amount, quantity decimal.Decimal) (*invoice.Invoice, *invoice.InvoiceLineItem) {
	inv := s.createDraftInvoice(ctx, amount)

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
		Currency:               inv.Currency,
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, li))

	return inv, li
}

func (s *LineItemEditSuite) TestAddAddsNewPublishedLineItem() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.Zero)

	resp, err := s.service.AddBulkLineItem(ctx, inv.ID, dto.AddBulkLineItemRequest{
		Items: []dto.AddLineItemRequest{
			{
				DisplayName: "New Item",
				Amount:      decimal.NewFromInt(100),
				Quantity:    decimal.NewFromInt(2),
			},
		},
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

func (s *LineItemEditSuite) TestAddRecalculatesTotalsAndFlagsManuallyEdited() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.NewFromInt(50))

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

	resp, err := s.service.AddBulkLineItem(ctx, inv.ID, dto.AddBulkLineItemRequest{
		Items: []dto.AddLineItemRequest{
			{
				DisplayName: "New Item",
				Amount:      decimal.NewFromInt(100),
				Quantity:    decimal.NewFromInt(2),
			},
		},
		MarkManuallyEdited: true,
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

func (s *LineItemEditSuite) TestAddDoesNotMarkManuallyEditedByDefault() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.Zero)

	_, err := s.service.AddBulkLineItem(ctx, inv.ID, dto.AddBulkLineItemRequest{
		Items: []dto.AddLineItemRequest{
			{
				DisplayName: "New Item",
				Amount:      decimal.NewFromInt(100),
				Quantity:    decimal.NewFromInt(2),
			},
		},
	})
	s.NoError(err)

	updatedInv, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.False(updatedInv.IsManuallyEdited)
}

func (s *LineItemEditSuite) TestAddDoesNotResetManuallyEditedFlagWhenNotRequested() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.Zero)
	inv.IsManuallyEdited = true
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

	_, err := s.service.AddBulkLineItem(ctx, inv.ID, dto.AddBulkLineItemRequest{
		Items: []dto.AddLineItemRequest{
			{
				DisplayName: "New Item",
				Amount:      decimal.NewFromInt(100),
				Quantity:    decimal.NewFromInt(2),
			},
		},
	})
	s.NoError(err)

	updatedInv, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.True(updatedInv.IsManuallyEdited)
}

func (s *LineItemEditSuite) TestAddRejectsOnNonDraftInvoice() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.Zero)
	inv.InvoiceStatus = types.InvoiceStatusFinalized
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

	_, err := s.service.AddBulkLineItem(ctx, inv.ID, dto.AddBulkLineItemRequest{
		Items: []dto.AddLineItemRequest{
			{
				DisplayName: "Should Not Be Added",
				Amount:      decimal.NewFromInt(100),
				Quantity:    decimal.NewFromInt(1),
			},
		},
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))

	published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Len(published, 0)
}

func (s *LineItemEditSuite) TestAddRejectsNegativeAmount() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.Zero)

	_, err := s.service.AddBulkLineItem(ctx, inv.ID, dto.AddBulkLineItemRequest{
		Items: []dto.AddLineItemRequest{
			{
				DisplayName: "Bad Item",
				Amount:      decimal.NewFromInt(-10),
				Quantity:    decimal.NewFromInt(1),
			},
		},
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *LineItemEditSuite) TestAddRejectsNegativeQuantity() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.Zero)

	_, err := s.service.AddBulkLineItem(ctx, inv.ID, dto.AddBulkLineItemRequest{
		Items: []dto.AddLineItemRequest{
			{
				DisplayName: "Bad Item",
				Amount:      decimal.NewFromInt(10),
				Quantity:    decimal.NewFromInt(-1),
			},
		},
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *LineItemEditSuite) TestAddMultipleLineItemsInOneCall() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.Zero)

	resp, err := s.service.AddBulkLineItem(ctx, inv.ID, dto.AddBulkLineItemRequest{
		Items: []dto.AddLineItemRequest{
			{
				DisplayName: "First Item",
				Amount:      decimal.NewFromInt(100),
				Quantity:    decimal.NewFromInt(1),
			},
			{
				DisplayName: "Second Item",
				Amount:      decimal.NewFromInt(50),
				Quantity:    decimal.NewFromInt(2),
			},
		},
	})
	s.NoError(err)
	s.Require().Len(resp.LineItems, 2)
	s.True(resp.Subtotal.Equal(decimal.NewFromInt(150)))

	published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Require().Len(published, 2)

	names := []string{lo.FromPtr(published[0].DisplayName), lo.FromPtr(published[1].DisplayName)}
	s.ElementsMatch([]string{"First Item", "Second Item"}, names)
}

func (s *LineItemEditSuite) TestAddBulkLineItemRejectsEmptyBatch() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.Zero)

	_, err := s.service.AddBulkLineItem(ctx, inv.ID, dto.AddBulkLineItemRequest{
		Items: []dto.AddLineItemRequest{},
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *LineItemEditSuite) TestAddBulkLineItemRejectsOversizedBatch() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice(ctx, decimal.Zero)

	items := make([]dto.AddLineItemRequest, 101)
	for i := range items {
		items[i] = dto.AddLineItemRequest{
			DisplayName: "Item",
			Amount:      decimal.NewFromInt(1),
			Quantity:    decimal.NewFromInt(1),
		}
	}

	_, err := s.service.AddBulkLineItem(ctx, inv.ID, dto.AddBulkLineItemRequest{
		Items: items,
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *LineItemEditSuite) TestUpdateArchivesOldCreatesNew() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	newName := "New Name"
	resp, err := s.service.UpdateLineItem(ctx, inv.ID, li.ID, dto.UpdateLineItemRequest{
		DisplayName:        &newName,
		MarkManuallyEdited: true,
	})
	s.NoError(err)
	s.NotNil(resp)

	oldRow, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, li.ID)
	s.NoError(err)
	s.Equal(types.StatusArchived, oldRow.Status)

	published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Len(published, 1)
	newRow := published[0]
	s.NotEqual(li.ID, newRow.ID)

	s.Equal(lo.FromPtr(li.PriceID), lo.FromPtr(newRow.PriceID))
	s.Equal(lo.FromPtr(li.MeterID), lo.FromPtr(newRow.MeterID))
	s.Equal(lo.FromPtr(li.SubscriptionLineItemID), lo.FromPtr(newRow.SubscriptionLineItemID))

	s.Equal(newName, lo.FromPtr(newRow.DisplayName))
	s.True(newRow.Amount.Equal(li.Amount))
	s.True(newRow.Quantity.Equal(li.Quantity))

	s.Require().NotNil(newRow.ParentLineItemID)
	s.Equal(li.ID, *newRow.ParentLineItemID)

	updatedInv, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.True(updatedInv.IsManuallyEdited)
}

func (s *LineItemEditSuite) TestUpdateDoesNotMarkManuallyEditedByDefault() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	newName := "New Name"
	_, err := s.service.UpdateLineItem(ctx, inv.ID, li.ID, dto.UpdateLineItemRequest{
		DisplayName: &newName,
	})
	s.NoError(err)

	updatedInv, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.False(updatedInv.IsManuallyEdited)
}

func (s *LineItemEditSuite) TestUpdateChainsLineageAcrossMultipleEdits() {
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

	s.Require().NotNil(v3.ParentLineItemID)
	s.Equal(v2.ID, *v3.ParentLineItemID)
	s.NotEqual(v1.ID, *v3.ParentLineItemID)

	midRow, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, *v3.ParentLineItemID)
	s.NoError(err)
	s.Equal(v2.ID, midRow.ID)
	s.Require().NotNil(midRow.ParentLineItemID)
	s.Equal(v1.ID, *midRow.ParentLineItemID)

	s.Equal(types.StatusArchived, midRow.Status)
	origRow, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, v1.ID)
	s.NoError(err)
	s.Equal(types.StatusArchived, origRow.Status)
}

func (s *LineItemEditSuite) TestUpdateQuantityAmountIndependent() {
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

func (s *LineItemEditSuite) TestUpdateRejectsEditOnFinalizedInvoice() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	inv.InvoiceStatus = types.InvoiceStatusFinalized
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

	newName := "Should Not Apply"
	_, err := s.service.UpdateLineItem(ctx, inv.ID, li.ID, dto.UpdateLineItemRequest{DisplayName: &newName})
	s.Error(err)
	s.True(ierr.IsValidation(err))

	unchanged, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, li.ID)
	s.NoError(err)
	s.Equal(types.StatusPublished, unchanged.Status)
	s.Equal("Original Name", lo.FromPtr(unchanged.DisplayName))
}

func (s *LineItemEditSuite) TestUpdateRecalculatesTotals() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	newAmount := decimal.NewFromInt(150)
	resp, err := s.service.UpdateLineItem(ctx, inv.ID, li.ID, dto.UpdateLineItemRequest{Amount: &newAmount})
	s.NoError(err)
	s.True(resp.Subtotal.Equal(decimal.NewFromInt(150)))
	s.True(resp.Total.Equal(decimal.NewFromInt(150)))
	s.True(resp.AmountDue.Equal(decimal.NewFromInt(150)))

	s.Require().Len(resp.LineItems, 1)
	s.True(resp.LineItems[0].Amount.Equal(newAmount))
}

func (s *LineItemEditSuite) TestUpdateRejectsNegativeAmount() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	negativeAmount := decimal.NewFromInt(-10)
	_, err := s.service.UpdateLineItem(ctx, inv.ID, li.ID, dto.UpdateLineItemRequest{Amount: &negativeAmount})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *LineItemEditSuite) TestUpdateRejectsLineItemFromDifferentInvoice() {
	ctx := s.GetContext()
	inv1, _ := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))
	inv2, li2 := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(200), decimal.NewFromInt(5))
	_ = inv2

	newName := "Should Not Apply"
	_, err := s.service.UpdateLineItem(ctx, inv1.ID, li2.ID, dto.UpdateLineItemRequest{DisplayName: &newName})
	s.Error(err)
	s.True(ierr.IsNotFound(err))
}

func (s *LineItemEditSuite) TestUpdateRejectsEditOnAlreadyArchivedLineItem() {
	ctx := s.GetContext()
	inv, v1 := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	name2 := "v2"
	_, err := s.service.UpdateLineItem(ctx, inv.ID, v1.ID, dto.UpdateLineItemRequest{DisplayName: &name2})
	s.Require().NoError(err)

	name3 := "v3-via-stale-id"
	_, err = s.service.UpdateLineItem(ctx, inv.ID, v1.ID, dto.UpdateLineItemRequest{DisplayName: &name3})
	s.Error(err)
	s.True(ierr.IsValidation(err))

	published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Require().Len(published, 1)
	s.Equal(name2, lo.FromPtr(published[0].DisplayName))
}

func (s *LineItemEditSuite) TestRemoveSoftDeletesLineItem() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	resp, err := s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{
		LineItemIDs:        []string{li.ID},
		MarkManuallyEdited: true,
	})
	s.NoError(err)
	s.NotNil(resp)

	row, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, li.ID)
	s.NoError(err)
	s.Equal(types.StatusDeleted, row.Status)

	published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Len(published, 0)

	s.Len(resp.LineItems, 0)

	updatedInv, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.True(updatedInv.IsManuallyEdited)
}

func (s *LineItemEditSuite) TestRemoveDoesNotMarkManuallyEditedByDefault() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	_, err := s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{
		LineItemIDs: []string{li.ID},
	})
	s.NoError(err)

	updatedInv, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.False(updatedInv.IsManuallyEdited)
}

func (s *LineItemEditSuite) TestRemoveRecalculatesTotalsExcludingRemovedItem() {
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

	resp, err := s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{
		LineItemIDs: []string{li1.ID},
	})
	s.NoError(err)

	s.True(resp.Subtotal.Equal(decimal.NewFromInt(50)))
	s.True(resp.Total.Equal(decimal.NewFromInt(50)))
	s.True(resp.AmountDue.Equal(decimal.NewFromInt(50)))
	s.Require().Len(resp.LineItems, 1)
	s.Equal(li2.ID, resp.LineItems[0].ID)
}

func (s *LineItemEditSuite) TestRemoveRejectsOnFinalizedInvoice() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	inv.InvoiceStatus = types.InvoiceStatusFinalized
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

	_, err := s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{
		LineItemIDs: []string{li.ID},
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))

	unchanged, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, li.ID)
	s.NoError(err)
	s.Equal(types.StatusPublished, unchanged.Status)
}

func (s *LineItemEditSuite) TestRemoveRejectsLineItemFromDifferentInvoice() {
	ctx := s.GetContext()
	inv1, _ := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))
	inv2, li2 := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(200), decimal.NewFromInt(5))
	_ = inv2

	_, err := s.service.RemoveBulkLineItem(ctx, inv1.ID, dto.RemoveBulkLineItemRequest{
		LineItemIDs: []string{li2.ID},
	})
	s.Error(err)
	s.True(ierr.IsNotFound(err))

	unchanged, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, li2.ID)
	s.NoError(err)
	s.Equal(types.StatusPublished, unchanged.Status)
}

func (s *LineItemEditSuite) TestRemoveRejectsAlreadyDeletedLineItem() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	_, err := s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{
		LineItemIDs: []string{li.ID},
	})
	s.Require().NoError(err)

	_, err = s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{
		LineItemIDs: []string{li.ID},
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *LineItemEditSuite) TestRemoveRejectsNonExistentLineItem() {
	ctx := s.GetContext()
	inv, _ := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	_, err := s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{
		LineItemIDs: []string{"li_does_not_exist"},
	})
	s.Error(err)
	s.True(ierr.IsNotFound(err))
}

func (s *LineItemEditSuite) TestRemoveMultipleLineItemsInOneCall() {
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

	li3 := &invoice.InvoiceLineItem{
		ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:   inv.ID,
		CustomerID:  inv.CustomerID,
		DisplayName: lo.ToPtr("Third Item"),
		Amount:      decimal.NewFromInt(25),
		Quantity:    decimal.NewFromInt(1),
		Currency:    "usd",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, li3))

	resp, err := s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{
		LineItemIDs: []string{li1.ID, li2.ID},
	})
	s.NoError(err)
	s.Require().Len(resp.LineItems, 1)
	s.Equal(li3.ID, resp.LineItems[0].ID)
	s.True(resp.Subtotal.Equal(decimal.NewFromInt(25)))
}

func (s *LineItemEditSuite) TestRemoveBulkLineItemRejectsEmptyBatch() {
	ctx := s.GetContext()
	inv, _ := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	_, err := s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{
		LineItemIDs: []string{},
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *LineItemEditSuite) TestRemoveBulkLineItemRejectsOversizedBatch() {
	ctx := s.GetContext()
	inv, _ := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	ids := make([]string, 101)
	for i := range ids {
		ids[i] = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM)
	}

	_, err := s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{
		LineItemIDs: ids,
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}
