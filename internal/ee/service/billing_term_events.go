package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// publishBillingTermsEvent sends a billing terms webhook for the owner once the surrounding transaction commits.
// A failure is logged and never fails the write.
func publishBillingTermsEvent(ctx context.Context, params ServiceParams, name types.WebhookEventName, owner billingterm.Owner) {
	event, err := types.NewWebhookEvent(name).
		WithIdentityFromContext(ctx).
		WithEntity(types.SystemEntityTypeCommitment, owner.OwnerID()).
		WithPayload(webhookDto.InternalBillingTermsEvent{
			OwnerType: owner.OwnerType(),
			OwnerID:   owner.OwnerID(),
			TenantID:  types.GetTenantID(ctx),
		}).
		Build()
	if err != nil {
		params.Logger.Error(ctx, "failed to build billing terms webhook",
			"error", err,
			"event_name", name,
			"owner_id", owner.OwnerID(),
		)
		return
	}

	publishAfterCommit(ctx, params, event)
}

// publishInstalmentEvents sends one webhook per instalment once the surrounding transaction commits.
// A failure is logged and never fails the write.
func publishInstalmentEvents(ctx context.Context, params ServiceParams, name types.WebhookEventName, instalments []*billingterm.Instalment) {
	events := make([]*types.WebhookEvent, 0, len(instalments))
	for _, instalment := range instalments {
		event, err := types.NewWebhookEvent(name).
			WithIdentityFromContext(ctx).
			WithEntity(types.SystemEntityTypeBillingInstalment, instalment.ID).
			WithPayload(webhookDto.InternalBillingInstalmentEvent{
				InstalmentID: instalment.ID,
				TenantID:     types.GetTenantID(ctx),
			}).
			Build()
		if err != nil {
			params.Logger.Error(ctx, "failed to build billing instalment webhook",
				"error", err,
				"event_name", name,
				"instalment_id", instalment.ID,
			)
			continue
		}

		events = append(events, event)
	}

	publishAfterCommit(ctx, params, events...)
}

// publishAfterCommit holds events until the transaction commits, so the payload builder can read the rows they
// describe and a rollback sends nothing. Outside a transaction they go out at once.
func publishAfterCommit(ctx context.Context, params ServiceParams, events ...*types.WebhookEvent) {
	if len(events) == 0 {
		return
	}

	// The publisher records the event in Postgres, which must not run on the finished transaction.
	publishCtx := types.WithoutDBTransaction(ctx)
	publish := func() {
		for _, event := range events {
			if err := params.WebhookPublisher.PublishWebhook(publishCtx, event); err != nil {
				params.Logger.Error(publishCtx, "failed to publish billing term webhook",
					"error", err,
					"event_name", event.EventName,
					"entity_id", event.EntityID,
				)
			}
		}
	}

	if !types.RegisterPostCommit(ctx, publish) {
		publish()
	}
}
