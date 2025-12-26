package creditnote

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CreditNote is the model entity for the CreditNote schema.
type CreditNote struct {
	// id is the unique identifier for the credit note
	ID string `json:"id"`

	// credit_note_number is the unique identifier for credit notes
	CreditNoteNumber string `json:"credit_note_number"`

	// invoice_id is the id of the invoice resource that this credit note is applied to
	InvoiceID string `json:"invoice_id"`

	// customer_id is the unique identifier of the customer who owns this credit note
	CustomerID string `json:"customer_id"`

	// subscription_id is the optional unique identifier of the subscription related to this credit note
	SubscriptionID *string `json:"subscription_id,omitempty"`

	// credit_note_status represents the current status of the credit note (e.g., draft, finalized, voided)
	CreditNoteStatus types.CreditNoteStatus `json:"credit_note_status"`

	// credit_note_type indicates the type of credit note (refund, adjustment)
	CreditNoteType types.CreditNoteType `json:"credit_note_type"`

	// refund_status represents the status of any refund associated with this credit note
	RefundStatus *types.PaymentStatus `json:"refund_status"`

	// reason specifies the reason for creating this credit note (duplicate, fraudulent, order_change, product_unsatisfactory)
	Reason types.CreditNoteReason `json:"reason"`

	// memo is an optional memo supplied on the credit note
	Memo string `json:"memo"`

	// currency is the three-letter ISO currency code (e.g., USD, EUR) for the credit note
	Currency string `json:"currency"`

	// metadata contains additional custom key-value pairs for storing extra information
	Metadata types.Metadata `json:"metadata"`

	// line_items contains all of the line items associated with this credit note
	LineItems []*CreditNoteLineItem `json:"line_items"`

	// environment_id is the unique identifier of the environment this credit note belongs to
	EnvironmentID string `json:"environment_id"`

	// total_amount is the total including creditable invoice-level discounts or minimums, and tax
	TotalAmount decimal.Decimal `json:"total_amount" swaggertype:"string"`

	// voided_at is the timestamp when the credit note was voided
	VoidedAt *time.Time `json:"voided_at,omitempty"`

	// finalized_at is the timestamp when the credit note was finalized
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`

	// idempotency_key is an optional key used to prevent duplicate credit note creation
	IdempotencyKey *string `json:"idempotency_key"`

	types.BaseModel
}

// FromEnt converts an ent credit note to domain credit note
func FromEnt(e *ent.CreditNote) *CreditNote {

	creditNoteLineItem := CreditNoteLineItem{}

	return &CreditNote{
		ID:               e.ID,
		EnvironmentID:    e.EnvironmentID,
		CreditNoteNumber: e.CreditNoteNumber,
		InvoiceID:        e.InvoiceID,
		CreditNoteStatus: e.CreditNoteStatus,
		CreditNoteType:   e.CreditNoteType,
		RefundStatus:     e.RefundStatus,
		Reason:           e.Reason,
		Memo:             e.Memo,
		Currency:         e.Currency,
		Metadata:         e.Metadata,
		CustomerID:       e.CustomerID,
		SubscriptionID:   e.SubscriptionID,
		VoidedAt:         e.VoidedAt,
		FinalizedAt:      e.FinalizedAt,
		LineItems:        creditNoteLineItem.FromEntList(e.Edges.LineItems),
		TotalAmount:      e.TotalAmount,
		IdempotencyKey:   e.IdempotencyKey,
		BaseModel: types.BaseModel{
			Status:    types.Status(e.Status),
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
			TenantID:  e.TenantID,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		},
	}
}

// FromEntList converts a list of ent credit notes to domain credit notes
func FromEntList(creditNotes []*ent.CreditNote) []*CreditNote {
	result := make([]*CreditNote, len(creditNotes))
	for i, cn := range creditNotes {
		result[i] = FromEnt(cn)
	}
	return result
}
