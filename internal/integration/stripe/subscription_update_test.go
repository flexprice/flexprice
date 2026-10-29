package stripe

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	stripeapi "github.com/stripe/stripe-go/v82"
)

type subscriptionUpdateTransaction struct{}

type subscriptionUpdateDB struct {
	postgres.IClient
	calls int
}

func (d *subscriptionUpdateDB) WithTx(ctx context.Context, apply func(context.Context) error) error {
	d.calls++
	return apply(context.WithValue(ctx, subscriptionUpdateTransaction{}, true))
}

type subscriptionUpdateMapping struct {
	interfaces.EntityIntegrationMappingService
}

func (s *subscriptionUpdateMapping) GetEntityIntegrationMappings(_ context.Context, filter *types.EntityIntegrationMappingFilter) (*dto.ListEntityIntegrationMappingsResponse, error) {
	mapping := &dto.EntityIntegrationMappingResponse{EntityID: "sub_current", ProviderEntityID: "sub_stripe"}
	if filter.EntityType == types.IntegrationEntityTypePlan {
		mapping.ProviderEntityID = "product_current"
	}
	return &dto.ListEntityIntegrationMappingsResponse{Items: []*dto.EntityIntegrationMappingResponse{mapping}}, nil
}

type subscriptionUpdateService struct {
	interfaces.SubscriptionService
	t             *testing.T
	updateError   error
	cancelError   error
	updates       []dto.UpdateSubscriptionRequest
	cancellations int
}

func (s *subscriptionUpdateService) GetSubscription(context.Context, string) (*dto.SubscriptionResponse, error) {
	return &dto.SubscriptionResponse{Subscription: &subscription.Subscription{ID: "sub_current", PlanID: "plan_current"}}, nil
}

func (s *subscriptionUpdateService) UpdateSubscription(ctx context.Context, id string, req dto.UpdateSubscriptionRequest) (*dto.SubscriptionResponse, error) {
	require.Nil(s.t, ctx.Value(subscriptionUpdateTransaction{}), "normal update must own its lifecycle transaction")
	require.Equal(s.t, "sub_current", id)
	s.updates = append(s.updates, req)
	return &dto.SubscriptionResponse{}, s.updateError
}

func (s *subscriptionUpdateService) CancelSubscription(ctx context.Context, _ string, _ *dto.CancelSubscriptionRequest) (*dto.CancelSubscriptionResponse, error) {
	require.Equal(s.t, true, ctx.Value(subscriptionUpdateTransaction{}), "plan replacement must retain its atomic transaction")
	s.cancellations++
	return nil, s.cancelError
}

func TestStripeSubscriptionUpdateTransactionBoundary(t *testing.T) {
	for _, scenario := range []string{"normal update", "normal update failure", "plan replacement"} {
		t.Run(scenario, func(t *testing.T) {
			db := &subscriptionUpdateDB{}
			subs := &subscriptionUpdateService{t: t}
			source := &stripeapi.Subscription{ID: "sub_stripe", Status: stripeapi.SubscriptionStatusCanceled,
				Items:             &stripeapi.SubscriptionItemList{Data: []*stripeapi.SubscriptionItem{{Plan: &stripeapi.Plan{Product: &stripeapi.Product{ID: "product_current"}}}}},
				CancelAtPeriodEnd: true, CancelAt: 1800000000}
			failure := errors.New("retryable subscription failure")
			if scenario == "normal update failure" {
				subs.updateError = failure
			}
			if scenario == "plan replacement" {
				source.Items.Data[0].Plan.Product.ID = "product_new"
				subs.cancelError = failure
			}
			svc := &stripeSubscriptionService{logger: logger.NewNoopLogger()}
			err := svc.applySubscriptionUpdate(context.Background(), source, &ServiceDependencies{
				DB: db, SubscriptionService: subs, EntityIntegrationMappingService: &subscriptionUpdateMapping{},
			})
			if scenario == "normal update" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, failure)
			}
			if scenario == "plan replacement" {
				require.Equal(t, 1, db.calls)
				require.Equal(t, 1, subs.cancellations)
				require.Empty(t, subs.updates)
				return
			}
			require.Zero(t, db.calls)
			require.Len(t, subs.updates, 1)
			require.Equal(t, types.SubscriptionStatusCancelled, subs.updates[0].Status)
			require.True(t, *subs.updates[0].CancelAtPeriodEnd)
			require.Equal(t, source.CancelAt, subs.updates[0].CancelAt.Unix())
		})
	}
}
