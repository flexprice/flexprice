package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalCreditNoteEvent struct {
	CreditNoteID string `json:"credit_note_id"`
	TenantID     string `json:"tenant_id"`
}

type CreditNoteWebhookPayload struct {
	EventType  types.WebhookEventName  `json:"event_type"`
	CreditNote *dto.CreditNoteResponse `json:"credit_note"`
}

func NewCreditNoteWebhookPayload(creditNote *dto.CreditNoteResponse, eventType types.WebhookEventName) *CreditNoteWebhookPayload {
	return &CreditNoteWebhookPayload{EventType: eventType, CreditNote: creditNote}
}
