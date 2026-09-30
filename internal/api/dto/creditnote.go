package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// CreateCreditNoteRequest represents the request payload for creating a new credit note
type CreateCreditNoteRequest struct {
	// credit_note_number is an optional human-readable identifier for the credit note
	CreditNoteNumber string `json:"credit_note_number" validate:"omitempty"`

	// invoice_id is the unique identifier of the invoice this credit note is applied to
	InvoiceID string `json:"invoice_id" validate:"required"`

	// memo is an optional free-text field for additional notes about the credit note
	Memo string `json:"memo" validate:"omitempty"`

	// reason specifies the reason for creating this credit note (duplicate, fraudulent, order_change, product_unsatisfactory)
	Reason types.CreditNoteReason `json:"reason" validate:"required"`

	// metadata contains additional custom key-value pairs for storing extra information
	Metadata types.Metadata `json:"metadata" validate:"omitempty"`

	// line_items contains the individual line items that make up this credit note (minimum 1 required)
	LineItems []CreateCreditNoteLineItemRequest `json:"line_items"`

	// idempotency_key is an optional key used to prevent duplicate credit note creation
	IdempotencyKey *string `json:"idempotency_key" validate:"omitempty"`

	// process_credit_note is a flag to process the credit note after creation
	ProcessCreditNote bool `json:"process_credit_note" validate:"omitempty" default:"true"`

	// refund_target asks where a refund credit note should send the money: BACK_TO_SOURCE
	// returns it the way it arrived, PREPAID_WALLET (the default) keeps it as customer credit.
	// Only honoured when the credit note is processed by this same call; a separately
	// finalized credit note takes it from the finalize request instead.
	RefundTarget *types.RefundTarget `json:"refund_target" validate:"omitempty"`
}

func (r *CreateCreditNoteRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.RefundTarget != nil {
		if err := r.RefundTarget.Validate(); err != nil {
			return err
		}
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
		CustomerID:       inv.CustomerID,
		SubscriptionID:   inv.SubscriptionID,
		Memo:             r.Memo,
		CreditNoteNumber: r.CreditNoteNumber,
		CreditNoteStatus: types.CreditNoteStatusDraft,
		CreditNoteType:   types.CreditNoteTypeAdjustment,
		Reason:           r.Reason,
		Currency:         inv.Currency,
		TotalAmount:      decimal.Zero,
		Metadata:         r.Metadata,
		LineItems:        make([]*creditnote.CreditNoteLineItem, 0),
		IdempotencyKey:   r.IdempotencyKey,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}

	for _, lineItem := range r.LineItems {
		cn.LineItems = append(cn.LineItems, lineItem.ToCreditNoteLineItem(ctx, cn))
		cn.TotalAmount = cn.TotalAmount.Add(lineItem.Amount)
	}

	return cn
}

// CreditNotePreviewResponse is what a credit note would come to if it were issued now. The tax
// on a credit is only known once the engine has been asked, so the tenant sees what the customer
// will actually get back before committing to it. Nothing is written to produce this.
type CreditNotePreviewResponse struct {
	// Subtotal is the sum of the requested line items, before tax.
	Subtotal decimal.Decimal `json:"subtotal" swaggertype:"string"`

	// TotalTax is the tax on that subtotal. Zero when no external engine is configured.
	TotalTax decimal.Decimal `json:"total_tax" swaggertype:"string"`

	// TotalAmount is what the customer gets back, tax included.
	TotalAmount decimal.Decimal `json:"total_amount" swaggertype:"string"`

	Currency string `json:"currency"`

	// CreditNoteType is what this would be issued as, which follows the invoice's payment state.
	CreditNoteType types.CreditNoteType `json:"credit_note_type"`

	// Taxes are the individual taxes making up TotalTax, for display.
	Taxes []*TaxAppliedResponse `json:"taxes,omitempty"`
}

// CreateCreditNoteLineItemRequest represents a single line item in a credit note creation request
type CreateCreditNoteLineItemRequest struct {
	// invoice_line_item_id is the unique identifier of the invoice line item being credited
	InvoiceLineItemID string `json:"invoice_line_item_id" validate:"required"`

	// display_name is an optional human-readable name for this credit note line item
	DisplayName string `json:"display_name" validate:"omitempty"`

	// amount is the monetary amount to be credited for this line item
	Amount decimal.Decimal `json:"amount" validate:"required" swaggertype:"string"`

	// metadata contains additional custom key-value pairs for storing extra information about this line item
	Metadata types.Metadata `json:"metadata" validate:"omitempty"`
}

func (r *CreateCreditNoteLineItemRequest) Validate() error {

	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	return nil
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
		BaseModel:         types.GetDefaultBaseModel(ctx),
		EnvironmentID:     types.GetEnvironmentID(ctx),
	}
}

// FinalizeCreditNoteRequest is the optional body of the finalize call.
type FinalizeCreditNoteRequest struct {
	// refund_target asks where a refund credit note should send the money: BACK_TO_SOURCE
	// returns it the way it arrived, PREPAID_WALLET (the default) keeps it as customer credit.
	RefundTarget *types.RefundTarget `json:"refund_target" validate:"omitempty"`
}

func (r *FinalizeCreditNoteRequest) Validate() error {
	if r == nil || r.RefundTarget == nil {
		return nil
	}
	return r.RefundTarget.Validate()
}

// CreditNoteResponse represents the response payload containing credit note information
type CreditNoteResponse struct {
	*creditnote.CreditNote

	// invoice contains the associated invoice information if requested
	Invoice *InvoiceResponse `json:"invoice,omitempty"`

	// subscription contains the associated subscription information if applicable
	Subscription *SubscriptionResponse `json:"subscription,omitempty"`

	// customer contains the customer information associated with this credit note
	Customer *customer.Customer `json:"customer,omitempty"`
}

// ListCreditNotesResponse represents the paginated response for listing credit notes
type ListCreditNotesResponse = types.ListResponse[*CreditNoteResponse]
