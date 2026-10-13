package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// InternalInvoiceSyncEvent is what the publisher puts in WebhookEvent.Payload.
type InternalInvoiceSyncEvent struct {
	InvoiceID string               `json:"invoice_id"`
	TenantID  string               `json:"tenant_id"`
	Provider  types.SecretProvider `json:"provider"`
	Error     string               `json:"error,omitempty"`
}

// ProviderDetails describes the provider's copy of a synced invoice.
type ProviderDetails struct {
	Provider         types.SecretProvider `json:"provider"`
	InvoiceID        *string              `json:"invoice_id"`
	HostedInvoiceURL *string              `json:"hosted_invoice_url"`
}

type InvoiceSyncWebhookPayload struct {
	EventType       types.WebhookEventName `json:"event_type"`
	Invoice         *Invoice               `json:"invoice"`
	ProviderDetails *ProviderDetails       `json:"provider_details"`
	Error           *string                `json:"error"`
}

func NewInvoiceSyncWebhookPayload(invoice *dto.InvoiceResponse, details *ProviderDetails, syncErr string, eventType types.WebhookEventName) *InvoiceSyncWebhookPayload {
	payload := &InvoiceSyncWebhookPayload{
		EventType:       eventType,
		Invoice:         NewInvoice(invoice),
		ProviderDetails: details,
	}
	if syncErr != "" {
		payload.Error = &syncErr
	}
	return payload
}
