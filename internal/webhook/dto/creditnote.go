package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type InternalCreditNoteEvent struct {
	CreditNoteID string `json:"credit_note_id"`
	TenantID     string `json:"tenant_id"`
}

type CreditNote struct {
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
	Metadata         types.Metadata         `json:"metadata,omitempty"`
}

func NewCreditNote(resp *dto.CreditNoteResponse) *CreditNote {
	if resp == nil || resp.CreditNote == nil {
		return nil
	}
	return &CreditNote{
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
		Metadata:         resp.Metadata,
	}
}

type CreditNoteWebhookPayload struct {
	EventType  types.WebhookEventName `json:"event_type"`
	CreditNote *CreditNote            `json:"credit_note"`
}

func NewCreditNoteWebhookPayload(creditNote *dto.CreditNoteResponse, eventType types.WebhookEventName) *CreditNoteWebhookPayload {
	return &CreditNoteWebhookPayload{EventType: eventType, CreditNote: NewCreditNote(creditNote)}
}
