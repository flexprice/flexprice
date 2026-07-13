package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// UnmatchedEventData is a snapshot of the event that matched no meter,
// so the builder needs no re-fetch.
type UnmatchedEventData struct {
	ID                 string                 `json:"id"`
	EventName          string                 `json:"event_name"`
	CustomerID         string                 `json:"customer_id,omitempty"`
	ExternalCustomerID string                 `json:"external_customer_id,omitempty"`
	Source             string                 `json:"source,omitempty"`
	Properties         map[string]interface{} `json:"properties,omitempty"`
	Timestamp          time.Time              `json:"timestamp"`
}

// InternalUnmatchedEvent is what the publisher puts in WebhookEvent.Payload.
type InternalUnmatchedEvent struct {
	Reason types.UnmatchedEventReason `json:"reason"`
	Event  UnmatchedEventData         `json:"event"`
}

// UnmatchedEventWebhookPayload is the outbound payload delivered to subscribers.
type UnmatchedEventWebhookPayload struct {
	EventType types.WebhookEventName     `json:"event_type"`
	Reason    types.UnmatchedEventReason `json:"reason"`
	Event     UnmatchedEventData         `json:"event"`
}

func NewUnmatchedEventWebhookPayload(internal *InternalUnmatchedEvent, eventType types.WebhookEventName) *UnmatchedEventWebhookPayload {
	return &UnmatchedEventWebhookPayload{
		EventType: eventType,
		Reason:    internal.Reason,
		Event:     internal.Event,
	}
}
