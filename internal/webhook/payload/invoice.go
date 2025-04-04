package payload

import (
	"context"
	"encoding/json"
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
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
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal invoice event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	invoiceID, tenantID := parsedPayload.InvoiceID, parsedPayload.TenantID
	if invoiceID == "" || tenantID == "" {
		return nil, ierr.NewError("invalid data type for invoice event").
			WithHint("Please provide a valid invoice ID and tenant ID").
			WithReportableDetails(map[string]any{
				"expected": "string",
				"got":      fmt.Sprintf("%T", data),
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Get invoice details
	invoice, err := b.services.InvoiceService.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}

	// Return the invoice response as is
	return json.Marshal(invoice)
}
