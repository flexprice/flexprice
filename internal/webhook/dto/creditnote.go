package webhookDto

import "github.com/flexprice/flexprice/internal/api/dto"

type InternalCreditNoteEvent struct {
	CreditNoteID string `json:"credit_note_id"`
	TenantID     string `json:"tenant_id"`
}

type CreditNoteWebhookPayload struct {
	EventType  string                  `json:"event_type"`
	CreditNote *dto.CreditNoteResponse `json:"credit_note"`
}

func NewCreditNoteWebhookPayload(creditNote *dto.CreditNoteResponse, eventType string) *CreditNoteWebhookPayload {
	return &CreditNoteWebhookPayload{EventType: eventType, CreditNote: creditNote}
}
