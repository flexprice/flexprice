package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/planpricesync"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// sequenceReadHook runs beforeRead once, just before the plan sequence is read.
type sequenceReadHook struct {
	planpricesync.Repository
	beforeRead func()
}

func (r *sequenceReadHook) CurrentPlanSequence(ctx context.Context, planID string) (int64, error) {
	if r.beforeRead != nil {
		r.beforeRead()
		r.beforeRead = nil
	}
	return r.Repository.CurrentPlanSequence(ctx, planID)
}

// A price added between the plan-sequence read and the price read must either get a
// line item or stay above synced_price_sequence, so the next sync still creates it.
func (s *SubscriptionServiceSuite) TestCreateSubscription_SyncedSequenceNeverCoversMissedPrice() {
	ctx := s.GetContext()
	const addedSeq = int64(1000)
	added := *s.testData.prices.apiCalls
	added.ID, added.Sequence = "price_added_mid_create", addedSeq

	params := s.service.(*subscriptionService).ServiceParams
	params.PlanPriceSyncRepo = &sequenceReadHook{
		Repository: s.GetStores().PlanPriceSyncRepo,
		beforeRead: func() { s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &added)) },
	}

	resp, err := NewSubscriptionService(params).CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		StartDate:          lo.ToPtr(s.testData.now),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
	})
	s.Require().NoError(err)

	sub, items, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, resp.ID)
	s.Require().NoError(err)
	hasLineItem := lo.ContainsBy(items, func(li *subscription.SubscriptionLineItem) bool { return li.PriceID == added.ID })
	s.True(hasLineItem || sub.SyncedPriceSequence < addedSeq,
		"synced sequence %d covers the added price but the sub has no line item for it", sub.SyncedPriceSequence)
}

// Same guarantee for a plan change: the swapped-in line items and synced_price_sequence must agree.
func (s *SubscriptionChangeV2Suite) TestExecute_SyncedSequenceNeverCoversMissedPrice() {
	ctx := s.GetContext()
	const addedSeq = int64(1000)
	var added *price.Price

	params := s.serviceParams()
	params.PlanPriceSyncRepo = &sequenceReadHook{
		Repository: s.GetStores().PlanPriceSyncRepo,
		beforeRead: func() { added = s.createUsagePriceWithSequence(s.td.pro.ID, addedSeq) },
	}

	_, err := NewSubscriptionService(params).ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().NoError(err)

	s.Require().NotNil(added)
	hasLineItem := lo.ContainsBy(s.liveLineItems(), func(li *subscription.SubscriptionLineItem) bool { return li.PriceID == added.ID })
	s.True(hasLineItem || s.currentSub().SyncedPriceSequence < addedSeq,
		"synced sequence %d covers the added price but the sub has no line item for it", s.currentSub().SyncedPriceSequence)
}
