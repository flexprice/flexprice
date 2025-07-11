package webhookDto

import "github.com/flexprice/flexprice/internal/api/dto"

type InternalPaymentEvent struct {
	PaymentID string `json:"payment_id"`
	TenantID  string `json:"tenant_id"`
}

type PaymentWebhookPayload struct {
	Payment *dto.PaymentResponse `json:"payment"`
}

func NewPaymentWebhookPayload(payment *dto.PaymentResponse) *PaymentWebhookPayload {
	return &PaymentWebhookPayload{Payment: payment}
}
