package outbound

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// EntitlementWebhookPayload is the minimal webhook representation of an entitlement.
type EntitlementWebhookPayload struct {
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

func NewEntitlementWebhookPayload(resp *dto.EntitlementResponse) *EntitlementWebhookPayload {
	if resp == nil || resp.Entitlement == nil {
		return nil
	}
	return &EntitlementWebhookPayload{
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
