package webhookDto

import "github.com/flexprice/flexprice/internal/api/dto"

type InternalSubscriptionPhaseEvent struct {
	PhaseID  string `json:"phase_id"`
	TenantID string `json:"tenant_id"`
}

type SubscriptionPhaseWebhookPayload struct {
	EventType string                         `json:"event_type"`
	Phase     *dto.SubscriptionPhaseResponse `json:"phase"`
}

func NewSubscriptionPhaseWebhookPayload(phase *dto.SubscriptionPhaseResponse, eventType string) *SubscriptionPhaseWebhookPayload {
	return &SubscriptionPhaseWebhookPayload{EventType: eventType, Phase: phase}
}
