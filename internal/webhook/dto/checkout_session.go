package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// InternalCheckoutSessionEvent is the internal payload stored in system_events.
// The builder re-fetches the session by ID to build the full outbound payload.
type InternalCheckoutSessionEvent struct {
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id"`
}

// CheckoutSessionWebhookPayload is the outbound payload delivered to subscribers.
type CheckoutSessionWebhookPayload struct {
	EventType       types.WebhookEventName       `json:"event_type"`
	CheckoutSession *dto.CheckoutSessionResponse `json:"checkout_session"`
}

func NewCheckoutSessionWebhookPayload(session *dto.CheckoutSessionResponse, eventType types.WebhookEventName) *CheckoutSessionWebhookPayload {
	return &CheckoutSessionWebhookPayload{
		EventType:       eventType,
		CheckoutSession: session,
	}
}
