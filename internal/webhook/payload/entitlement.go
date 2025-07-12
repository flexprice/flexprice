package payload

import (
	"context"
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type EntitlementPayloadBuilder struct {
	services *Services
}

func NewEntitlementPayloadBuilder(services *Services) PayloadBuilder {
	return &EntitlementPayloadBuilder{services: services}
}

func (b *EntitlementPayloadBuilder) BuildPayload(ctx context.Context, eventType string, data json.RawMessage) (json.RawMessage, error) {
	var parsedPayload webhookDto.InternalEntitlementEvent

	err := json.Unmarshal(data, &parsedPayload)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal entitlement event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	entitlementID, tenantID := parsedPayload.EntitlementID, parsedPayload.TenantID
	if entitlementID == "" || tenantID == "" {
		return nil, ierr.NewError("invalid data type for entitlement event").
			WithHint("Please provide a valid entitlement ID and tenant ID").
			WithReportableDetails(map[string]any{
				"entitlement_id": entitlementID,
				"tenant_id":      tenantID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	entitlement, err := b.services.EntitlementService.GetEntitlement(ctx, entitlementID)
	if err != nil {
		return nil, err
	}

	payload := webhookDto.NewEntitlementWebhookPayload(entitlement)

	return json.Marshal(payload)
}
