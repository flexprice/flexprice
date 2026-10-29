package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// subGrantFixture is a subscription with two flat-priced usage lines (calls $0.10,
// storage $1) and a subscription entitlement grant over both.
type subGrantFixture struct {
	sub                    *subscription.Subscription
	callsLine, storeLine   string
	callsMeter, storeMeter string
	extID                  string
}

func (s *EntitlementGrantSuite) newSubGrantFixture(tag string, ec *entitlement.Entitlement) subGrantFixture {
	fx := s.newSubGrantSubscription(tag)
	ec.ID = "ent-subgrant-" + tag
	ec.EntityType = types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION
	ec.EntityID = fx.sub.ID
	ec.GrantScope = types.EntitlementGrantScopeSubscription
	ec.GrantMeasure = types.EntitlementGrantMeasureAmount
	ec.IsEnabled = true
	ec.BaseModel = types.GetDefaultBaseModel(s.GetContext())
	s.Require().NoError(ec.Validate())
	_, err := s.GetStores().EntitlementRepo.Create(s.GetContext(), ec)
	s.Require().NoError(err)
	return fx
}

// newSubGrantSubscription is the fixture subscription and its two usage lines, with no grant.
func (s *EntitlementGrantSuite) newSubGrantSubscription(tag string) subGrantFixture {
	ctx := s.GetContext()
	calls := s.simpleMeter("meter-calls-" + tag)
	store := s.simpleMeter("meter-store-" + tag)
	p := s.simplePlan("plan-" + tag)
	cust := s.simpleCustomer("cust-" + tag)
	sub := s.simpleSubscription("sub-"+tag, cust.ID, p.ID)

	addLine := func(id string, meterID string, unit float64) {
		pr := &price.Price{
			ID:           "price-" + id,
			Amount:       decimal.NewFromFloat(unit),
			Currency:     "usd",
			Type:         types.PRICE_TYPE_USAGE,
			BillingModel: types.BILLING_MODEL_FLAT_FEE,
			MeterID:      meterID,
			BaseModel:    types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, pr))
		s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, &subscription.SubscriptionLineItem{
			ID:             id,
			SubscriptionID: sub.ID,
			CustomerID:     cust.ID,
			EntityID:       p.ID,
			EntityType:     types.SubscriptionLineItemEntityTypePlan,
			PriceID:        pr.ID,
			PriceType:      types.PRICE_TYPE_USAGE,
			MeterID:        meterID,
			Currency:       "usd",
			BillingPeriod:  sub.BillingPeriod,
			InvoiceCadence: types.InvoiceCadenceArrear,
			StartDate:      sub.StartDate,
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}))
	}
	addLine("sli-calls-"+tag, calls.ID, 0.10)
	addLine("sli-store-"+tag, store.ID, 1)

	return subGrantFixture{
		sub: sub, callsLine: "sli-calls-" + tag, storeLine: "sli-store-" + tag,
		callsMeter: calls.ID, storeMeter: store.ID, extID: cust.ExternalID,
	}
}

func dailySubscriptionEntitlementGrant(quota int64, behavior types.EntitlementGrantAllocationBehavior) *entitlement.Entitlement {
	return &entitlement.Entitlement{
		GrantDurationUnit:       types.EntitlementGrantDurationUnitDay,
		GrantDurationValue:      lo.ToPtr(1),
		GrantAllocationBehavior: behavior,
		GrantQuota:              lo.ToPtr(decimal.NewFromInt(quota)),
	}
}

func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrant_OpensAtFirstEventOnAnyMeter() {
	fx := s.newSubGrantFixture("first", dailySubscriptionEntitlementGrant(10, types.EntitlementGrantAllocationBehaviorFirstUsage))
	// The first event lands on the storage meter, not the calls meter.
	first := fx.sub.CurrentPeriodStart.Add(40 * time.Minute)
	s.seedMeterUsage(fx.extID, fx.storeMeter, first, 1)
	s.seedMeterUsage(fx.extID, fx.callsMeter, first.Add(time.Hour), 10)

	at := fx.sub.CurrentPeriodStart.Add(3 * time.Hour)
	grants, _, err := s.grantService.EnsureGrantsForSubscriptions(s.GetContext(),
		s.customerOf(fx.sub), []*subscription.Subscription{fx.sub}, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	g := grants[0]
	s.Equal(types.EntitlementGrantScopeSubscription, g.ScopeEntityType)
	s.Equal(fx.sub.ID, g.ScopeEntityID)
	s.Equal(types.EntitlementGrantMeasureAmount, g.Measure)
	s.True(g.ValidFrom.Equal(first), "window opens at the first event on any meter, got %s", g.ValidFrom)
	s.True(g.ValidTo.Equal(first.Add(24 * time.Hour)))
}

func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrant_UnitStartAlignsToDay() {
	fx := s.newSubGrantFixture("unit", dailySubscriptionEntitlementGrant(10, types.EntitlementGrantAllocationBehaviorUnitStart))
	s.seedMeterUsage(fx.extID, fx.callsMeter, fx.sub.CurrentPeriodStart.Add(26*time.Hour), 10)

	at := fx.sub.CurrentPeriodStart.Add(27 * time.Hour)
	grants, _, err := s.grantService.EnsureGrantsForSubscriptions(s.GetContext(),
		s.customerOf(fx.sub), []*subscription.Subscription{fx.sub}, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	dayStart := types.FloorToStartOfDay(fx.sub.CurrentPeriodStart, fx.sub.Timezone)
	for !dayStart.Add(24 * time.Hour).After(fx.sub.CurrentPeriodStart.Add(26 * time.Hour)) {
		dayStart = dayStart.Add(24 * time.Hour)
	}
	s.True(grants[0].ValidFrom.Equal(types.LatestOf(dayStart, fx.sub.CurrentPeriodStart)),
		"unit_start window starts on the day boundary, got %s", grants[0].ValidFrom)
}

func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrant_NoUsage_NoWindow() {
	fx := s.newSubGrantFixture("idle", dailySubscriptionEntitlementGrant(10, types.EntitlementGrantAllocationBehaviorFirstUsage))
	grants, _, err := s.grantService.EnsureGrantsForSubscriptions(s.GetContext(),
		s.customerOf(fx.sub), []*subscription.Subscription{fx.sub}, fx.sub.CurrentPeriodStart.Add(time.Hour))
	s.Require().NoError(err)
	s.Empty(grants)
}

func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrant_EvaluateTotalsEveryLine() {
	fx := s.newSubGrantFixture("eval", dailySubscriptionEntitlementGrant(10, types.EntitlementGrantAllocationBehaviorFirstUsage))
	first := fx.sub.CurrentPeriodStart.Add(30 * time.Minute)
	s.seedMeterUsage(fx.extID, fx.callsMeter, first, 100)                // $10
	s.seedMeterUsage(fx.extID, fx.storeMeter, first.Add(time.Minute), 5) // $5

	at := fx.sub.CurrentPeriodStart.Add(2 * time.Hour)
	grants, meta, err := s.grantService.EnsureGrantsForSubscriptions(s.GetContext(),
		s.customerOf(fx.sub), []*subscription.Subscription{fx.sub}, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)

	s.Require().NoError(s.evalAlertService().evaluateEntitlementGrantsForCustomer(
		s.GetContext(), s.customerOf(fx.sub), meta, grants, at))

	stored, err := s.GetStores().EntitlementGrantRepo.Get(s.GetContext(), grants[0].ID)
	s.Require().NoError(err)
	s.True(stored.Usage.Equal(decimal.NewFromInt(15)), "usage = $10 calls + $5 storage, got %s", stored.Usage)
	s.Equal(types.EntitlementGrantStatusExhausted, stored.GrantStatus)
	s.Require().NotNil(stored.QuotaCrossedAt)
}

func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrant_ParallelGrantsOpenOneWindowEach() {
	fx := s.newSubGrantFixture("par", &entitlement.Entitlement{
		GrantDurationUnit:       types.EntitlementGrantDurationUnitDay,
		GrantDurationValue:      lo.ToPtr(1),
		GrantAllocationBehavior: types.EntitlementGrantAllocationBehaviorFirstUsage,
		GrantQuota:              lo.ToPtr(decimal.NewFromInt(10)),
		AggregationMode:         types.EntitlementAggregationModeParallel,
	})
	monthly := &entitlement.Entitlement{
		ID:                "ent-subgrant-par-monthly",
		EntityType:        types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:          fx.sub.ID,
		GrantScope:        types.EntitlementGrantScopeSubscription,
		GrantMeasure:      types.EntitlementGrantMeasureAmount,
		GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod,
		GrantQuota:        lo.ToPtr(decimal.NewFromInt(200)),
		AggregationMode:   types.EntitlementAggregationModeParallel,
		IsEnabled:         true,
		BaseModel:         types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(monthly.Validate())
	_, err := s.GetStores().EntitlementRepo.Create(s.GetContext(), monthly)
	s.Require().NoError(err)
	s.seedMeterUsage(fx.extID, fx.callsMeter, fx.sub.CurrentPeriodStart.Add(time.Hour), 10)

	grants, _, err := s.grantService.EnsureGrantsForSubscriptions(s.GetContext(),
		s.customerOf(fx.sub), []*subscription.Subscription{fx.sub}, fx.sub.CurrentPeriodStart.Add(2*time.Hour))
	s.Require().NoError(err)
	s.Require().Len(grants, 2)
	configs := lo.Map(grants, func(g *entitlementgrant.EntitlementGrant, _ int) string { return g.EntitlementConfigID })
	s.ElementsMatch([]string{"ent-subgrant-par", monthly.ID}, configs)
}

func (s *EntitlementGrantSuite) customerOf(sub *subscription.Subscription) *customer.Customer {
	c, err := s.GetStores().CustomerRepo.Get(s.GetContext(), sub.CustomerID)
	s.Require().NoError(err)
	return c
}

func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrant_AdditiveGrantsPoolIntoOneWindow() {
	fx := s.newSubGrantFixture("add", dailySubscriptionEntitlementGrant(10, types.EntitlementGrantAllocationBehaviorFirstUsage))
	topUp := dailySubscriptionEntitlementGrant(5, types.EntitlementGrantAllocationBehaviorFirstUsage)
	topUp.ID = "ent-subgrant-add-topup"
	topUp.EntityType = types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION
	topUp.EntityID = fx.sub.ID
	topUp.GrantScope = types.EntitlementGrantScopeSubscription
	topUp.GrantMeasure = types.EntitlementGrantMeasureAmount
	topUp.IsEnabled = true
	topUp.BaseModel = types.GetDefaultBaseModel(s.GetContext())
	_, err := s.GetStores().EntitlementRepo.Create(s.GetContext(), topUp)
	s.Require().NoError(err)
	s.seedMeterUsage(fx.extID, fx.callsMeter, fx.sub.CurrentPeriodStart.Add(time.Hour), 10)

	grants, _, err := s.grantService.EnsureGrantsForSubscriptions(s.GetContext(),
		s.customerOf(fx.sub), []*subscription.Subscription{fx.sub}, fx.sub.CurrentPeriodStart.Add(2*time.Hour))
	s.Require().NoError(err)
	s.Require().Len(grants, 1, "additive grants share one window")
	s.True(grants[0].Quota.Equal(decimal.NewFromInt(15)), "pooled quota $10 + $5, got %s", grants[0].Quota)
}

// A grant past its end_date opens no new window, and the last one stops at the end_date.
func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrant_EndDateStopsNewWindows() {
	ec := dailySubscriptionEntitlementGrant(10, types.EntitlementGrantAllocationBehaviorFirstUsage)
	fx := s.newSubGrantFixture("ended", ec)
	end := fx.sub.CurrentPeriodStart.Add(2 * time.Hour)
	ec.EndDate = &end
	_, err := s.GetStores().EntitlementRepo.Update(s.GetContext(), ec)
	s.Require().NoError(err)

	s.seedMeterUsage(fx.extID, fx.callsMeter, fx.sub.CurrentPeriodStart.Add(time.Hour), 10)
	s.seedMeterUsage(fx.extID, fx.callsMeter, fx.sub.CurrentPeriodStart.Add(30*time.Hour), 10)

	for _, at := range []time.Time{fx.sub.CurrentPeriodStart.Add(90 * time.Minute), fx.sub.CurrentPeriodStart.Add(31 * time.Hour)} {
		_, _, err := s.grantService.EnsureGrantsForSubscriptions(s.GetContext(), s.customerOf(fx.sub), []*subscription.Subscription{fx.sub}, at)
		s.Require().NoError(err)
	}

	rows, err := s.GetStores().EntitlementGrantRepo.List(s.GetContext(), types.NewNoLimitEntitlementGrantFilter().
		WithSubscriptionIDs(fx.sub.ID).WithScopeEntityType(types.EntitlementGrantScopeSubscription))
	s.Require().NoError(err)
	s.Require().Len(rows, 1, "usage after end_date opens nothing")
	s.False(rows[0].ValidTo.After(end), "the window stops at end_date, got %s", rows[0].ValidTo)
}
