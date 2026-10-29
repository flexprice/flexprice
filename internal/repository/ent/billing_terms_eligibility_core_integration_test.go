package ent

import (
	"context"
	"testing"
	"time"

	subscriptiondb "github.com/flexprice/flexprice/ent/subscription"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type guardedSubscriptionOwner struct {
	client         postgres.IClient
	snapshot       domain.OwnerSnapshot
	subscriptionID string
	reads          chan types.SubscriptionStatus
}

func (a *guardedSubscriptionOwner) Type() types.BillingTermOwnerType { return a.snapshot.Owner.Type }

func (a *guardedSubscriptionOwner) Snapshot(ctx context.Context, _ domain.OwnerRef) (*domain.OwnerSnapshot, error) {
	sub, err := a.client.Reader(ctx).Subscription.Query().Where(
		subscriptiondb.ID(a.subscriptionID), subscriptiondb.TenantID(types.GetTenantID(ctx)),
		subscriptiondb.EnvironmentID(types.GetEnvironmentID(ctx)), subscriptiondb.Status(string(types.StatusPublished)),
	).Only(ctx)
	if err != nil {
		return nil, err
	}
	snapshot := a.snapshot
	snapshot.CanEdit = sub.SubscriptionStatus == types.SubscriptionStatusActive
	snapshot.CanCollect = snapshot.CanEdit
	snapshot.LifecycleKey = string(sub.SubscriptionStatus)
	snapshot.EligibilityScopes = []domain.EligibilityScope{{Type: "subscription", ID: sub.ID}}
	select {
	case a.reads <- sub.SubscriptionStatus:
		return &snapshot, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type observedEligibilityStore struct {
	domain.ExecutionStore
	eligibility chan []domain.EligibilityScope
	owners      chan domain.OwnerRef
}

func (s *observedEligibilityStore) LockEligibility(ctx context.Context, scopes []domain.EligibilityScope) error {
	select {
	case s.eligibility <- append([]domain.EligibilityScope(nil), scopes...):
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.ExecutionStore.LockEligibility(ctx, scopes)
}

func (s *observedEligibilityStore) LockOwner(ctx context.Context, owner domain.OwnerRef) error {
	select {
	case s.owners <- owner:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.ExecutionStore.LockOwner(ctx, owner)
}

func TestBillingTermsEligibilityRevalidatesAfterWaitPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx, cancel := context.WithTimeout(billingTestContext("guarded_core_tenant", "guarded_core_environment"), 10*time.Second)
	defer cancel()
	subscriptions := NewSubscriptionRepository(f.client, logger.NewNoopLogger(), newFakeRedisCache())
	sub := subscriptionTestFixture(ctx, types.GenerateUUIDWithPrefix("sub"), types.SubscriptionStatusActive, nil, nil)
	require.NoError(t, subscriptions.Create(ctx, sub))
	owner := newBillingCoreFixture(t, f, ctx).owner
	adapter := &guardedSubscriptionOwner{client: f.client, snapshot: owner, subscriptionID: sub.ID, reads: make(chan types.SubscriptionStatus, 2)}
	resolver, err := core.NewOwnerResolver(adapter)
	require.NoError(t, err)
	store := NewBillingTermsExecutionStore(f.client)
	observed := &observedEligibilityStore{ExecutionStore: store, eligibility: make(chan []domain.EligibilityScope, 1), owners: make(chan domain.OwnerRef, 1)}
	service := core.NewService(f.rules, f.items, observed, resolver)
	scope := domain.EligibilityScope{Type: "subscription", ID: sub.ID}
	held, changeStatus, holderDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		holderDone <- store.WithTx(ctx, func(tx context.Context) error {
			if _, err := subscriptions.GetForUpdate(tx, sub.ID); err != nil {
				return err
			}
			if err := store.LockEligibility(tx, []domain.EligibilityScope{scope}); err != nil {
				return err
			}
			close(held)
			select {
			case <-changeStatus:
			case <-ctx.Done():
				return ctx.Err()
			}
			_, err := f.client.Writer(tx).Subscription.Update().Where(
				subscriptiondb.ID(sub.ID), subscriptiondb.TenantID(types.GetTenantID(tx)),
				subscriptiondb.EnvironmentID(types.GetEnvironmentID(tx)), subscriptiondb.Status(string(types.StatusPublished)),
			).SetSubscriptionStatus(types.SubscriptionStatusCancelled).Save(tx)
			return err
		})
	}()
	select {
	case <-held:
	case err := <-holderDone:
		t.Fatalf("lifecycle guard holder failed: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	initialized := make(chan error, 1)
	go func() {
		_, err := service.Initialize(ctx, core.InitializeRequest{Owner: owner.Owner, Rows: []core.RuleInput{billingCoreOnce(100, owner.StartDate, "Initial arrangement")}})
		initialized <- err
	}()
	select {
	case status := <-adapter.reads:
		require.Equal(t, types.SubscriptionStatusActive, status)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case scopes := <-observed.eligibility:
		require.Equal(t, []domain.EligibilityScope{scope}, scopes)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-observed.owners:
		t.Fatal("initialization attempted an owner lock before its eligibility guard was available")
	case err := <-initialized:
		t.Fatalf("initialization completed while the lifecycle guard was held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(changeStatus)
	require.NoError(t, <-holderDone)
	err = <-initialized
	require.True(t, ierr.IsValidation(err), "the fresh cancelled state must reject initialization: %v", err)
	select {
	case status := <-adapter.reads:
		require.Equal(t, types.SubscriptionStatusCancelled, status, "the post-lock snapshot reads the committed lifecycle change")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	rules, err := f.rules.List(ctx, &domain.RuleFilter{Owner: &owner.Owner, IncludeArchived: true})
	require.NoError(t, err)
	require.Empty(t, rules)
	items, err := f.items.List(ctx, &domain.ScheduleItemFilter{Owner: &owner.Owner, IncludeArchived: true})
	require.NoError(t, err)
	require.Empty(t, items)
}
