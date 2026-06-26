package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// CheckoutSessionWebhookPayload is the payload published for checkout session lifecycle events.
type CheckoutSessionWebhookPayload struct {
	EventType       types.WebhookEventName         `json:"event_type"`
	CheckoutSession *dto.CheckoutSessionResponse   `json:"checkout_session"`
}

func NewCheckoutSessionWebhookPayload(session *dto.CheckoutSessionResponse, eventType types.WebhookEventName) *CheckoutSessionWebhookPayload {
	return &CheckoutSessionWebhookPayload{
		EventType:       eventType,
		CheckoutSession: session,
	}
}
