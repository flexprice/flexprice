package payload

import (
	"context"
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type RefundPayloadBuilder struct {
	services *Services
}

func NewRefundPayloadBuilder(services *Services) PayloadBuilder {
	return &RefundPayloadBuilder{services: services}
}

func (b *RefundPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var parsedPayload webhookDto.InternalRefundEvent

	if err := json.Unmarshal(data, &parsedPayload); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal refund event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	if parsedPayload.RefundID == "" || parsedPayload.TenantID == "" {
		return nil, ierr.NewError("invalid data for refund event").
			WithHint("Please provide a valid refund ID and tenant ID").
			Mark(ierr.ErrInvalidOperation)
	}

	refund, err := b.services.RefundService.GetRefund(ctx, parsedPayload.RefundID)
	if err != nil {
		return nil, err
	}

	return json.Marshal(webhookDto.NewRefundWebhookPayload(refund, eventType))
}
