package payload

import (
	"context"
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// UnmatchedEventPayloadBuilder builds the event.unmatched payload. Pass-through:
// the event snapshot is already in the message, so no re-fetch.
type UnmatchedEventPayloadBuilder struct {
	services *Services
}

func NewUnmatchedEventPayloadBuilder(services *Services) PayloadBuilder {
	return &UnmatchedEventPayloadBuilder{services: services}
}

func (b *UnmatchedEventPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var internal webhookDto.InternalUnmatchedEvent
	if err := json.Unmarshal(data, &internal); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal unmatched event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	if internal.Event.ID == "" || internal.Event.EventName == "" {
		return nil, ierr.NewError("invalid data for unmatched event").
			WithHint("Please provide a valid event ID and event name").
			WithReportableDetails(map[string]any{
				"event_id":   internal.Event.ID,
				"event_name": internal.Event.EventName,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	return json.Marshal(webhookDto.NewUnmatchedEventWebhookPayload(&internal, eventType))
}
