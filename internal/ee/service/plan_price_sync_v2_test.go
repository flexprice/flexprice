package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type PlanPriceSyncV2Suite struct {
	testutil.BaseServiceTestSuite
	svc *planService
}

func TestPlanPriceSyncV2(t *testing.T) {
	suite.Run(t, new(PlanPriceSyncV2Suite))
}

func (s *PlanPriceSyncV2Suite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewPlanService(fullServiceParams(&s.BaseServiceTestSuite)).(*planService)
}

var (
	syncStart   = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	oldPriceEnd = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	newPriceEnd = time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
)

func (s *PlanPriceSyncV2Suite) createPrice(id string, seq int64, end *time.Time) {
	ctx := s.GetContext()
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &price.Price{
		ID:                 id,
		Amount:             decimal.NewFromInt(1),
		Currency:           "usd",
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           "plan_sync",
		MeterID:            "meter_sync",
		StartDate:          lo.ToPtr(syncStart),
		EndDate:            end,
		Sequence:           seq,
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}))
}

// createSub creates a sub at the given synced sequence with a live line item for each price.
func (s *PlanPriceSyncV2Suite) createSub(id string, syncedSeq int64, priceIDs ...string) {
	ctx := s.GetContext()
	sub := &subscription.Subscription{
		ID:                  id,
		CustomerID:          "cust_" + id,
		PlanID:              "plan_sync",
		SubscriptionStatus:  types.SubscriptionStatusActive,
		SubscriptionType:    types.SubscriptionTypeStandalone,
		Currency:            "usd",
		BillingAnchor:       syncStart,
		StartDate:           syncStart,
		CurrentPeriodStart:  syncStart,
		CurrentPeriodEnd:    syncStart.AddDate(0, 1, 0),
		BillingPeriod:       types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:  1,
		SyncedPriceSequence: syncedSeq,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	items := lo.Map(priceIDs, func(priceID string, _ int) *subscription.SubscriptionLineItem {
		return &subscription.SubscriptionLineItem{
			ID:             "subsli_" + id + "_" + priceID,
			SubscriptionID: id,
			CustomerID:     sub.CustomerID,
			EntityID:       "plan_sync",
			EntityType:     types.SubscriptionLineItemEntityTypePlan,
			PriceID:        priceID,
			PriceType:      types.PRICE_TYPE_USAGE,
			Quantity:       decimal.Zero,
			Currency:       "usd",
			BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
			StartDate:      syncStart,
			EnvironmentID:  types.GetEnvironmentID(ctx),
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}
	})
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, items))
}

func (s *PlanPriceSyncV2Suite) lineItemEnds(subID string) map[string]time.Time {
	_, items, err := s.GetStores().SubscriptionRepo.GetWithLineItems(s.GetContext(), subID)
	s.Require().NoError(err)
	return lo.SliceToMap(items, func(li *subscription.SubscriptionLineItem) (string, time.Time) {
		return li.PriceID, li.EndDate
	})
}

func (s *PlanPriceSyncV2Suite) TestSync_TerminatesOnlyPricesEndedSinceSyncedSequence() {
	ctx := s.GetContext()
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, &plan.Plan{
		ID:            "plan_sync",
		Name:          "Plan",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))
	s.createPrice("price_old", 5, &oldPriceEnd)
	s.createPrice("price_at_synced_seq", 10, &oldPriceEnd)
	s.createPrice("price_live", 11, nil)
	s.createPrice("price_new", 12, &newPriceEnd)
	s.createPrice("price_added", 13, nil)
	// sub_a already saw price_old end (synced at 10); sub_b saw neither end (synced at 3).
	s.createSub("sub_a", 10, "price_old", "price_at_synced_seq", "price_live", "price_new")
	s.createSub("sub_b", 3, "price_old", "price_live", "price_new")

	resp, err := s.svc.SyncPlanPricesV2(ctx, "plan_sync")
	s.Require().NoError(err)
	// price_added for both subs, plus price_at_synced_seq for sub_b, which never had it.
	s.Equal(3, resp.Summary.LineItemsCreated)
	s.Equal(3, resp.Summary.LineItemsTerminated)

	a := s.lineItemEnds("sub_a")
	s.True(a["price_old"].IsZero(), "price ended before sub_a's synced sequence is not re-applied")
	s.True(a["price_at_synced_seq"].IsZero(), "a sequence equal to the synced sequence is already applied")
	s.True(a["price_new"].Equal(newPriceEnd))
	s.True(a["price_live"].IsZero())
	s.Require().Contains(a, "price_added")
	s.True(a["price_added"].IsZero())

	b := s.lineItemEnds("sub_b")
	s.True(b["price_old"].Equal(oldPriceEnd))
	s.True(b["price_new"].Equal(newPriceEnd))
	s.Require().Contains(b, "price_added")
	s.True(b["price_added"].IsZero())

	for _, id := range []string{"sub_a", "sub_b"} {
		sub, err := s.GetStores().SubscriptionRepo.Get(ctx, id)
		s.Require().NoError(err)
		s.Equal(int64(13), sub.SyncedPriceSequence)
	}
}
