package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalEntitlementEvent struct {
	EntitlementID string `json:"entitlement_id"`
	TenantID      string `json:"tenant_id"`
}

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
