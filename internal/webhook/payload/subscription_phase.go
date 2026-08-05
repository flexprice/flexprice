package payload

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type SubscriptionPhase struct {
	ID             string     `json:"id"`
	SubscriptionID string     `json:"subscription_id"`
	StartDate      time.Time  `json:"start_date"`
	EndDate        *time.Time `json:"end_date,omitempty"`
}

func NewSubscriptionPhase(resp *dto.SubscriptionPhaseResponse) *SubscriptionPhase {
	if resp == nil || resp.SubscriptionPhase == nil {
		return nil
	}
	return &SubscriptionPhase{
		ID:             resp.ID,
		SubscriptionID: resp.SubscriptionID,
		StartDate:      resp.StartDate,
		EndDate:        resp.EndDate,
	}
}

type SubscriptionPhaseWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Phase     *SubscriptionPhase     `json:"phase"`
}

func NewSubscriptionPhaseWebhookPayload(phase *SubscriptionPhase, eventType types.WebhookEventName) *SubscriptionPhaseWebhookPayload {
	return &SubscriptionPhaseWebhookPayload{EventType: eventType, Phase: phase}
}

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

	payload := NewSubscriptionPhaseWebhookPayload(NewSubscriptionPhase(phase), eventType)

	return json.Marshal(payload)
}
