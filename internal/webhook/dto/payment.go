package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalPaymentEvent struct {
	PaymentID string `json:"payment_id"`
	TenantID  string `json:"tenant_id"`
}

type PaymentWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Payment   *dto.PaymentResponse   `json:"payment"`
}

func NewPaymentWebhookPayload(payment *dto.PaymentResponse, eventType types.WebhookEventName) *PaymentWebhookPayload {
	return &PaymentWebhookPayload{EventType: eventType, Payment: payment}
}
