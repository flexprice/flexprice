package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalCustomerEvent struct {
	CustomerID string `json:"customer_id"`
	TenantID   string `json:"tenant_id"`
}

type Customer struct {
	ID                string            `json:"id"`
	ExternalID        string            `json:"external_id"`
	EnvironmentID     string            `json:"environment_id"`
	Name              string            `json:"name"`
	Email             string            `json:"email,omitempty"`
	AddressLine1      string            `json:"address_line1,omitempty"`
	AddressLine2      string            `json:"address_line2,omitempty"`
	AddressCity       string            `json:"address_city,omitempty"`
	AddressState      string            `json:"address_state,omitempty"`
	AddressPostalCode string            `json:"address_postal_code,omitempty"`
	AddressCountry    string            `json:"address_country,omitempty"`
	Timezone          string            `json:"timezone,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

func NewCustomer(resp *dto.CustomerResponse) *Customer {
	if resp == nil || resp.Customer == nil {
		return nil
	}
	return &Customer{
		ID:                resp.ID,
		ExternalID:        resp.ExternalID,
		EnvironmentID:     resp.EnvironmentID,
		Name:              resp.Name,
		Email:             resp.Email,
		AddressLine1:      resp.AddressLine1,
		AddressLine2:      resp.AddressLine2,
		AddressCity:       resp.AddressCity,
		AddressState:      resp.AddressState,
		AddressPostalCode: resp.AddressPostalCode,
		AddressCountry:    resp.AddressCountry,
		Timezone:          resp.Timezone,
		Metadata:          resp.Metadata,
	}
}

type CustomerWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Customer  *Customer              `json:"customer"`
}

func NewCustomerWebhookPayload(customer *dto.CustomerResponse, eventType types.WebhookEventName) *CustomerWebhookPayload {
	return &CustomerWebhookPayload{EventType: eventType, Customer: NewCustomer(customer)}
}
