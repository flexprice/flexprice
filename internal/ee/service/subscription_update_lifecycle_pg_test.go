package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	repository "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type subscriptionUpdateObservedStore struct {
	domain.ExecutionStore
	waiting chan struct{}
}

func (s *subscriptionUpdateObservedStore) LockEligibility(ctx context.Context, scopes []domain.EligibilityScope) error {
	close(s.waiting)
	return s.ExecutionStore.LockEligibility(ctx, scopes)
}

type subscriptionUpdateFailingRepository struct {
	subscription.Repository
}

func (r *subscriptionUpdateFailingRepository) Update(ctx context.Context, sub *subscription.Subscription) error {
	if err := r.Repository.Update(ctx, sub); err != nil {
		return err
	}
	return errors.New("failure after database update")
}

func TestSubscriptionUpdate_WaitsForEligibilityPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	s, sub := newSubscriptionUpdateFixture(t)
	ctx, cancel := context.WithTimeout(s.GetContext(), 15*time.Second)
	defer cancel()
	params := s.params
	params.DB = db
	params.SubRepo = repository.NewSubscriptionRepository(db, params.Logger, testutil.NewInMemoryRedis())
	store := repository.NewBillingTermsExecutionStore(db)
	params.BillingTermsStore = &subscriptionUpdateObservedStore{ExecutionStore: store, waiting: make(chan struct{})}
	require.NoError(t, params.SubRepo.Create(ctx, sub))
	svc := NewSubscriptionService(params).(*subscriptionService)
	done := make(chan error, 1)

	err := db.WithTx(ctx, func(tx context.Context) error {
		if err := store.LockEligibility(tx, []domain.EligibilityScope{{Type: "subscription", ID: sub.ID}}); err != nil {
			return err
		}
		go func() {
			done <- svc.updateSubscription(ctx, sub.ID, dto.UpdateSubscriptionRequest{Status: types.SubscriptionStatusIncomplete})
		}()
		select {
		case <-params.BillingTermsStore.(*subscriptionUpdateObservedStore).waiting:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case err := <-done:
			t.Errorf("update completed while eligibility was held: %v", err)
			return errors.New("eligibility guard did not serialize the update")
		case <-time.After(100 * time.Millisecond):
		}
		rows, err := params.SubRepo.List(tx, &types.SubscriptionFilter{
			QueryFilter: types.NewDefaultQueryFilter(), SubscriptionIDs: []string{sub.ID},
			SubscriptionStatus: types.AllSubscriptionStatuses(),
		})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, types.SubscriptionStatusActive, rows[0].SubscriptionStatus)
		return nil
	})
	require.NoError(t, err)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stored, err := params.SubRepo.Get(types.WithForceWriter(ctx), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusIncomplete, stored.SubscriptionStatus)
}

func TestSubscriptionUpdate_RollsBackWithoutNotificationPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	s, sub := newSubscriptionUpdateFixture(t)
	ctx := s.GetContext()
	params := s.params
	params.DB = db
	repo := repository.NewSubscriptionRepository(db, params.Logger, testutil.NewInMemoryRedis())
	params.SubRepo = &subscriptionUpdateFailingRepository{Repository: repo}
	params.BillingTermsStore = repository.NewBillingTermsExecutionStore(db)
	require.NoError(t, repo.Create(ctx, sub))
	svc := NewSubscriptionService(params)
	_, err := svc.UpdateSubscription(ctx, sub.ID, dto.UpdateSubscriptionRequest{Status: types.SubscriptionStatusIncomplete})
	require.ErrorContains(t, err, "failure after database update")
	stored, err := repo.Get(types.WithForceWriter(ctx), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
	require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())

	params.SubRepo = repo
	_, err = NewSubscriptionService(params).UpdateSubscription(ctx, sub.ID, dto.UpdateSubscriptionRequest{Status: types.SubscriptionStatusIncomplete})
	require.NoError(t, err)
	stored, err = repo.Get(types.WithForceWriter(ctx), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusIncomplete, stored.SubscriptionStatus)
	events := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events()
	require.Len(t, events, 1)
	require.Equal(t, types.WebhookEventSubscriptionUpdated, events[0].EventName)
}

func TestSubscriptionUpdate_ParentCascadeContentionPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	s, child := newSubscriptionUpdateFixture(t)
	ctx, cancel := context.WithTimeout(s.GetContext(), 10*time.Second)
	defer cancel()
	params := s.params
	params.DB = db
	params.SubRepo = repository.NewSubscriptionRepository(db, params.Logger, testutil.NewInMemoryRedis())
	params.BillingTermsStore = repository.NewBillingTermsExecutionStore(db)
	parent := *child
	parent.ID = "z_parent"
	parent.SubscriptionType = types.SubscriptionTypeParent
	child.SubscriptionType = types.SubscriptionTypeInherited
	child.ParentSubscriptionID = &parent.ID
	require.NoError(t, params.SubRepo.Create(ctx, &parent))
	require.NoError(t, params.SubRepo.Create(ctx, child))
	svc := NewSubscriptionService(params).(*subscriptionService)
	done := make(chan error, 1)
	err := db.WithTx(ctx, func(tx context.Context) error {
		if _, err := params.SubRepo.GetForUpdate(tx, parent.ID); err != nil {
			return err
		}
		go func() {
			done <- svc.updateSubscription(ctx, child.ID, dto.UpdateSubscriptionRequest{Status: types.SubscriptionStatusIncomplete})
		}()
		select {
		case err := <-done:
			require.True(t, ierr.IsVersionConflict(err), "%+v", err)
		case <-ctx.Done():
			return ctx.Err()
		}
		// A failed prefix acquisition must release the child row before the cascade proceeds.
		lockedChild, err := params.SubRepo.GetForUpdate(tx, child.ID)
		if err != nil {
			return err
		}
		require.Equal(t, types.SubscriptionStatusActive, lockedChild.SubscriptionStatus)
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, svc.updateSubscription(ctx, child.ID, dto.UpdateSubscriptionRequest{Status: types.SubscriptionStatusIncomplete}))
}
