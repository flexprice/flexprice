package payload

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
)

type InvoiceSyncPayloadBuilder struct {
	services *Services
}

func NewInvoiceSyncPayloadBuilder(services *Services) PayloadBuilder {
	return &InvoiceSyncPayloadBuilder{services: services}
}

func (b *InvoiceSyncPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var internal webhookDto.InternalInvoiceSyncEvent
	if err := json.Unmarshal(data, &internal); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal invoice sync event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	if internal.InvoiceID == "" || internal.TenantID == "" || internal.Provider == "" {
		return nil, ierr.NewError("missing required field(s) for invoice sync event").
			WithHint("Please provide a valid invoice ID, tenant ID and provider").
			WithReportableDetails(map[string]any{
				"invoice_id": internal.InvoiceID,
				"tenant_id":  internal.TenantID,
				"provider":   internal.Provider,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	invoice, err := b.services.InvoiceService.GetInvoice(ctx, internal.InvoiceID)
	if err != nil {
		return nil, err
	}

	pdfURL, err := b.services.InvoiceService.GetInvoicePDFUrl(ctx, dto.InvoicePDFRequest{InvoiceID: internal.InvoiceID})
	if err != nil {
		b.services.Tracing.CaptureException(ctx, err)
	}
	invoice.InvoicePDFURL = lo.ToPtr(pdfURL)

	mappings, err := b.services.EntityIntegrationMappingService.GetEntityIntegrationMappings(ctx, &types.EntityIntegrationMappingFilter{
		EntityID:      internal.InvoiceID,
		EntityType:    types.IntegrationEntityTypeInvoice,
		ProviderTypes: []string{string(internal.Provider)},
		QueryFilter:   types.NewDefaultQueryFilter(),
	})
	if err != nil {
		return nil, err
	}
	mapping, _ := lo.First(mappings.Items)

	details := buildProviderDetails(internal.Provider, mapping, invoice)
	return json.Marshal(webhookDto.NewInvoiceSyncWebhookPayload(invoice, details, internal.Error, eventType))
}

func buildProviderDetails(provider types.SecretProvider, mapping *dto.EntityIntegrationMappingResponse, invoice *dto.InvoiceResponse) *webhookDto.ProviderDetails {
	details := &webhookDto.ProviderDetails{Provider: provider}
	if mapping != nil {
		details.InvoiceID = lo.ToPtr(mapping.ProviderEntityID)
	}

	if provider == types.SecretProviderStripe {
		if url := invoice.Metadata[types.InvoiceMetadataKeyStripeHostedInvoiceURL]; url != "" {
			details.HostedInvoiceURL = lo.ToPtr(url)
		}
	}

	return details
}
