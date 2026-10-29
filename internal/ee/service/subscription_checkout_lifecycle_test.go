package service

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestCheckoutLifecycle_RechecksEligibility(t *testing.T) {
	for _, status := range []types.SubscriptionStatus{types.SubscriptionStatusPaused, types.SubscriptionStatusCancelled, types.SubscriptionStatusActive} {
		t.Run(string(status), func(t *testing.T) {
			s, svc, sub, _, _ := newPaymentLifecycleFixture(t)
			sub.SubscriptionStatus = types.SubscriptionStatusDraft
			stale := subscription.NewSubscriptionBuilder(sub).Build()
			svc.BillingTermsStore = &lifecycleTestStore{hook: func(context.Context) { sub.SubscriptionStatus = status }}
			err := svc.activateDraftSubscription(s.GetContext(), stale)
			if status == types.SubscriptionStatusActive {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			fresh, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			require.Equal(t, status, fresh.SubscriptionStatus)
			require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
		})
	}
}

func TestCheckoutLifecycle_NestedGuardFailsWithoutEffects(t *testing.T) {
	s, svc, sub, _, _ := newPaymentLifecycleFixture(t)
	sub.SubscriptionStatus = types.SubscriptionStatusDraft
	store := &lifecycleTestStore{err: errors.New("eligibility busy")}
	svc.BillingTermsStore = store
	tx := types.WithTxStore(types.WithPostCommitHooks(context.WithValue(s.GetContext(), types.CtxDBTransaction, &ent.Tx{})))
	require.ErrorContains(t, svc.activateDraftSubscription(tx, sub), "eligibility busy")
	require.Equal(t, 1, store.tries)
	require.Equal(t, types.SubscriptionStatusDraft, sub.SubscriptionStatus)
	types.RunPostCommitHooks(tx)
	require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
}

func TestCheckoutLifecycle_ArchiveRejectsActivatedChild(t *testing.T) {
	s, svc, sub, _, _ := newPaymentLifecycleFixture(t)
	sub.SubscriptionStatus = types.SubscriptionStatusDraft
	sub.SubscriptionType = types.SubscriptionTypeParent
	child := subscription.NewSubscriptionBuilder(sub).
		WithStatus(types.SubscriptionStatusActive).
		WithSubscriptionType(types.SubscriptionTypeGroupedInvoicing).
		WithParentSubscriptionID(lo.ToPtr(sub.ID)).
		Build()
	child.ID = "checkout_child"
	require.NoError(t, svc.SubRepo.Create(s.GetContext(), child))
	require.ErrorContains(t, svc.archiveDraftCheckoutSubscription(s.GetContext(), sub.ID), "child has left its initial draft or trial")
	for _, id := range []string{sub.ID, child.ID} {
		fresh, err := svc.SubRepo.Get(s.GetContext(), id)
		require.NoError(t, err)
		require.Equal(t, types.StatusPublished, fresh.Status)
	}
}
