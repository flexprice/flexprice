package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/planpricesync"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type PlanSwapEnablementSuite struct {
	testutil.BaseServiceTestSuite
	td planSwapTestData
}

type planSwapTestData struct {
	fromPlan *plan.Plan
	toPlan   *plan.Plan
	sub      *subscription.Subscription
}

const (
	fromPlanSeq = int64(5000)
	toPlanSeq   = int64(3000)
	laterToSeq  = int64(3500)
)

func TestPlanSwapEnablement(t *testing.T) {
	suite.Run(t, new(PlanSwapEnablementSuite))
}

func (s *PlanSwapEnablementSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupTestData()
}

func (s *PlanSwapEnablementSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *PlanSwapEnablementSuite) setupTestData() {
	ctx := s.GetContext()

	cust := &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "cust_plan_swap",
		Name:       "Plan Swap Co",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	s.td.fromPlan = &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Starter",
		LookupKey: "starter",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.td.toPlan = &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Pro",
		LookupKey: "pro",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.td.fromPlan))
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.td.toPlan))

	s.createPlanPrice(s.td.fromPlan.ID, fromPlanSeq)
	s.createPlanPrice(s.td.toPlan.ID, toPlanSeq)

	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	s.td.sub = &subscription.Subscription{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:          cust.ID,
		PlanID:              s.td.fromPlan.ID,
		SubscriptionStatus:  types.SubscriptionStatusActive,
		SubscriptionType:    types.SubscriptionTypeStandalone,
		Currency:            "usd",
		BillingAnchor:       periodStart,
		StartDate:           periodStart,
		CurrentPeriodStart:  periodStart,
		CurrentPeriodEnd:    periodStart.AddDate(0, 1, 0),
		BillingPeriod:       types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:  1,
		BillingCadence:      types.BILLING_CADENCE_RECURRING,
		BillingCycle:        types.BillingCycleAnniversary,
		SyncedPriceSequence: fromPlanSeq,
		Timezone:            "UTC",
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, s.td.sub))
}

func (s *PlanSwapEnablementSuite) createPlanPrice(planID string, seq int64) *price.Price {
	ctx := s.GetContext()
	p := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(1),
		Currency:           "usd",
		Type:               types.PRICE_TYPE_USAGE,
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           planID,
		MeterID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		Sequence:           seq,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
	return p
}

func (s *PlanSwapEnablementSuite) swapPlan(toPlanID string) {
	ctx := s.GetContext()
	sub, err := s.GetStores().SubscriptionRepo.GetForUpdate(ctx, s.td.sub.ID)
	s.Require().NoError(err)

	sub.PlanID = toPlanID
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))
}

func (s *PlanSwapEnablementSuite) staleForPlan(planID string) ([]string, []planpricesync.PlanLineItemCreationDelta) {
	ctx := s.GetContext()
	syncRepo := s.GetStores().PlanPriceSyncRepo

	targetSeq, err := syncRepo.CurrentPlanSequence(ctx, planID)
	s.Require().NoError(err)

	items, staleSubIDs, err := syncRepo.ListPlanLineItemsToCreateV2(ctx, planpricesync.ListPlanLineItemsToCreateV2Params{
		PlanID:    planID,
		TargetSeq: targetSeq,
	})
	s.Require().NoError(err)
	return staleSubIDs, items
}

func (s *PlanSwapEnablementSuite) TestPlanIDIsMutable() {
	ctx := s.GetContext()

	s.swapPlan(s.td.toPlan.ID)

	reloaded, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)

	s.Equal(s.td.toPlan.ID, reloaded.PlanID, "plan_id must persist the swap")
	s.Equal(s.td.sub.ID, reloaded.ID, "the subscription row survives; its id never changes")
	s.True(reloaded.BillingAnchor.Equal(s.td.sub.BillingAnchor), "the billing anchor is untouched by a swap")
	s.True(reloaded.CurrentPeriodStart.Equal(s.td.sub.CurrentPeriodStart))
	s.True(reloaded.CurrentPeriodEnd.Equal(s.td.sub.CurrentPeriodEnd))
}

func (s *PlanSwapEnablementSuite) TestCarriedSequenceHidesSubFromPlanPriceSync() {
	s.swapPlan(s.td.toPlan.ID)

	s.createPlanPrice(s.td.toPlan.ID, laterToSeq)

	staleSubIDs, items := s.staleForPlan(s.td.toPlan.ID)

	s.NotContains(staleSubIDs, s.td.sub.ID,
		"carrying sequence %d past a swap onto a plan at %d hides the subscription from sync",
		fromPlanSeq, laterToSeq)
	s.Empty(items, "and so no line item is ever created for the new plan price")
}

func (s *PlanSwapEnablementSuite) TestReanchorMakesSubTrackNewPlanPrices() {
	ctx := s.GetContext()
	syncRepo := s.GetStores().PlanPriceSyncRepo

	s.swapPlan(s.td.toPlan.ID)

	seqAtSwap, err := syncRepo.CurrentPlanSequence(ctx, s.td.toPlan.ID)
	s.Require().NoError(err)
	s.Equal(toPlanSeq, seqAtSwap)
	s.Require().NoError(syncRepo.ReanchorSubSyncedSequence(ctx, s.td.sub.ID, seqAtSwap))

	reloaded, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	s.Equal(toPlanSeq, reloaded.SyncedPriceSequence,
		"the watermark must be re-anchored to the target plan, moving downward here")

	staleSubIDs, _ := s.staleForPlan(s.td.toPlan.ID)
	s.NotContains(staleSubIDs, s.td.sub.ID, "a freshly swapped sub is in sync with its new plan")

	newPrice := s.createPlanPrice(s.td.toPlan.ID, laterToSeq)

	staleSubIDs, items := s.staleForPlan(s.td.toPlan.ID)
	s.Contains(staleSubIDs, s.td.sub.ID, "a later price change on the new plan must select the sub")
	s.Require().Len(items, 1)
	s.Equal(newPrice.ID, items[0].PriceID)
	s.Equal(s.td.sub.ID, items[0].SubscriptionID)
}

func (s *PlanSwapEnablementSuite) TestReanchorLowersWhereStampCannot() {
	ctx := s.GetContext()
	syncRepo := s.GetStores().PlanPriceSyncRepo

	updated, err := syncRepo.StampSubsAsSynced(ctx, planpricesync.StampSubsAsSyncedParams{
		TargetSeq: toPlanSeq,
		SubIDs:    []string{s.td.sub.ID},
	})
	s.Require().NoError(err)
	s.Zero(updated, "stamp is forward-only and must refuse to lower the watermark")

	afterStamp, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	s.Equal(fromPlanSeq, afterStamp.SyncedPriceSequence)

	s.Require().NoError(syncRepo.ReanchorSubSyncedSequence(ctx, s.td.sub.ID, toPlanSeq))

	afterReanchor, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	s.Equal(toPlanSeq, afterReanchor.SyncedPriceSequence, "re-anchor moves the watermark in either direction")
}

func (s *PlanSwapEnablementSuite) TestGetForUpdateReturnsSubscription() {
	ctx := s.GetContext()

	locked, err := s.GetStores().SubscriptionRepo.GetForUpdate(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	s.Equal(s.td.sub.ID, locked.ID)
	s.Equal(s.td.fromPlan.ID, locked.PlanID)

	_, err = s.GetStores().SubscriptionRepo.GetForUpdate(ctx, "sub_does_not_exist")
	s.Error(err, "a missing subscription must not silently return a zero value")
}
