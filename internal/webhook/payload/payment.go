package payload

import (
	"context"
	"encoding/json"
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type PaymentPayloadBuilder struct {
	services *Services
}

func NewPaymentPayloadBuilder(services *Services) PayloadBuilder {
	return &PaymentPayloadBuilder{services: services}
}

func (b *PaymentPayloadBuilder) BuildPayload(ctx context.Context, eventType string, data json.RawMessage) (json.RawMessage, error) {
	var parsedPayload webhookDto.InternalPaymentEvent

	err := json.Unmarshal(data, &parsedPayload)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal payment event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	paymentID, tenantID := parsedPayload.PaymentID, parsedPayload.TenantID
	if paymentID == "" || tenantID == "" {
		return nil, ierr.NewError("invalid data type for payment event").
			WithHint("Please provide a valid payment ID and tenant ID").
			WithReportableDetails(map[string]any{
				"expected": "string",
				"got":      fmt.Sprintf("%T", data),
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	payment, err := b.services.PaymentService.GetPayment(ctx, paymentID)
	if err != nil {
		return nil, err
	}

	payload := webhookDto.NewPaymentWebhookPayload(payment)

	return json.Marshal(payload)
}
