package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
)

type InternalSubscriptionEvent struct {
	EventType      string `json:"event_type"`
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
}

// SubscriptionWebhookPayload represents the detailed payload for subscription payment webhooks
type SubscriptionWebhookPayload struct {
	Subscription *dto.SubscriptionResponse
}

func NewSubscriptionWebhookPayload(subscription *dto.SubscriptionResponse) *SubscriptionWebhookPayload {
	return &SubscriptionWebhookPayload{
		Subscription: subscription,
	}
}
