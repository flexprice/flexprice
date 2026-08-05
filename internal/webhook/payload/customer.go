package payload

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type Customer struct {
	ID                string            `json:"id"`
	ExternalID        string            `json:"external_id"`
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

type CustomerPayloadBuilder struct {
	services *Services
}

func NewCustomerPayloadBuilder(services *Services) PayloadBuilder {
	return &CustomerPayloadBuilder{services: services}
}

func (b *CustomerPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var parsedPayload webhookDto.InternalCustomerEvent

	err := json.Unmarshal(data, &parsedPayload)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal customer event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	customerID, tenantID := parsedPayload.CustomerID, parsedPayload.TenantID
	if customerID == "" || tenantID == "" {
		return nil, ierr.NewError("invalid data type for customer event").
			WithHint("Please provide a valid customer ID and tenant ID").
			WithReportableDetails(map[string]any{
				"expected": "string",
				"got":      fmt.Sprintf("%T", data),
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	customer, err := b.services.CustomerService.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}

	payload := NewCustomerWebhookPayload(customer, eventType)

	return json.Marshal(payload)
}
