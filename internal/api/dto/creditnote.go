package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

type CreateCreditNoteRequest struct {
	CreditNoteNumber string                            `json:"credit_note_number" validate:"omitempty"`
	InvoiceID        string                            `json:"invoice_id" validate:"required"`
	Memo             string                            `json:"memo" validate:"omitempty"`
	Reason           types.CreditNoteReason            `json:"reason" validate:"required"`
	Metadata         types.Metadata                    `json:"metadata" validate:"omitempty"`
	LineItems        []CreateCreditNoteLineItemRequest `json:"line_items"`
}

func (r *CreateCreditNoteRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if err := r.Reason.Validate(); err != nil {
		return err
	}

	if len(r.LineItems) == 0 {
		return ierr.NewError("line_items is required").
			WithHint("Please provide at least one line item").
			Mark(ierr.ErrValidation)
	}

	for _, lineItem := range r.LineItems {
		if err := lineItem.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (r *CreateCreditNoteRequest) ToCreditNote(ctx context.Context, inv *invoice.Invoice) *creditnote.CreditNote {
	cn := &creditnote.CreditNote{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_NOTE),
		EnvironmentID:    types.GetEnvironmentID(ctx),
		InvoiceID:        r.InvoiceID,
		Memo:             r.Memo,
		CreditNoteNumber: r.CreditNoteNumber,
		CreditNoteStatus: types.CreditNoteStatusDraft,
		CreditNoteType:   types.CreditNoteTypeAdjustment,
		Reason:           r.Reason,
		Currency:         inv.Currency,
		TotalAmount:      decimal.Zero,
		Metadata:         r.Metadata,
		LineItems:        make([]*creditnote.CreditNoteLineItem, 0),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}

	for _, lineItem := range r.LineItems {
		cn.LineItems = append(cn.LineItems, lineItem.ToCreditNoteLineItem(ctx, cn))
		cn.TotalAmount = cn.TotalAmount.Add(lineItem.Amount)
	}

	return cn
}

func (r *CreateCreditNoteLineItemRequest) Validate() error {

	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	return nil
}

type CreateCreditNoteLineItemRequest struct {
	InvoiceLineItemID string          `json:"invoice_line_item_id" validate:"required"`
	DisplayName       string          `json:"display_name" validate:"omitempty"`
	Amount            decimal.Decimal `json:"amount" validate:"required"`
	Metadata          types.Metadata  `json:"metadata" validate:"omitempty"`
}

func (r *CreateCreditNoteLineItemRequest) ToCreditNoteLineItem(ctx context.Context, cn *creditnote.CreditNote) *creditnote.CreditNoteLineItem {
	return &creditnote.CreditNoteLineItem{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_NOTE_LINE_ITEM),
		InvoiceLineItemID: r.InvoiceLineItemID,
		DisplayName:       r.DisplayName,
		Amount:            r.Amount,
		Metadata:          r.Metadata,
		CreditNoteID:      cn.ID,
		Currency:          cn.Currency,
		EnvironmentID:     types.GetEnvironmentID(ctx),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
}

type CreditNoteResponse struct {
	*creditnote.CreditNote
}

// ListCreditNotesResponse represents the response for listing credit notes
type ListCreditNotesResponse = types.ListResponse[*CreditNoteResponse]
