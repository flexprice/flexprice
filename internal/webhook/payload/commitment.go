package payload

import (
	"context"
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type CommitmentPayloadBuilder struct {
	services *Services
}

func NewCommitmentPayloadBuilder(services *Services) PayloadBuilder {
	return &CommitmentPayloadBuilder{services: services}
}

func (b *CommitmentPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var parsed webhookDto.InternalCommitmentEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal commitment event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	if parsed.CommitmentID == "" || parsed.TenantID == "" {
		return nil, ierr.NewError("invalid data type for commitment event").
			WithHint("Please provide a valid commitment ID and tenant ID").
			WithReportableDetails(map[string]any{
				"commitment_id": parsed.CommitmentID,
				"tenant_id":     parsed.TenantID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	commitment, err := b.services.CommitmentService.Get(ctx, parsed.CommitmentID)
	if err != nil {
		return nil, err
	}

	return json.Marshal(webhookDto.NewCommitmentWebhookPayload(commitment, eventType))
}
