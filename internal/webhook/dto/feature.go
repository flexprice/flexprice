package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalFeatureEvent struct {
	FeatureID string `json:"feature_id"`
	TenantID  string `json:"tenant_id"`
}

type FeatureWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Feature   *dto.FeatureResponse   `json:"feature"`
}

func NewFeatureWebhookPayload(feature *dto.FeatureResponse, eventType types.WebhookEventName) *FeatureWebhookPayload {
	return &FeatureWebhookPayload{EventType: eventType, Feature: feature}
}
