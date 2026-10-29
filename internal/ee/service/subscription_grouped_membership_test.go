package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestGroupedMembershipLocksBeforeMutation(t *testing.T) {
	s, parent := newSubscriptionUpdateFixture(t)
	parent.SubscriptionType = types.SubscriptionTypeStandalone
	child := subscription.NewSubscriptionBuilder(parent).Build()
	child.ID = "a_grouped_child"
	require.NoError(t, s.params.SubRepo.Create(s.GetContext(), child))
	order := []string{}
	store := &lifecycleTestStore{order: &order}
	svc := s.service.(*subscriptionService)
	svc.BillingTermsStore = store
	svc.SubRepo = &subscriptionUpdateRepository{Repository: s.params.SubRepo, t: t, order: &order}
	require.NoError(t, svc.addToGroupedInvoicing(s.GetContext(), parent, child.ID))
	require.Equal(t, []string{child.ID, parent.ID, "eligibility"}, order)
	require.Equal(t, types.SubscriptionTypeParent, parent.SubscriptionType)
	fresh, err := s.params.SubRepo.Get(s.GetContext(), child.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionTypeGroupedInvoicing, fresh.SubscriptionType)
	require.Equal(t, parent.ID, *fresh.ParentSubscriptionID)
	start, end, anchor := fresh.StartDate, fresh.EndDate, fresh.BillingAnchor
	require.NoError(t, svc.removeFromGroupedInvoicing(s.GetContext(), child.ID))
	fresh, err = s.params.SubRepo.Get(s.GetContext(), child.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionTypeStandalone, fresh.SubscriptionType)
	require.Nil(t, fresh.ParentSubscriptionID)
	require.Equal(t, start, fresh.StartDate)
	require.Equal(t, end, fresh.EndDate)
	require.Equal(t, anchor, fresh.BillingAnchor)
}

func TestGroupedMembershipRejectsDriftAndBusyGuard(t *testing.T) {
	for _, scenario := range []string{"row drift", "busy guard", "paused parent", "foreign scope"} {
		t.Run(scenario, func(t *testing.T) {
			s, parent := newSubscriptionUpdateFixture(t)
			parent.SubscriptionType = types.SubscriptionTypeStandalone
			child := subscription.NewSubscriptionBuilder(parent).Build()
			child.ID = "a_grouped_child"
			require.NoError(t, s.params.SubRepo.Create(s.GetContext(), child))
			store := &lifecycleTestStore{}
			svc := s.service.(*subscriptionService)
			svc.BillingTermsStore = store
			repo := &subscriptionUpdateRepository{Repository: s.params.SubRepo, t: t}
			svc.SubRepo = repo
			switch scenario {
			case "row drift":
				repo.beforeLock = func(id string) {
					if id == child.ID {
						child.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
						child.ParentSubscriptionID = &parent.ID
					}
				}
			case "busy guard":
				store.err = ierr.NewError("busy guard").Mark(ierr.ErrVersionConflict)
			case "paused parent":
				store.hook = func(context.Context) { parent.SubscriptionStatus = types.SubscriptionStatusPaused }
			case "foreign scope":
				child.EnvironmentID = "another_environment"
			}
			err := svc.addToGroupedInvoicing(s.GetContext(), parent, child.ID)
			require.Error(t, err)
			require.Equal(t, types.SubscriptionTypeStandalone, parent.SubscriptionType, "parent promotion must wait for authoritative eligibility")
			if scenario == "row drift" || scenario == "busy guard" {
				require.True(t, ierr.IsVersionConflict(err), "%v", err)
			}
			if scenario != "row drift" {
				require.Nil(t, child.ParentSubscriptionID)
			}
		})
	}
}

func TestGroupedMembershipBoundsAndNestedGuard(t *testing.T) {
	s, parent := newSubscriptionUpdateFixture(t)
	child := subscription.NewSubscriptionBuilder(parent).Build()
	child.ID = "a_child"
	require.NoError(t, s.params.SubRepo.Create(s.GetContext(), child))
	store := &lifecycleTestStore{}
	svc := s.service.(*subscriptionService)
	svc.BillingTermsStore = store
	require.NoError(t, svc.DB.WithTx(s.GetContext(), func(tx context.Context) error {
		_, err := svc.lockGroupedMembership(tx, parent.ID, []string{child.ID})
		return err
	}))
	require.Equal(t, 1, store.tries)
	for _, ids := range [][]string{nil, {child.ID, child.ID}, make([]string, 501)} {
		require.Error(t, svc.DB.WithTx(s.GetContext(), func(tx context.Context) error { _, err := svc.lockGroupedMembership(tx, parent.ID, ids); return err }))
	}
	require.Equal(t, 1, store.tries, "invalid sets must fail before acquiring guards")
}
