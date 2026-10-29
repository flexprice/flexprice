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

func (s *SubscriptionChangeV2Suite) TestLifecycleGuardBusyLeavesPlanAndCalendarUnchanged() {
	params := s.serviceParams()
	params.BillingTermsStore = &lifecycleTestStore{err: ierr.NewError("collector owns eligibility").Mark(ierr.ErrVersionConflict)}
	svc := NewSubscriptionService(params).(*subscriptionService)
	before := subscription.NewSubscriptionBuilder(s.currentSub()).Build()
	count := s.countInvoices()
	req := s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)
	req.BillingPeriodBehaviour = types.BillingPeriodBehaviourAnchorAtEffect
	_, err := svc.ExecutePlanChange(s.GetContext(), before.ID, req, time.Now().UTC())
	s.Require().True(ierr.IsVersionConflict(err), "%v", err)
	after := s.currentSub()
	s.Equal(before.PlanID, after.PlanID)
	s.Equal(before.BillingAnchor, after.BillingAnchor)
	s.Equal(before.CurrentPeriodStart, after.CurrentPeriodStart)
	s.Equal(before.CurrentPeriodEnd, after.CurrentPeriodEnd)
	s.Equal(count, s.countInvoices())
	s.Require().Len(s.liveLineItems(), 1)
	s.Equal(s.td.starterBase.ID, s.liveLineItems()[0].PriceID)
}

func (s *SubscriptionChangeV2Suite) TestLifecyclePauseBeforeLockRejectsPlanChange() {
	params := s.serviceParams()
	params.BillingTermsStore = &lifecycleTestStore{}
	before := subscription.NewSubscriptionBuilder(s.currentSub()).Build()
	params.SubRepo = &subscriptionUpdateRepository{Repository: params.SubRepo, t: s.T(), beforeLock: func(id string) {
		if id != before.ID {
			return
		}
		current, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), id)
		s.Require().NoError(err)
		s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), subscription.NewSubscriptionBuilder(current).WithStatus(types.SubscriptionStatusPaused).WithPauseStatus(types.PauseStatusActive).Build()))
	}}
	svc := NewSubscriptionService(params).(*subscriptionService)
	count := s.countInvoices()
	_, err := svc.ExecutePlanChange(s.GetContext(), before.ID, s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().Error(err)
	after := s.currentSub()
	s.Equal(types.SubscriptionStatusPaused, after.SubscriptionStatus)
	s.Equal(before.PlanID, after.PlanID)
	s.Equal(before.BillingAnchor, after.BillingAnchor)
	s.Equal(count, s.countInvoices())
}

func TestPlanChangeEligibilityContentionPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	suite := &SubscriptionChangeV2Suite{}
	suite.SetT(t)
	suite.SetupSuite()
	suite.SetupTest()
	t.Cleanup(suite.TearDownTest)
	ctx, cancel := context.WithTimeout(suite.GetContext(), 10*time.Second)
	defer cancel()
	params := suite.serviceParams()
	params.DB = db
	params.SubRepo = repository.NewSubscriptionRepository(db, params.Logger, testutil.NewInMemoryRedis())
	params.InvoiceRepo = repository.NewInvoiceRepository(db, params.Logger)
	params.BillingTermsStore = repository.NewBillingTermsExecutionStore(db)
	require.NoError(t, params.SubRepo.Create(ctx, suite.td.sub))
	svc := NewSubscriptionService(params).(*subscriptionService)
	before, err := params.SubRepo.Get(ctx, suite.td.sub.ID)
	require.NoError(t, err)
	require.NoError(t, db.WithTx(ctx, func(held context.Context) error {
		if err := params.BillingTermsStore.LockEligibility(held, []domain.EligibilityScope{{Type: "subscription", ID: before.ID}}); err != nil {
			return err
		}
		err := db.WithTx(ctx, func(caller context.Context) error {
			_, err := svc.ExecutePlanChange(caller, before.ID, suite.changeRequest(suite.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
			return err
		})
		require.True(t, ierr.IsVersionConflict(err), "nested plan change must retry behind source collection: %v", err)
		return nil
	}))
	after, err := params.SubRepo.Get(ctx, before.ID)
	require.NoError(t, err)
	require.Equal(t, before.PlanID, after.PlanID)
	require.Equal(t, before.BillingAnchor, after.BillingAnchor)
	require.Equal(t, before.CurrentPeriodStart, after.CurrentPeriodStart)
	require.Equal(t, before.CurrentPeriodEnd, after.CurrentPeriodEnd)
}
