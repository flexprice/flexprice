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
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

type subscriptionUpdateStore struct {
	domain.ExecutionStore
	scopes []domain.EligibilityScope
	order  *[]string
	err    error
	nested bool
}

func (s *subscriptionUpdateStore) InTransaction(context.Context) bool { return s.nested }
func (s *subscriptionUpdateStore) LockEligibility(_ context.Context, scopes []domain.EligibilityScope) error {
	s.scopes = append([]domain.EligibilityScope(nil), scopes...)
	if s.order != nil {
		*s.order = append(*s.order, "eligibility")
	}
	return s.err
}

type subscriptionUpdateRepository struct {
	subscription.Repository
	t          *testing.T
	order      *[]string
	beforeLock func(string)
	stale      *subscription.Subscription
}

func (r *subscriptionUpdateRepository) List(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	require.True(r.t, types.ShouldForceWriter(ctx))
	rows, err := r.Repository.List(ctx, filter)
	detached := make([]*subscription.Subscription, 0, len(rows))
	for _, row := range rows {
		cloned := *row
		detached = append(detached, &cloned)
	}
	return detached, err
}

func (r *subscriptionUpdateRepository) GetForUpdateNowait(ctx context.Context, id string) (*subscription.Subscription, error) {
	if r.order != nil {
		*r.order = append(*r.order, id)
	}
	if r.beforeLock != nil {
		r.beforeLock(id)
	}
	return r.Repository.GetForUpdateNowait(ctx, id)
}

func (r *subscriptionUpdateRepository) Get(ctx context.Context, id string) (*subscription.Subscription, error) {
	if r.stale != nil && r.stale.ID == id {
		return r.stale, nil
	}
	return r.Repository.Get(ctx, id)
}

func newSubscriptionUpdateFixture(t *testing.T) (*SubscriptionPauseSuite, *subscription.Subscription) {
	t.Helper()
	s := &SubscriptionPauseSuite{}
	s.SetT(t)
	s.SetupSuite()
	s.SetupTest()
	t.Cleanup(s.TearDownTest)
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := s.subscriptionWithCharges("sub_update", start, start.AddDate(0, 1, 0))
	return s, sub
}

func TestSubscriptionUpdate_LocksCompleteParentSet(t *testing.T) {
	s, sub := newSubscriptionUpdateFixture(t)
	ctx := s.GetContext()
	start, end := sub.CurrentPeriodStart, sub.CurrentPeriodEnd
	oldParent := s.subscriptionWithCharges("z_old_parent", start, end)
	newParent := s.subscriptionWithCharges("a_new_parent", start, end)
	sub.ParentSubscriptionID = &oldParent.ID
	order := []string{}
	store := &subscriptionUpdateStore{order: &order}
	svc := s.service.(*subscriptionService)
	svc.BillingTermsStore = store
	svc.SubRepo = &subscriptionUpdateRepository{Repository: s.params.SubRepo, t: t, order: &order}
	_, err := svc.UpdateSubscription(ctx, sub.ID, dto.UpdateSubscriptionRequest{ParentSubscriptionID: &newParent.ID})
	require.NoError(t, err)
	require.Equal(t, []string{newParent.ID, sub.ID, oldParent.ID, "eligibility"}, order)
	require.Equal(t, []domain.EligibilityScope{
		{Type: "subscription", ID: newParent.ID},
		{Type: "subscription", ID: sub.ID},
		{Type: "subscription", ID: oldParent.ID},
	}, store.scopes)
	stored, err := s.params.SubRepo.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, newParent.ID, *stored.ParentSubscriptionID)
	require.Equal(t, oldParent.ID, *sub.ParentSubscriptionID, "the locked input is not mutated")
}

func TestSubscriptionUpdate_MembershipDriftDoesNotAcquireLateLocks(t *testing.T) {
	s, sub := newSubscriptionUpdateFixture(t)
	order := []string{}
	store := &subscriptionUpdateStore{order: &order}
	svc := s.service.(*subscriptionService)
	svc.BillingTermsStore = store
	svc.SubRepo = &subscriptionUpdateRepository{
		Repository: s.params.SubRepo, t: t, order: &order,
		beforeLock: func(string) { sub.ParentSubscriptionID = lo.ToPtr("newly_assigned_parent") },
	}
	_, err := svc.UpdateSubscription(s.GetContext(), sub.ID, dto.UpdateSubscriptionRequest{Status: types.SubscriptionStatusIncomplete})
	require.True(t, ierr.IsVersionConflict(err), "%+v", err)
	require.Equal(t, []string{sub.ID}, order)
	require.Empty(t, store.scopes)
	require.Equal(t, types.SubscriptionStatusActive, sub.SubscriptionStatus)
	require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
}

func TestSubscriptionUpdate_RejectsStalePauseAndMissingGuards(t *testing.T) {
	for _, scenario := range []string{"stale pause", "missing store", "nested transaction", "guard failure", "missing scope"} {
		t.Run(scenario, func(t *testing.T) {
			s, sub := newSubscriptionUpdateFixture(t)
			svc := s.service.(*subscriptionService)
			stale := *sub
			svc.SubRepo = &subscriptionUpdateRepository{Repository: s.params.SubRepo, t: t, stale: &stale}
			store := &subscriptionUpdateStore{}
			svc.BillingTermsStore = store
			ctx := s.GetContext()
			switch scenario {
			case "stale pause":
				sub.SubscriptionStatus = types.SubscriptionStatusPaused
				sub.ActivePauseID = lo.ToPtr("pause_latest")
			case "missing store":
				svc.BillingTermsStore = nil
			case "nested transaction":
				store.nested = true
			case "guard failure":
				store.err = errors.New("guard unavailable")
			case "missing scope":
				ctx = types.SetEnvironmentID(ctx, "")
			}
			before := sub.SubscriptionStatus
			_, err := svc.UpdateSubscription(ctx, sub.ID, dto.UpdateSubscriptionRequest{Status: types.SubscriptionStatusIncomplete})
			require.Error(t, err)
			require.Equal(t, before, sub.SubscriptionStatus)
			require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
		})
	}
}
