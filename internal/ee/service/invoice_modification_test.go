package service

import (
	"context"
	"testing"
	"time"

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
		WebhookPublisher:    s.GetWebhookPublisher(),
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
		DisplayName: &newName,
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

// A stale version id resolves onto the CURRENT version via parent lineage instead of
// erroring or branching the chain: the edit lands on the live row, and the invoice
// still holds exactly one published version afterwards.
func (s *LineItemEditSuite) TestUpdateViaStaleVersionIDResolvesToCurrentVersion() {
	ctx := s.GetContext()
	inv, v1 := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	name2 := "v2"
	_, err := s.service.UpdateLineItem(ctx, inv.ID, v1.ID, dto.UpdateLineItemRequest{DisplayName: &name2})
	s.Require().NoError(err)

	name3 := "v3-via-stale-id"
	_, err = s.service.UpdateLineItem(ctx, inv.ID, v1.ID, dto.UpdateLineItemRequest{DisplayName: &name3})
	s.NoError(err)

	published, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Require().Len(published, 1)
	s.Equal(name3, lo.FromPtr(published[0].DisplayName))
}

func (s *LineItemEditSuite) TestRemoveSoftDeletesLineItem() {
	ctx := s.GetContext()
	inv, li := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(10))

	resp, err := s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{
		LineItemIDs: []string{li.ID},
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
	s.True(ierr.IsNotFound(err))
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

type InvoiceModificationServiceSuite struct {
	testutil.BaseServiceTestSuite
	service InvoiceService
}

func TestInvoiceModification(t *testing.T) {
	suite.Run(t, new(InvoiceModificationServiceSuite))
}

func (s *InvoiceModificationServiceSuite) SetupTest() {
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

func (s *InvoiceModificationServiceSuite) createDraftInvoice() *invoice.Invoice {
	ctx := s.GetContext()
	inv := &invoice.Invoice{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:    "cust_test",
		InvoiceType:   types.InvoiceTypeSubscription,
		InvoiceStatus: types.InvoiceStatusDraft,
		Currency:      "usd",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(ctx, inv))
	return inv
}

func (s *InvoiceModificationServiceSuite) TestExecuteAddLineItem() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice()

	resp, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action: dto.InvoiceModifyLineItemActionAdd,
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
		},
	})
	s.NoError(err)
	s.Require().NotNil(resp)
	s.Require().NotNil(resp.Invoice)
	s.Require().Len(resp.Invoice.LineItems, 2)
	s.True(resp.Invoice.Subtotal.Equal(decimal.NewFromInt(150)))
}

func (s *InvoiceModificationServiceSuite) TestExecuteRemoveLineItem() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice()

	li := &invoice.InvoiceLineItem{
		ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:   inv.ID,
		CustomerID:  inv.CustomerID,
		DisplayName: lo.ToPtr("Item To Remove"),
		Amount:      decimal.NewFromInt(100),
		Quantity:    decimal.NewFromInt(1),
		Currency:    "usd",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, li))

	resp, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action:      dto.InvoiceModifyLineItemActionRemove,
			LineItemIDs: []string{li.ID},
		},
	})
	s.NoError(err)
	s.Require().NotNil(resp)
	s.Require().NotNil(resp.Invoice)
	s.Len(resp.Invoice.LineItems, 0)
}

func (s *InvoiceModificationServiceSuite) TestExecuteUpdateLineItem() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice()

	li := &invoice.InvoiceLineItem{
		ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:   inv.ID,
		CustomerID:  inv.CustomerID,
		DisplayName: lo.ToPtr("Original Name"),
		Amount:      decimal.NewFromInt(100),
		Quantity:    decimal.NewFromInt(1),
		Currency:    "usd",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, li))

	resp, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action:     dto.InvoiceModifyLineItemActionUpdate,
			LineItemID: li.ID,
			Update: &dto.UpdateLineItemRequest{
				DisplayName: lo.ToPtr("Updated Name"),
				Amount:      lo.ToPtr(decimal.NewFromInt(250)),
			},
		},
	})
	s.NoError(err)
	s.Require().NotNil(resp)
	s.Require().NotNil(resp.Invoice)
	s.Require().Len(resp.Invoice.LineItems, 1)
	updated := resp.Invoice.LineItems[0]
	// The update is versioned: a new line item replaces the original.
	s.NotEqual(li.ID, updated.ID)
	s.Equal("Updated Name", lo.FromPtr(updated.DisplayName))
	s.True(updated.Amount.Equal(decimal.NewFromInt(250)))
	s.True(resp.Invoice.Subtotal.Equal(decimal.NewFromInt(250)))
}

func (s *InvoiceModificationServiceSuite) TestExecuteUpdateLineItemRequiresIDAndFields() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice()

	_, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action: dto.InvoiceModifyLineItemActionUpdate,
			Update: &dto.UpdateLineItemRequest{DisplayName: lo.ToPtr("x")},
		},
	})
	s.Error(err)

	_, err = s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action:     dto.InvoiceModifyLineItemActionUpdate,
			LineItemID: "li_missing_update",
		},
	})
	s.Error(err)
}

func (s *InvoiceModificationServiceSuite) TestExecuteMarksInvoiceAsManuallyEdited() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice()

	_, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action: dto.InvoiceModifyLineItemActionAdd,
			Items: []dto.AddLineItemRequest{
				{
					DisplayName: "First Item",
					Amount:      decimal.NewFromInt(100),
					Quantity:    decimal.NewFromInt(1),
				},
			},
		},
	})
	s.NoError(err)

	updatedInv, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.True(updatedInv.IsManuallyEdited)
}

func (s *InvoiceModificationServiceSuite) TestExecuteRejectsUnknownType() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice()

	_, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: "bogus",
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *InvoiceModificationServiceSuite) TestExecuteRejectsUnknownAction() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice()

	_, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action: "bogus",
		},
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *InvoiceModificationServiceSuite) TestExecuteRejectsMissingLineItemParams() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice()

	_, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *InvoiceModificationServiceSuite) TestExecutePropagatesUnderlyingErrors() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice()
	inv.InvoiceStatus = types.InvoiceStatusFinalized
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

	_, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action: dto.InvoiceModifyLineItemActionAdd,
			Items: []dto.AddLineItemRequest{
				{
					DisplayName: "Should Not Be Added",
					Amount:      decimal.NewFromInt(100),
					Quantity:    decimal.NewFromInt(1),
				},
			},
		},
	})
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

// ---- finalized-invoice edit flow (void and recreate as draft) ----

func (s *InvoiceModificationServiceSuite) createFinalizedInvoiceWithLineItem() (*invoice.Invoice, *invoice.InvoiceLineItem) {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 8, 31, 23, 59, 59, 0, time.UTC)
	dueDate := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	inv := &invoice.Invoice{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:    "cust_test",
		InvoiceType:   types.InvoiceTypeOneOff,
		InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusPending,
		Currency:      "usd",
		Description:   "August platform charges",
		BillingPeriod: lo.ToPtr("MONTHLY"),
		PeriodStart:   &periodStart,
		PeriodEnd:     &periodEnd,
		DueDate:       &dueDate,
		Metadata:      types.Metadata{"po_number": "PO-42"},
		Subtotal:      decimal.NewFromInt(100),
		Total:         decimal.NewFromInt(100),
		AmountDue:     decimal.NewFromInt(100),
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(ctx, inv))

	liPeriodStart := periodStart
	liPeriodEnd := periodEnd
	li := &invoice.InvoiceLineItem{
		ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:   inv.ID,
		CustomerID:  inv.CustomerID,
		DisplayName: lo.ToPtr("Platform fee"),
		Amount:      decimal.NewFromInt(100),
		Quantity:    decimal.NewFromInt(1),
		Currency:    "usd",
		PeriodStart: &liPeriodStart,
		PeriodEnd:   &liPeriodEnd,
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, li))
	return inv, li
}

func (s *InvoiceModificationServiceSuite) TestExecuteUpdateOnFinalizedVoidsAndRecreatesDraft() {
	ctx := s.GetContext()
	inv, li := s.createFinalizedInvoiceWithLineItem()

	resp, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action:     dto.InvoiceModifyLineItemActionUpdate,
			LineItemID: li.ID,
			Update:     &dto.UpdateLineItemRequest{Amount: lo.ToPtr(decimal.NewFromInt(250))},
		},
	})
	s.NoError(err)
	s.Require().NotNil(resp)
	s.Require().NotNil(resp.Invoice)

	// The response is a NEW draft invoice, not the original.
	s.NotEqual(inv.ID, resp.Invoice.ID)
	s.Equal(types.InvoiceStatusDraft, resp.Invoice.InvoiceStatus)
	s.True(resp.Invoice.IsManuallyEdited)

	// Copied invoice fields survive — description and the billing interval in particular.
	s.Equal("August platform charges", resp.Invoice.Description)
	s.Equal("MONTHLY", lo.FromPtr(resp.Invoice.BillingPeriod))
	s.Require().NotNil(resp.Invoice.PeriodStart)
	s.True(inv.PeriodStart.Equal(*resp.Invoice.PeriodStart))
	s.Require().NotNil(resp.Invoice.PeriodEnd)
	s.True(inv.PeriodEnd.Equal(*resp.Invoice.PeriodEnd))
	s.Require().NotNil(resp.Invoice.DueDate)
	s.True(inv.DueDate.Equal(*resp.Invoice.DueDate))
	s.Equal("PO-42", resp.Invoice.Metadata["po_number"])

	// The modification landed on the copy (versioned update on the copied item).
	s.Require().Len(resp.Invoice.LineItems, 1)
	updated := resp.Invoice.LineItems[0]
	s.True(updated.Amount.Equal(decimal.NewFromInt(250)))
	s.Equal("Platform fee", lo.FromPtr(updated.DisplayName))
	s.Require().NotNil(updated.PeriodStart)
	s.True(li.PeriodStart.Equal(*updated.PeriodStart))
	s.Require().NotNil(updated.PeriodEnd)
	s.True(li.PeriodEnd.Equal(*updated.PeriodEnd))
	s.True(resp.Invoice.Subtotal.Equal(decimal.NewFromInt(250)))

	// The original is voided and linked to the replacement.
	original, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.NoError(err)
	s.Equal(types.InvoiceStatusVoided, original.InvoiceStatus)
	s.Equal(resp.Invoice.ID, lo.FromPtr(original.RecalculatedInvoiceID))
}

func (s *InvoiceModificationServiceSuite) TestExecuteRemoveOnFinalizedTranslatesOriginalLineItemIDs() {
	ctx := s.GetContext()
	inv, li := s.createFinalizedInvoiceWithLineItem()

	// Remove using the ORIGINAL invoice's line item id; the copy has a new id.
	resp, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action:      dto.InvoiceModifyLineItemActionRemove,
			LineItemIDs: []string{li.ID},
		},
	})
	s.NoError(err)
	s.Require().NotNil(resp.Invoice)
	s.NotEqual(inv.ID, resp.Invoice.ID)
	s.Len(resp.Invoice.LineItems, 0)
	s.True(resp.Invoice.Subtotal.IsZero())
}

func (s *InvoiceModificationServiceSuite) TestExecuteOnVoidedOriginalIsRejectedWithReplacement() {
	ctx := s.GetContext()
	inv, li := s.createFinalizedInvoiceWithLineItem()

	// First call voids and recreates.
	first, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action:     dto.InvoiceModifyLineItemActionUpdate,
			LineItemID: li.ID,
			Update:     &dto.UpdateLineItemRequest{DisplayName: lo.ToPtr("Platform fee (edited)")},
		},
	})
	s.NoError(err)
	draftID := first.Invoice.ID

	// Hard stop: a call that still targets the voided original is rejected, and the
	// error names the replacement so the caller can retry against it.
	_, err = s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action: dto.InvoiceModifyLineItemActionAdd,
			Items:  []dto.AddLineItemRequest{{DisplayName: "Setup fee", Amount: decimal.NewFromInt(50), Quantity: decimal.NewFromInt(1)}},
		},
	})
	s.Error(err)
	s.Contains(err.Error(), draftID)

	// The same call against the returned draft id succeeds.
	second, err := s.service.ModifyInvoice(ctx, draftID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action: dto.InvoiceModifyLineItemActionAdd,
			Items:  []dto.AddLineItemRequest{{DisplayName: "Setup fee", Amount: decimal.NewFromInt(50), Quantity: decimal.NewFromInt(1)}},
		},
	})
	s.NoError(err)
	s.Equal(draftID, second.Invoice.ID)
	s.Len(second.Invoice.LineItems, 2)
}

// A targeted update of a nonexistent line item errors; the flow runs in one
// transaction, so in production the void and the draft copy roll back with it
// (the in-memory test client has no rollback, so only the error is asserted here).
func (s *InvoiceModificationServiceSuite) TestExecuteBadLineItemIDOnFinalizedErrors() {
	ctx := s.GetContext()
	inv, _ := s.createFinalizedInvoiceWithLineItem()

	_, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action:     dto.InvoiceModifyLineItemActionUpdate,
			LineItemID: "li_does_not_exist",
			Update:     &dto.UpdateLineItemRequest{Amount: lo.ToPtr(decimal.NewFromInt(1))},
		},
	})
	s.Error(err)
	s.True(ierr.IsNotFound(err))
}

func (s *InvoiceModificationServiceSuite) TestExecuteChainedEditsAcrossRecreations() {
	ctx := s.GetContext()
	inv, li := s.createFinalizedInvoiceWithLineItem()

	// Hop 1: edit voids the original and creates draft A.
	first, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action:     dto.InvoiceModifyLineItemActionUpdate,
			LineItemID: li.ID,
			Update:     &dto.UpdateLineItemRequest{Amount: lo.ToPtr(decimal.NewFromInt(150))},
		},
	})
	s.NoError(err)
	draftA := first.Invoice.ID

	// Draft A gets finalized and edited again -> voided with replacement draft B.
	invA, err := s.GetStores().InvoiceRepo.Get(ctx, draftA)
	s.NoError(err)
	invA.InvoiceStatus = types.InvoiceStatusFinalized
	invA.PaymentStatus = types.PaymentStatusPending
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, invA))

	second, err := s.service.ModifyInvoice(ctx, draftA, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action: dto.InvoiceModifyLineItemActionAdd,
			Items:  []dto.AddLineItemRequest{{DisplayName: "Extra", Amount: decimal.NewFromInt(10), Quantity: decimal.NewFromInt(1)}},
		},
	})
	s.NoError(err)
	draftB := second.Invoice.ID
	s.NotEqual(draftA, draftB)

	// Both stale ids are hard-stopped; each error names that invoice's direct replacement.
	_, err = s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type:           dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{Action: dto.InvoiceModifyLineItemActionAdd, Items: []dto.AddLineItemRequest{{DisplayName: "X", Amount: decimal.NewFromInt(1), Quantity: decimal.NewFromInt(1)}}},
	})
	s.Error(err)
	s.Contains(err.Error(), draftA)

	_, err = s.service.ModifyInvoice(ctx, draftA, dto.ExecuteInvoiceModifyRequest{
		Type:           dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{Action: dto.InvoiceModifyLineItemActionAdd, Items: []dto.AddLineItemRequest{{DisplayName: "X", Amount: decimal.NewFromInt(1), Quantity: decimal.NewFromInt(1)}}},
	})
	s.Error(err)
	s.Contains(err.Error(), draftB)
}

func (s *InvoiceModificationServiceSuite) TestLineItemDescriptionAndPeriodRoundTrip() {
	ctx := s.GetContext()
	inv := s.createDraftInvoice()
	periodStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC)

	// Add carries description and the period interval.
	resp, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action: dto.InvoiceModifyLineItemActionAdd,
			Items: []dto.AddLineItemRequest{{
				DisplayName: "Consulting",
				Amount:      decimal.NewFromInt(100),
				Quantity:    decimal.NewFromInt(1),
				Description: lo.ToPtr("September consulting retainer"),
				PeriodStart: &periodStart,
				PeriodEnd:   &periodEnd,
			}},
		},
	})
	s.NoError(err)
	s.Require().Len(resp.Invoice.LineItems, 1)
	added := resp.Invoice.LineItems[0]
	s.Equal("September consulting retainer", added.Metadata["description"])
	s.Require().NotNil(added.PeriodStart)
	s.True(periodStart.Equal(*added.PeriodStart))
	s.Require().NotNil(added.PeriodEnd)
	s.True(periodEnd.Equal(*added.PeriodEnd))

	// Update can change description and shift the interval.
	newEnd := periodEnd.AddDate(0, 1, 0)
	resp, err = s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action:     dto.InvoiceModifyLineItemActionUpdate,
			LineItemID: added.ID,
			Update: &dto.UpdateLineItemRequest{
				Description: lo.ToPtr("Sep-Oct consulting retainer"),
				PeriodEnd:   &newEnd,
			},
		},
	})
	s.NoError(err)
	s.Require().Len(resp.Invoice.LineItems, 1)
	updated := resp.Invoice.LineItems[0]
	s.Equal("Sep-Oct consulting retainer", updated.Metadata["description"])
	s.True(periodStart.Equal(*updated.PeriodStart))
	s.True(newEnd.Equal(*updated.PeriodEnd))

	// A reversed interval is rejected.
	bad := periodStart.AddDate(0, -1, 0)
	_, err = s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action:     dto.InvoiceModifyLineItemActionUpdate,
			LineItemID: updated.ID,
			Update:     &dto.UpdateLineItemRequest{PeriodEnd: &bad},
		},
	})
	s.Error(err) // the merged item is validated, so the reversed interval is caught
	_, err = s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type: dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{
			Action: dto.InvoiceModifyLineItemActionAdd,
			Items: []dto.AddLineItemRequest{{
				DisplayName: "Bad",
				Amount:      decimal.NewFromInt(1),
				Quantity:    decimal.NewFromInt(1),
				PeriodStart: &periodEnd,
				PeriodEnd:   &periodStart,
			}},
		},
	})
	s.Error(err)
}
