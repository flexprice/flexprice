package webhookDto

import (
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/webhook/outbound"
)

type InternalSubscriptionPhaseEvent struct {
	PhaseID  string `json:"phase_id"`
	TenantID string `json:"tenant_id"`
}

type SubscriptionPhaseWebhookPayload struct {
	EventType types.WebhookEventName                    `json:"event_type"`
	Phase     *outbound.SubscriptionPhaseWebhookPayload `json:"phase"`
}

func NewSubscriptionPhaseWebhookPayload(phase *outbound.SubscriptionPhaseWebhookPayload, eventType types.WebhookEventName) *SubscriptionPhaseWebhookPayload {
	return &SubscriptionPhaseWebhookPayload{EventType: eventType, Phase: phase}
}
