package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalInvoiceEvent struct {
	InvoiceID string `json:"invoice_id"`
	TenantID  string `json:"tenant_id"`
}

type InvoiceWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Invoice   *dto.InvoiceResponse   `json:"invoice"`
}

func NewInvoiceWebhookPayload(invoice *dto.InvoiceResponse, eventType types.WebhookEventName) *InvoiceWebhookPayload {
	return &InvoiceWebhookPayload{EventType: eventType, Invoice: invoice}
}
