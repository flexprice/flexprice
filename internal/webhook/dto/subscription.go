package webhookDto

import "github.com/flexprice/flexprice/internal/types"

type InternalSubscriptionEvent struct {
	EventType        types.WebhookEventName `json:"event_type"`
	SubscriptionID   string                 `json:"subscription_id"`
	CustomerID       string                 `json:"customer_id,omitempty"`
	PaymentBehavior  string                 `json:"payment_behavior,omitempty"`
	CollectionMethod string                 `json:"collection_method,omitempty"`
	TenantID         string                 `json:"tenant_id"`
	EnvironmentID    string                 `json:"environment_id"`
}
