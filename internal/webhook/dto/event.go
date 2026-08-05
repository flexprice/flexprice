package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// RejectedEventData is a snapshot of the event that matched no meter,
// so the builder needs no re-fetch.
type RejectedEventData struct {
	ID                 string                 `json:"id"`
	EventName          string                 `json:"event_name"`
	CustomerID         string                 `json:"customer_id,omitempty"`
	ExternalCustomerID string                 `json:"external_customer_id,omitempty"`
	Source             string                 `json:"source,omitempty"`
	Properties         map[string]interface{} `json:"properties,omitempty"`
	Timestamp          time.Time              `json:"timestamp"`
}

// InternalRejectedEvent is what the publisher puts in WebhookEvent.Payload.
type InternalRejectedEvent struct {
	Reason types.RejectedEventReason `json:"reason"`
	Event  RejectedEventData         `json:"event"`
}
