package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalCustomerEvent struct {
	CustomerID string `json:"customer_id"`
	TenantID   string `json:"tenant_id"`
}

type CustomerWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Customer  *dto.CustomerResponse  `json:"customer"`
}

func NewCustomerWebhookPayload(customer *dto.CustomerResponse, eventType types.WebhookEventName) *CustomerWebhookPayload {
	return &CustomerWebhookPayload{EventType: eventType, Customer: customer}
}
