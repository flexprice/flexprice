package webhookDto

import "github.com/flexprice/flexprice/internal/api/dto"

type InternalInvoiceEvent struct {
	InvoiceID string `json:"invoice_id"`
	TenantID  string `json:"tenant_id"`
}

type InvoiceWebhookPayload struct {
	Invoice *dto.InvoiceResponse
}

func NewInvoiceWebhookPayload(invoice *dto.InvoiceResponse) *InvoiceWebhookPayload {
	return &InvoiceWebhookPayload{Invoice: invoice}
}
