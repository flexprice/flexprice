package payload

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type Entitlement struct {
	ID               string                            `json:"id"`
	EntityType       types.EntitlementEntityType       `json:"entity_type"`
	EntityID         string                            `json:"entity_id"`
	FeatureID        string                            `json:"feature_id"`
	FeatureType      types.FeatureType                 `json:"feature_type"`
	IsEnabled        bool                              `json:"is_enabled"`
	UsageLimit       *int64                            `json:"usage_limit,omitempty"`
	UsageResetPeriod types.EntitlementUsageResetPeriod `json:"usage_reset_period,omitempty"`
	IsSoftLimit      bool                              `json:"is_soft_limit"`
	StaticValue      string                            `json:"static_value,omitempty"`
}

func NewEntitlement(resp *dto.EntitlementResponse) *Entitlement {
	if resp == nil || resp.Entitlement == nil {
		return nil
	}
	return &Entitlement{
		ID:               resp.ID,
		EntityType:       resp.EntityType,
		EntityID:         resp.EntityID,
		FeatureID:        resp.FeatureID,
		FeatureType:      resp.FeatureType,
		IsEnabled:        resp.IsEnabled,
		UsageLimit:       resp.UsageLimit,
		UsageResetPeriod: resp.UsageResetPeriod,
		IsSoftLimit:      resp.IsSoftLimit,
		StaticValue:      resp.StaticValue,
	}
}

type EntitlementWebhookPayload struct {
	EventType   types.WebhookEventName `json:"event_type"`
	Entitlement *Entitlement           `json:"entitlement"`
}

func NewEntitlementWebhookPayload(entitlement *dto.EntitlementResponse, eventType types.WebhookEventName) *EntitlementWebhookPayload {
	return &EntitlementWebhookPayload{EventType: eventType, Entitlement: NewEntitlement(entitlement)}
}

type EntitlementPayloadBuilder struct {
	services *Services
}

func NewEntitlementPayloadBuilder(services *Services) PayloadBuilder {
	return &EntitlementPayloadBuilder{services: services}
}

func (b *EntitlementPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
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

	payload := NewEntitlementWebhookPayload(entitlement, eventType)

	return json.Marshal(payload)
}
