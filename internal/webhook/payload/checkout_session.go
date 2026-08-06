package payload

import (
	"context"
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type CheckoutSessionPayloadBuilder struct {
	services *Services
}

func NewCheckoutSessionPayloadBuilder(services *Services) PayloadBuilder {
	return &CheckoutSessionPayloadBuilder{services: services}
}

func (b *CheckoutSessionPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var ev webhookDto.InternalCheckoutSessionEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal checkout session event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	if ev.SessionID == "" || ev.TenantID == "" {
		return nil, ierr.NewError("invalid data for checkout session event").
			WithHint("Please provide a valid session ID and tenant ID").
			Mark(ierr.ErrInvalidOperation)
	}

	session, err := b.services.CheckoutSessionService.Get(ctx, ev.SessionID)
	if err != nil {
		return nil, err
	}

	return json.Marshal(webhookDto.NewCheckoutSessionWebhookPayload(session, eventType))
}
