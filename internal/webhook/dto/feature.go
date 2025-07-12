package webhookDto

import "github.com/flexprice/flexprice/internal/api/dto"

type InternalFeatureEvent struct {
	FeatureID string `json:"feature_id"`
	TenantID  string `json:"tenant_id"`
}

type FeatureWebhookPayload struct {
	Feature *dto.FeatureResponse `json:"feature"`
}

func NewFeatureWebhookPayload(feature *dto.FeatureResponse) *FeatureWebhookPayload {
	return &FeatureWebhookPayload{Feature: feature}
}
