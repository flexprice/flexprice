package payload

import (
	"context"
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type SubscriptionPayloadBuilder struct {
	services *Services
}

func NewSubscriptionPayloadBuilder(services *Services) PayloadBuilder {
	return SubscriptionPayloadBuilder{
		services: services,
	}
}

func (b SubscriptionPayloadBuilder) BuildPayload(ctx context.Context, eventType string, data json.RawMessage) (json.RawMessage, error) {
	// Validate input data
	var parsedPayload webhookDto.InternalSubscriptionEvent

	err := json.Unmarshal(data, &parsedPayload)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal subscription event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	// Create payload
	subscriptionData, err := b.services.SubscriptionService.GetSubscription(ctx, parsedPayload.SubscriptionID)
	if err != nil {
		return nil, err
	}

	payload := webhookDto.NewSubscriptionWebhookPayload(subscriptionData)

	// Marshal payload
	return json.Marshal(payload)

}
