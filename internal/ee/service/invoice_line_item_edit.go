package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// recalculateTotalsFromLineItems re-derives totals from already-applied discount/tax;
// callers must pass only published line items, and discount/tax are not recomputed here.
func (s *invoiceService) recalculateTotalsFromLineItems(inv *invoice.Invoice, lineItems []*invoice.InvoiceLineItem) {
	subtotal := decimal.Zero
	for _, li := range lineItems {
		subtotal = subtotal.Add(li.Amount)
	}
	inv.Subtotal = subtotal

	// Discount-first-then-tax: total = subtotal - prepaid credits - discount + tax
	inv.Total = inv.Subtotal.Sub(inv.TotalPrepaidCreditsApplied).Sub(inv.TotalDiscount).Add(inv.TotalTax)
	if inv.Total.IsNegative() {
		inv.Total = decimal.Zero
	}
	inv.AmountDue = inv.Total
	inv.AmountRemaining = inv.Total.Sub(inv.AmountPaid)
	if inv.AmountRemaining.IsNegative() {
		inv.AmountRemaining = decimal.Zero
	}
}

func (s *invoiceService) UpdateLineItem(ctx context.Context, invoiceID, lineItemID string, req dto.UpdateLineItemRequest) (*dto.InvoiceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var lockedInv *invoice.Invoice
	var publishedLineItems []*invoice.InvoiceLineItem

	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		inv, err := s.InvoiceRepo.GetForUpdate(txCtx, invoiceID)
		if err != nil {
			return err
		}
		if inv.InvoiceStatus != types.InvoiceStatusDraft {
			return ierr.NewError("invoice is not in draft status").
				WithHint("invoice must be in draft status to be edited").
				Mark(ierr.ErrValidation)
		}
		lockedInv = inv

		existingItem, err := s.InvoiceLineItemRepo.Get(txCtx, lineItemID)
		if err != nil {
			return err
		}
		if existingItem.InvoiceID != invoiceID {
			return ierr.NewError("line item not found").
				WithHintf("line item %s does not belong to invoice %s", lineItemID, invoiceID).
				Mark(ierr.ErrNotFound)
		}
		if existingItem.Status != types.StatusPublished {
			// Editing an already-archived/deleted row would branch the lineage
			// chain instead of extending it, or resurrect a removed item.
			return ierr.NewError("line item is not editable").
				WithHintf("line item %s has status %s and is not the current version", lineItemID, existingItem.Status).
				Mark(ierr.ErrValidation)
		}

		newItem := *existingItem
		newItem.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM)
		newItem.ParentLineItemID = &existingItem.ID
		newItem.BaseModel = types.GetDefaultBaseModel(txCtx)

		if req.DisplayName != nil {
			newItem.DisplayName = req.DisplayName
		}
		if req.Amount != nil {
			newItem.Amount = *req.Amount
		}
		if req.Quantity != nil {
			newItem.Quantity = *req.Quantity
		}

		if err := newItem.Validate(); err != nil {
			return err
		}

		existingItem.Status = types.StatusArchived
		if err := s.InvoiceLineItemRepo.Update(txCtx, existingItem); err != nil {
			return err
		}

		if err := s.InvoiceLineItemRepo.Create(txCtx, &newItem); err != nil {
			return err
		}

		remaining := make([]*invoice.InvoiceLineItem, 0, len(lockedInv.LineItems))
		for _, li := range lockedInv.LineItems {
			if li.ID == existingItem.ID {
				continue
			}
			remaining = append(remaining, li)
		}
		publishedLineItems = append(remaining, &newItem)

		s.recalculateTotalsFromLineItems(lockedInv, publishedLineItems)
		if req.MarkManuallyEdited {
			lockedInv.IsManuallyEdited = true
		}

		return s.InvoiceRepo.Update(txCtx, lockedInv)
	})
	if err != nil {
		return nil, err
	}

	lockedInv.LineItems = publishedLineItems
	return dto.NewInvoiceResponse(lockedInv), nil
}

func (s *invoiceService) AddBulkLineItem(ctx context.Context, invoiceID string, req dto.AddBulkLineItemRequest) (*dto.InvoiceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var lockedInv *invoice.Invoice
	var publishedLineItems []*invoice.InvoiceLineItem

	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		inv, err := s.InvoiceRepo.GetForUpdate(txCtx, invoiceID)
		if err != nil {
			return err
		}
		if inv.InvoiceStatus != types.InvoiceStatusDraft {
			return ierr.NewError("invoice is not in draft status").
				WithHint("invoice must be in draft status to be edited").
				Mark(ierr.ErrValidation)
		}
		lockedInv = inv

		newItems := make([]*invoice.InvoiceLineItem, len(req.Items))
		for i, item := range req.Items {
			newItem := &invoice.InvoiceLineItem{
				ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
				InvoiceID:     inv.ID,
				CustomerID:    inv.CustomerID,
				EnvironmentID: inv.EnvironmentID,
				DisplayName:   lo.ToPtr(item.DisplayName),
				Amount:        item.Amount,
				Quantity:      item.Quantity,
				Currency:      inv.Currency,
				BaseModel:     types.GetDefaultBaseModel(txCtx),
			}
			if err := newItem.Validate(); err != nil {
				return err
			}
			newItems[i] = newItem
		}

		if err := s.InvoiceLineItemRepo.CreateBulk(txCtx, newItems); err != nil {
			return err
		}

		publishedLineItems = make([]*invoice.InvoiceLineItem, 0, len(lockedInv.LineItems)+len(newItems))
		publishedLineItems = append(publishedLineItems, lockedInv.LineItems...)
		publishedLineItems = append(publishedLineItems, newItems...)

		s.recalculateTotalsFromLineItems(lockedInv, publishedLineItems)
		if req.MarkManuallyEdited {
			lockedInv.IsManuallyEdited = true
		}

		return s.InvoiceRepo.Update(txCtx, lockedInv)
	})
	if err != nil {
		return nil, err
	}

	lockedInv.LineItems = publishedLineItems
	return dto.NewInvoiceResponse(lockedInv), nil
}

func (s *invoiceService) RemoveBulkLineItem(ctx context.Context, invoiceID string, req dto.RemoveBulkLineItemRequest) (*dto.InvoiceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var lockedInv *invoice.Invoice
	var publishedLineItems []*invoice.InvoiceLineItem

	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		inv, err := s.InvoiceRepo.GetForUpdate(txCtx, invoiceID)
		if err != nil {
			return err
		}
		if inv.InvoiceStatus != types.InvoiceStatusDraft {
			return ierr.NewError("invoice is not in draft status").
				WithHint("invoice must be in draft status to be edited").
				Mark(ierr.ErrValidation)
		}
		lockedInv = inv

		removedIDs := make(map[string]bool, len(req.LineItemIDs))
		for _, lineItemID := range req.LineItemIDs {
			existingItem, err := s.InvoiceLineItemRepo.Get(txCtx, lineItemID)
			if err != nil {
				return err
			}
			if existingItem.InvoiceID != invoiceID {
				return ierr.NewError("line item not found").
					WithHintf("line item %s does not belong to invoice %s", lineItemID, invoiceID).
					Mark(ierr.ErrNotFound)
			}
			if existingItem.Status != types.StatusPublished {
				return ierr.NewError("line item is not removable").
					WithHintf("line item %s has status %s and is not the current version", lineItemID, existingItem.Status).
					Mark(ierr.ErrValidation)
			}
			removedIDs[lineItemID] = true
		}

		if err := s.InvoiceRepo.RemoveLineItems(txCtx, invoiceID, req.LineItemIDs); err != nil {
			return err
		}

		remaining := make([]*invoice.InvoiceLineItem, 0, len(lockedInv.LineItems))
		for _, li := range lockedInv.LineItems {
			if removedIDs[li.ID] {
				continue
			}
			remaining = append(remaining, li)
		}
		publishedLineItems = remaining

		s.recalculateTotalsFromLineItems(lockedInv, publishedLineItems)
		if req.MarkManuallyEdited {
			lockedInv.IsManuallyEdited = true
		}

		return s.InvoiceRepo.Update(txCtx, lockedInv)
	})
	if err != nil {
		return nil, err
	}

	lockedInv.LineItems = publishedLineItems
	return dto.NewInvoiceResponse(lockedInv), nil
}
