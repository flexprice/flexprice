package payload

import (
	"context"
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
)

type CommunicationPayloadBuilder struct {
	services *Services
}

func NewCommunicationPayloadBuilder(services *Services) PayloadBuilder {
	return &CommunicationPayloadBuilder{
		services: services,
	}
}

// BuildPayload builds the webhook payload for communication events
func (b *CommunicationPayloadBuilder) BuildPayload(ctx context.Context, eventType string, data json.RawMessage) (json.RawMessage, error) {
	var parsedPayload webhookDto.InternalCommunicationEvent

	err := json.Unmarshal(data, &parsedPayload)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal communication event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	invoiceID, tenantID := parsedPayload.InvoiceID, parsedPayload.TenantID
	if invoiceID == "" || tenantID == "" {
		return nil, ierr.NewError("missing required field(s) for communication event").
			WithHint("Please provide a valid invoice ID and tenant ID").
			WithReportableDetails(map[string]any{
				"invoice_id": invoiceID,
				"tenant_id":  tenantID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Get invoice details
	invoice, err := b.services.InvoiceService.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}

	// inject the invoice pdf url into the invoice response
	pdfUrl, err := b.services.InvoiceService.GetInvoicePDFUrl(ctx, invoiceID)
	if err != nil {
		b.services.Sentry.CaptureException(err)
	}
	invoice.InvoicePDFURL = lo.ToPtr(pdfUrl)

	payload := webhookDto.NewCommunicationWebhookPayload(invoice, eventType)

	// Return the communication payload
	return json.Marshal(payload)
}
