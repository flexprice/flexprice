package cascaderules

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// entitlementCascadeRule cascades subscription.updated from subscription-scoped
// entitlement.* events. The entitlement is resolved fresh (works for soft-deleted rows too),
// so no data needs to be carried on the source event.
type EntitlementCascadeRule struct {
	entitlementService service.EntitlementService
	logger             *logger.Logger
}

// NewEntitlementCascadeRule builds the entitlement→subscription.updated cascade rule.
func NewEntitlementCascadeRule(entitlementService service.EntitlementService, logger *logger.Logger) CascadeRule {
	return &EntitlementCascadeRule{
		entitlementService: entitlementService,
		logger:             logger,
	}
}

func (r *EntitlementCascadeRule) SourceEvents() []types.WebhookEventName {
	return []types.WebhookEventName{
		types.WebhookEventEntitlementCreated,
		types.WebhookEventEntitlementUpdated,
		types.WebhookEventEntitlementDeleted,
	}
}

func (r *EntitlementCascadeRule) TargetEvents() []types.WebhookEventName {
	return []types.WebhookEventName{types.WebhookEventSubscriptionUpdated}
}

func (r *EntitlementCascadeRule) Cascade(ctx context.Context, event *types.WebhookEvent) []*types.WebhookEvent {
	if event.EntityID == "" {
		return nil
	}

	ent, err := r.entitlementService.GetEntitlement(ctx, event.EntityID)
	if err != nil {
		r.logger.Error(ctx, "webhook cascade: failed to resolve entitlement",
			"error", err,
			"entitlement_id", event.EntityID,
			"event_name", event.EventName,
		)
		return nil
	}
	if ent == nil {
		return nil
	}

	var cascadedEvents []*types.WebhookEvent
	switch ent.EntityType {
	case types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION:
		cascaded, err := types.NewWebhookEvent(types.WebhookEventSubscriptionUpdated).
			WithIdentityFrom(event).
			WithEntity(types.SystemEntityTypeSubscription, ent.EntityID).
			WithPayload(webhookDto.InternalSubscriptionEvent{
				SubscriptionID: ent.EntityID,
				TenantID:       event.TenantID,
			}).
			Build()
		if err != nil {
			r.logger.Error(ctx, "webhook cascade: failed to build subscription.updated event",
				"error", err,
				"subscription_id", ent.EntityID,
			)
			return nil
		}

		cascadedEvents = append(cascadedEvents, cascaded)
	}

	return cascadedEvents
}
