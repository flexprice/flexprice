package service

import (
	"context"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	repository "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestGroupedMembershipSerializesWithCollectionPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	s, parent := newSubscriptionUpdateFixture(t)
	ctx, cancel := context.WithTimeout(s.GetContext(), 10*time.Second)
	defer cancel()
	params := s.params
	params.DB = db
	params.SubRepo = repository.NewSubscriptionRepository(db, params.Logger, testutil.NewInMemoryRedis())
	params.BillingTermsStore = repository.NewBillingTermsExecutionStore(db)
	parent = subscription.NewSubscriptionBuilder(parent).WithSubscriptionType(types.SubscriptionTypeStandalone).Build()
	child := subscription.NewSubscriptionBuilder(parent).Build()
	child.ID = "grouped_child"
	require.NoError(t, params.SubRepo.Create(ctx, parent))
	require.NoError(t, params.SubRepo.Create(ctx, child))
	svc := NewSubscriptionService(params).(*subscriptionService)
	require.NoError(t, db.WithTx(ctx, func(held context.Context) error {
		if err := params.BillingTermsStore.LockEligibility(held, []domain.EligibilityScope{{Type: "subscription", ID: parent.ID}}); err != nil {
			return err
		}
		err := svc.addToGroupedInvoicing(ctx, parent, child.ID)
		require.True(t, ierr.IsVersionConflict(err), "membership must retry while invoice collection owns the destination: %v", err)
		return nil
	}))
	stored, err := params.SubRepo.Get(ctx, child.ID)
	require.NoError(t, err)
	require.Nil(t, stored.ParentSubscriptionID)
	require.Equal(t, types.SubscriptionTypeStandalone, stored.SubscriptionType)
	require.NoError(t, svc.addToGroupedInvoicing(ctx, parent, child.ID))
	require.NoError(t, db.WithTx(ctx, func(held context.Context) error {
		if err := params.BillingTermsStore.LockEligibility(held, []domain.EligibilityScope{{Type: "subscription", ID: parent.ID}}); err != nil {
			return err
		}
		err := svc.removeFromGroupedInvoicing(ctx, child.ID)
		require.True(t, ierr.IsVersionConflict(err), "removal must retry while invoice collection owns the destination: %v", err)
		return nil
	}))
	require.NoError(t, svc.removeFromGroupedInvoicing(ctx, child.ID))
}
