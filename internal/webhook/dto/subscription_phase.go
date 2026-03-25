package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalSubscriptionPhaseEvent struct {
	PhaseID  string `json:"phase_id"`
	TenantID string `json:"tenant_id"`
}

type SubscriptionPhaseWebhookPayload struct {
	EventType types.WebhookEventName         `json:"event_type"`
	Phase     *dto.SubscriptionPhaseResponse `json:"phase"`
}

func NewSubscriptionPhaseWebhookPayload(phase *dto.SubscriptionPhaseResponse, eventType types.WebhookEventName) *SubscriptionPhaseWebhookPayload {
	return &SubscriptionPhaseWebhookPayload{EventType: eventType, Phase: phase}
}
