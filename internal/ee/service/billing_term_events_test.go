package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/stretchr/testify/require"
)

func newEventParams() (ServiceParams, *testutil.InMemoryWebhookPublisher) {
	publisher := testutil.NewInMemoryWebhookPublisher()
	return ServiceParams{Logger: logger.NewNoopLogger(), WebhookPublisher: publisher}, publisher
}

func eventContext() context.Context {
	ctx := types.SetTenantID(context.Background(), "ten_events")
	return types.SetEnvironmentID(ctx, "env_events")
}

func TestPublishInstalmentEvents_AfterCommit(t *testing.T) {
	instalments := []*billingterm.Instalment{{ID: "binst_1"}, {ID: "binst_2"}}

	t.Run("held until the transaction commits", func(t *testing.T) {
		params, publisher := newEventParams()
		ctx := types.WithPostCommitHooks(eventContext())

		publishInstalmentEvents(ctx, params, types.WebhookEventBillingInstalmentCreated, instalments)
		require.Empty(t, publisher.Events(), "nothing is published while the transaction is open")

		types.RunPostCommitHooks(ctx)

		events := publisher.Events()
		require.Len(t, events, 2)
		for i, event := range events {
			require.Equal(t, types.WebhookEventBillingInstalmentCreated, event.EventName)
			require.Equal(t, types.SystemEntityTypeBillingInstalment, event.EntityType)
			require.Equal(t, instalments[i].ID, event.EntityID)
			require.Equal(t, "ten_events", event.TenantID)
			require.Equal(t, "env_events", event.EnvironmentID)

			var payload webhookDto.InternalBillingInstalmentEvent
			require.NoError(t, json.Unmarshal(event.Payload, &payload))
			require.Equal(t, instalments[i].ID, payload.InstalmentID)
			require.Equal(t, "ten_events", payload.TenantID)
		}
	})

	t.Run("a rolled-back transaction publishes nothing", func(t *testing.T) {
		params, publisher := newEventParams()
		ctx := types.WithPostCommitHooks(eventContext())

		publishInstalmentEvents(ctx, params, types.WebhookEventBillingInstalmentCancelled, instalments)
		types.DiscardPostCommitHooks(ctx)

		require.Empty(t, publisher.Events())
	})

	t.Run("outside a transaction publishes at once", func(t *testing.T) {
		params, publisher := newEventParams()

		publishInstalmentEvents(eventContext(), params, types.WebhookEventBillingInstalmentCancelled, instalments)

		require.Len(t, publisher.Events(), 2)
	})

	t.Run("no instalments publishes nothing", func(t *testing.T) {
		params, publisher := newEventParams()

		publishInstalmentEvents(eventContext(), params, types.WebhookEventBillingInstalmentCreated, nil)

		require.Empty(t, publisher.Events())
	})
}

func TestPublishBillingTermsEvent_AfterCommit(t *testing.T) {
	owner := &commitment.Commitment{ID: "cmt_1"}

	t.Run("held until the transaction commits", func(t *testing.T) {
		params, publisher := newEventParams()
		ctx := types.WithPostCommitHooks(eventContext())

		publishBillingTermsEvent(ctx, params, types.WebhookEventBillingTermsCreated, owner)
		require.Empty(t, publisher.Events())

		types.RunPostCommitHooks(ctx)

		events := publisher.Events()
		require.Len(t, events, 1)
		require.Equal(t, types.WebhookEventBillingTermsCreated, events[0].EventName)
		require.Equal(t, types.SystemEntityTypeCommitment, events[0].EntityType)
		require.Equal(t, "cmt_1", events[0].EntityID)

		var payload webhookDto.InternalBillingTermsEvent
		require.NoError(t, json.Unmarshal(events[0].Payload, &payload))
		require.Equal(t, types.BillingTermOwnerCommitment, payload.OwnerType)
		require.Equal(t, "cmt_1", payload.OwnerID)
		require.Equal(t, "ten_events", payload.TenantID)
	})

	t.Run("a rolled-back transaction publishes nothing", func(t *testing.T) {
		params, publisher := newEventParams()
		ctx := types.WithPostCommitHooks(eventContext())

		publishBillingTermsEvent(ctx, params, types.WebhookEventBillingTermsUpdated, owner)
		types.DiscardPostCommitHooks(ctx)

		require.Empty(t, publisher.Events())
	})
}
