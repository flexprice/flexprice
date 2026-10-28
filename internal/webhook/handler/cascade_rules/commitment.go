package cascaderules

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// CommitmentCascadeRule follows a new commitment with subscription.updated for its subscription.
type CommitmentCascadeRule struct {
	logger *logger.Logger
}

func NewCommitmentCascadeRule(logger *logger.Logger) CascadeRule {
	return &CommitmentCascadeRule{logger: logger}
}

func (r *CommitmentCascadeRule) SourceEvents() []types.WebhookEventName {
	return []types.WebhookEventName{types.WebhookEventCommitmentCreated}
}

func (r *CommitmentCascadeRule) TargetEvents() []types.WebhookEventName {
	return []types.WebhookEventName{types.WebhookEventSubscriptionUpdated}
}

func (r *CommitmentCascadeRule) GetEventsToCascade(ctx context.Context, event *types.WebhookEvent) []*types.WebhookEvent {
	var parsed webhookDto.InternalCommitmentEvent
	if err := json.Unmarshal(event.Payload, &parsed); err != nil {
		r.logger.Error(ctx, "webhook cascade: failed to read commitment event",
			"error", err,
			"commitment_id", event.EntityID,
			"event_name", event.EventName,
		)
		return nil
	}

	if parsed.SubscriptionID == "" {
		return nil
	}

	newEvent, err := types.NewWebhookEvent(types.WebhookEventSubscriptionUpdated).
		WithIdentityFrom(event).
		WithEntity(types.SystemEntityTypeSubscription, parsed.SubscriptionID).
		WithPayload(webhookDto.InternalSubscriptionEvent{
			SubscriptionID: parsed.SubscriptionID,
			TenantID:       event.TenantID,
		}).
		Build()
	if err != nil {
		r.logger.Error(ctx, "webhook cascade: failed to build subscription.updated event",
			"error", err,
			"subscription_id", parsed.SubscriptionID,
		)
		return nil
	}

	return []*types.WebhookEvent{newEvent}
}
