package payload

import (
	"context"
	"encoding/json"
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type CustomerPayloadBuilder struct {
	services *Services
}

func NewCustomerPayloadBuilder(services *Services) PayloadBuilder {
	return &CustomerPayloadBuilder{services: services}
}

func (b *CustomerPayloadBuilder) BuildPayload(ctx context.Context, eventType string, data json.RawMessage) (json.RawMessage, error) {
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

	payload := webhookDto.NewCustomerWebhookPayload(customer)

	return json.Marshal(payload)
}
 