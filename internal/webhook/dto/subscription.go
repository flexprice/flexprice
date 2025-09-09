package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
)

type InternalSubscriptionEvent struct {
	EventType      string `json:"event_type"`
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
}

// SubscriptionWebhookPayload represents the detailed payload for subscription payment webhooks
type SubscriptionWebhookPayload struct {
	EventType    string                    `json:"event_type"`
	Subscription *dto.SubscriptionResponse `json:"subscription"`
}

func NewSubscriptionWebhookPayload(subscription *dto.SubscriptionResponse, eventType string) *SubscriptionWebhookPayload {
	return &SubscriptionWebhookPayload{
		EventType:    eventType,
		Subscription: subscription,
	}
}
