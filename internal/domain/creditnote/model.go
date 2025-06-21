package creditnote

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CreditNote is the model entity for the CreditNote schema.
type CreditNote struct {
	ID               string                 `json:"id"`
	CreditNoteNumber string                 `json:"credit_note_number"`
	InvoiceID        string                 `json:"invoice_id"`
	CreditNoteStatus types.CreditNoteStatus `json:"credit_note_status"`
	CreditNoteType   types.CreditNoteType   `json:"credit_note_type"`
	RefundStatus     *types.PaymentStatus   `json:"refund_status"`
	Reason           types.CreditNoteReason `json:"reason"`
	Memo             string                 `json:"memo"`
	Currency         string                 `json:"currency"`
	Metadata         types.Metadata         `json:"metadata"`
	LineItems        []*CreditNoteLineItem  `json:"line_items"`
	EnvironmentID    string                 `json:"environment_id"`
	TotalAmount      decimal.Decimal        `json:"total_amount"`
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
		LineItems:        creditNoteLineItem.FromEntList(e.Edges.LineItems),
		TotalAmount:      e.TotalAmount,
		BaseModel: types.BaseModel{
			Status:    types.Status(e.Status),
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
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
