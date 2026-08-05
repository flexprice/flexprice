package outbound

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CreditNoteWebhookPayload is the minimal webhook representation of a credit note.
type CreditNoteWebhookPayload struct {
	ID               string                 `json:"id"`
	CreditNoteNumber string                 `json:"credit_note_number,omitempty"`
	InvoiceID        string                 `json:"invoice_id"`
	CustomerID       string                 `json:"customer_id"`
	SubscriptionID   *string                `json:"subscription_id,omitempty"`
	CreditNoteStatus types.CreditNoteStatus `json:"credit_note_status"`
	CreditNoteType   types.CreditNoteType   `json:"credit_note_type"`
	Reason           types.CreditNoteReason `json:"reason"`
	Memo             string                 `json:"memo,omitempty"`
	Currency         string                 `json:"currency"`
	TotalAmount      decimal.Decimal        `json:"total_amount" swaggertype:"string"`
	VoidedAt         *time.Time             `json:"voided_at,omitempty"`
	FinalizedAt      *time.Time             `json:"finalized_at,omitempty"`
}

func NewCreditNoteWebhookPayload(resp *dto.CreditNoteResponse) *CreditNoteWebhookPayload {
	if resp == nil || resp.CreditNote == nil {
		return nil
	}
	return &CreditNoteWebhookPayload{
		ID:               resp.ID,
		CreditNoteNumber: resp.CreditNoteNumber,
		InvoiceID:        resp.InvoiceID,
		CustomerID:       resp.CustomerID,
		SubscriptionID:   resp.SubscriptionID,
		CreditNoteStatus: resp.CreditNoteStatus,
		CreditNoteType:   resp.CreditNoteType,
		Reason:           resp.Reason,
		Memo:             resp.Memo,
		Currency:         resp.Currency,
		TotalAmount:      resp.TotalAmount,
		VoidedAt:         resp.VoidedAt,
		FinalizedAt:      resp.FinalizedAt,
	}
}
