package payload

import (
	"context"
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type SubscriptionPhasePayloadBuilder struct {
	services *Services
}

func NewSubscriptionPhasePayloadBuilder(services *Services) PayloadBuilder {
	return &SubscriptionPhasePayloadBuilder{services: services}
}

func (b *SubscriptionPhasePayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var parsedPayload webhookDto.InternalSubscriptionPhaseEvent

	if err := json.Unmarshal(data, &parsedPayload); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal subscription phase event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	if parsedPayload.PhaseID == "" {
		return nil, ierr.NewError("invalid data for subscription phase event").
			WithHint("Please provide a valid phase ID").
			Mark(ierr.ErrInvalidOperation)
	}

	phase, err := b.services.SubscriptionPhaseService.GetSubscriptionPhase(ctx, parsedPayload.PhaseID)
	if err != nil {
		return nil, err
	}

	payload := webhookDto.NewSubscriptionPhaseWebhookPayload(webhookDto.NewSubscriptionPhase(phase), eventType)

	return json.Marshal(payload)
}
