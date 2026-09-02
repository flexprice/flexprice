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

		builder := invoice.NewInvoiceLineItemBuilder(existingItem).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM)).
			WithParentLineItemID(lo.ToPtr(existingItem.ID)).
			WithBaseModel(types.GetDefaultBaseModel(txCtx))

		if req.DisplayName != nil {
			builder = builder.WithDisplayName(req.DisplayName)
		}
		if req.Amount != nil {
			builder = builder.WithAmount(*req.Amount)
		}
		if req.Quantity != nil {
			builder = builder.WithQuantity(*req.Quantity)
		}

		newItem := builder.Build()
		if err := newItem.Validate(); err != nil {
			return err
		}

		existingItem.Status = types.StatusArchived
		if err := s.InvoiceLineItemRepo.Update(txCtx, existingItem); err != nil {
			return err
		}

		if err := s.InvoiceLineItemRepo.Create(txCtx, newItem); err != nil {
			return err
		}

		remaining := make([]*invoice.InvoiceLineItem, 0, len(lockedInv.LineItems))
		for _, li := range lockedInv.LineItems {
			if li.ID == existingItem.ID {
				continue
			}
			remaining = append(remaining, li)
		}
		publishedLineItems = append(remaining, newItem)

		s.recalculateTotalsFromLineItems(lockedInv, publishedLineItems)
		lockedInv.IsManuallyEdited = true

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

		newItems := lo.Map(req.Items, func(item dto.AddLineItemRequest, _ int) *invoice.InvoiceLineItem {
			return invoice.NewInvoiceLineItemBuilder(nil).
				WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM)).
				WithInvoiceID(inv.ID).
				WithCustomerID(inv.CustomerID).
				WithEnvironmentID(inv.EnvironmentID).
				WithDisplayName(lo.ToPtr(item.DisplayName)).
				WithAmount(item.Amount).
				WithQuantity(item.Quantity).
				WithCurrency(inv.Currency).
				WithBaseModel(types.GetDefaultBaseModel(txCtx)).
				Build()
		})
		for _, newItem := range newItems {
			if err := newItem.Validate(); err != nil {
				return err
			}
		}

		if err := s.InvoiceLineItemRepo.CreateBulk(txCtx, newItems); err != nil {
			return err
		}

		publishedLineItems = make([]*invoice.InvoiceLineItem, 0, len(lockedInv.LineItems)+len(newItems))
		publishedLineItems = append(publishedLineItems, lockedInv.LineItems...)
		publishedLineItems = append(publishedLineItems, newItems...)

		s.recalculateTotalsFromLineItems(lockedInv, publishedLineItems)
		lockedInv.IsManuallyEdited = true

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
		lockedInv.IsManuallyEdited = true

		return s.InvoiceRepo.Update(txCtx, lockedInv)
	})
	if err != nil {
		return nil, err
	}

	lockedInv.LineItems = publishedLineItems
	return dto.NewInvoiceResponse(lockedInv), nil
}

func (s *invoiceService) ModifyInvoice(ctx context.Context, invoiceID string, req dto.ExecuteInvoiceModifyRequest) (*dto.InvoiceModifyResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	switch req.Type {
	case dto.InvoiceModifyTypeLineItem:
		return s.executeLineItemModification(ctx, invoiceID, req.LineItemParams)
	default:
		return nil, ierr.NewError("unknown modification type: " + string(req.Type)).
			WithHint("valid values: line_item").
			Mark(ierr.ErrValidation)
	}
}

func (s *invoiceService) executeLineItemModification(ctx context.Context, invoiceID string, params *dto.InvoiceModifyLineItemParams) (*dto.InvoiceModifyResponse, error) {
	var (
		resp *dto.InvoiceResponse
		err  error
	)

	switch params.Action {
	case dto.InvoiceModifyLineItemActionAdd:
		resp, err = s.AddBulkLineItem(ctx, invoiceID, dto.AddBulkLineItemRequest{
			Items: params.Items,
		})
	case dto.InvoiceModifyLineItemActionRemove:
		resp, err = s.RemoveBulkLineItem(ctx, invoiceID, dto.RemoveBulkLineItemRequest{
			LineItemIDs: params.LineItemIDs,
		})
	default:
		return nil, ierr.NewError("unknown line item action: " + string(params.Action)).
			WithHint("valid values: add, remove").
			Mark(ierr.ErrValidation)
	}
	if err != nil {
		return nil, err
	}

	return &dto.InvoiceModifyResponse{Invoice: resp}, nil
}
