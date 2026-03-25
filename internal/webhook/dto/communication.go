package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalCommunicationEvent struct {
	InvoiceID string `json:"invoice_id"`
	TenantID  string `json:"tenant_id"`
}

type CommunicationWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Invoice   *dto.InvoiceResponse   `json:"invoice"`
}

func NewCommunicationWebhookPayload(invoice *dto.InvoiceResponse, eventType types.WebhookEventName) *CommunicationWebhookPayload {
	return &CommunicationWebhookPayload{EventType: eventType, Invoice: invoice}
}
