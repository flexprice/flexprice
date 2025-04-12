package webhookDto

import "github.com/flexprice/flexprice/internal/api/dto"

type InternalCustomerEvent struct {
	CustomerID string `json:"customer_id"`
	TenantID   string `json:"tenant_id"`
}

type CustomerWebhookPayload struct {
	Customer *dto.CustomerResponse
}

func NewCustomerWebhookPayload(customer *dto.CustomerResponse) *CustomerWebhookPayload {
	return &CustomerWebhookPayload{Customer: customer}
}
