package webhookDto

import "github.com/flexprice/flexprice/internal/types"

// InternalSubscriptionLineItemEvent is the payload for the internal-only
// subscription.line_item.created / subscription.line_item.deleted events.
// These events are never delivered to customers (no PayloadBuilder is
// registered for them) — they exist solely to decouple line-item mutation
// points from integration-specific side effects (see
// internal/integration/events/dispatch.go).
type InternalSubscriptionLineItemEvent struct {
	SubscriptionID string          `json:"subscription_id"`
	LineItemID     string          `json:"line_item_id"`
	CustomerID     string          `json:"customer_id"`
	PriceType      types.PriceType `json:"price_type"`
	TenantID       string          `json:"tenant_id"`
	EnvironmentID  string          `json:"environment_id"`
}
