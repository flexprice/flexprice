package payload

import (
	"context"
	"encoding/json"
	"fmt"
)

type InvoicePayloadBuilder struct {
	services *Services
}

func NewInvoicePayloadBuilder(services *Services) PayloadBuilder {
	return &InvoicePayloadBuilder{
		services: services,
	}
}

// BuildPayload builds the webhook payload for invoice events
func (b *InvoicePayloadBuilder) BuildPayload(ctx context.Context, eventType string, data interface{}) (json.RawMessage, error) {
	parsedPayload := struct {
		InvoiceID string `json:"invoice_id"`
		TenantID  string `json:"tenant_id"`
	}{}

	err := json.Unmarshal(data.(json.RawMessage), &parsedPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal invoice event payload: %w", err)
	}

	invoiceID, tenantID := parsedPayload.InvoiceID, parsedPayload.TenantID
	if invoiceID == "" || tenantID == "" {
		return nil, fmt.Errorf("invalid data type for invoice event, expected string got %T", data)
	}

	// Get invoice details
	invoice, err := b.services.InvoiceService.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get invoice: %w", err)
	}

	// Return the invoice response as is
	return json.Marshal(invoice)
}
