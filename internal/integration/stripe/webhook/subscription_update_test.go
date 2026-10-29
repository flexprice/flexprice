package webhook_test

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/integration/stripe"
	"github.com/flexprice/flexprice/internal/integration/stripe/webhook"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	stripeapi "github.com/stripe/stripe-go/v82"
)

type subscriptionUpdateHandler struct {
	stripe.StripeSubscriptionService
	err   error
	calls int
}

func (s *subscriptionUpdateHandler) UpdateSubscription(_ context.Context, id string, _ *stripe.ServiceDependencies) error {
	s.calls++
	if id != "sub_stripe" {
		return errors.New("unexpected subscription")
	}
	return s.err
}

func TestSubscriptionUpdateWebhookPropagatesFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "applied", true: "retryable failure"}[fail], func(t *testing.T) {
			subs := &subscriptionUpdateHandler{}
			if fail {
				subs.err = errors.New("eligibility guard is busy")
			}
			connections := &fakeStripeConnectionRepo{conn: &connection.Connection{
				SyncConfig: &types.SyncConfig{Subscription: &types.EntitySyncConfig{Inbound: true}},
			}}
			handler := webhook.NewHandler(nil, nil, nil, nil, nil, subs, nil, connections, logger.NewNoopLogger())
			event := &stripeapi.Event{ID: "evt_subscription", Type: "customer.subscription.updated", Data: &stripeapi.EventData{Raw: []byte(`{"id":"sub_stripe"}`)}}
			err := handler.HandleWebhookEvent(testutil.SetupContext(), event, "env_test", &webhook.ServiceDependencies{})
			if fail {
				require.ErrorIs(t, err, subs.err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 1, subs.calls)
		})
	}
}
