package payload

import (
	"context"
	"encoding/json"
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type FXRatePayloadBuilder struct {
	services *Services
}

func NewFXRatePayloadBuilder(services *Services) PayloadBuilder {
	return &FXRatePayloadBuilder{services: services}
}

func (b *FXRatePayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var parsed webhookDto.InternalFXRateEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal fx rate event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	if parsed.FXRateID == "" || parsed.TenantID == "" {
		return nil, ierr.NewError("invalid data for fx rate event").
			WithHint("Please provide a valid fx rate ID and tenant ID").
			WithReportableDetails(map[string]any{
				"expected": "string",
				"got":      fmt.Sprintf("%T", data),
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	rate, err := b.services.FXRateService.GetFXRate(ctx, parsed.FXRateID)
	if err != nil {
		return nil, err
	}

	return json.Marshal(webhookDto.NewFXRateWebhookPayload(rate, eventType))
}
