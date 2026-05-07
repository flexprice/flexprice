package payload

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
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
func (b *InvoicePayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var parsedPayload webhookDto.InternalInvoiceEvent

	err := json.Unmarshal(data, &parsedPayload)
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
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}

	// TODO: this is a temporary fix to handle the invoice not found error.
	if ierr.IsNotFound(err) {
		time.Sleep(15 * time.Second)
		invoice, err = b.services.InvoiceService.GetInvoice(ctx, invoiceID)
		if err != nil {
			return nil, err
		}
	}

	// inject the invoice pdf url into the invoice response
	pdfUrl, err := b.services.InvoiceService.GetInvoicePDFUrl(ctx, invoiceID, false)
	if err != nil {
		b.services.Sentry.CaptureException(err)

	}
	invoice.InvoicePDFURL = lo.ToPtr(pdfUrl)
	invoice.Subscription = nil

	payload := webhookDto.NewInvoiceWebhookPayload(invoice, eventType)

	// Return the invoice response as is
	return json.Marshal(payload)
}
