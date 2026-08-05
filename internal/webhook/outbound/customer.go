package outbound

import "github.com/flexprice/flexprice/internal/api/dto"

// CustomerWebhookPayload is the minimal webhook representation of a customer.
// Top-level only — every other payload that references a customer carries
// customer_id instead of embedding this type.
type CustomerWebhookPayload struct {
	ID                string `json:"id"`
	ExternalID        string `json:"external_id"`
	Name              string `json:"name"`
	Email             string `json:"email,omitempty"`
	AddressLine1      string `json:"address_line1,omitempty"`
	AddressLine2      string `json:"address_line2,omitempty"`
	AddressCity       string `json:"address_city,omitempty"`
	AddressState      string `json:"address_state,omitempty"`
	AddressPostalCode string `json:"address_postal_code,omitempty"`
	AddressCountry    string `json:"address_country,omitempty"`
	Timezone          string `json:"timezone,omitempty"`
}

func NewCustomerWebhookPayload(resp *dto.CustomerResponse) *CustomerWebhookPayload {
	if resp == nil || resp.Customer == nil {
		return nil
	}
	return &CustomerWebhookPayload{
		ID:                resp.ID,
		ExternalID:        resp.ExternalID,
		Name:              resp.Name,
		Email:             resp.Email,
		AddressLine1:      resp.AddressLine1,
		AddressLine2:      resp.AddressLine2,
		AddressCity:       resp.AddressCity,
		AddressState:      resp.AddressState,
		AddressPostalCode: resp.AddressPostalCode,
		AddressCountry:    resp.AddressCountry,
		Timezone:          resp.Timezone,
	}
}
