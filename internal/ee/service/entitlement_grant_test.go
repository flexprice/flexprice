package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// -----------------------------------------------------------------------------
// EntitlementGrantSuite bundles fixtures for grant-shape validation, window
// math, and EnsureGrants. Split from EntitlementServiceSuite so the boilerplate
// doesn't crowd the existing entitlement tests.
// -----------------------------------------------------------------------------

type EntitlementGrantSuite struct {
	testutil.BaseServiceTestSuite

	meterStore   *testutil.InMemoryMeterStore
	entService   EntitlementService
	grantService EntitlementGrantService
	alertService AlertService
}

func TestEntitlementGrantSuite(t *testing.T) {
	suite.Run(t, new(EntitlementGrantSuite))
}

func (s *EntitlementGrantSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.meterStore = testutil.NewInMemoryMeterStore()

	params := s.buildServiceParams()
	s.entService = NewEntitlementService(params)
	s.grantService = NewEntitlementGrantService(params)
	s.alertService = NewAlertService(params)
}

func (s *EntitlementGrantSuite) buildServiceParams() ServiceParams {
	stores := s.GetStores()
	return ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		EntitlementRepo:          stores.EntitlementRepo,
		EntitlementGrantRepo:     stores.EntitlementGrantRepo,
		PlanRepo:                 stores.PlanRepo,
		FeatureRepo:              stores.FeatureRepo,
		MeterRepo:                s.meterStore,
		PriceRepo:                stores.PriceRepo,
		CustomerRepo:             stores.CustomerRepo,
		SubRepo:                  stores.SubscriptionRepo,
		SubscriptionLineItemRepo: stores.SubscriptionLineItemRepo,
		AlertRepo:                stores.AlertRepo,
		AlertLogsRepo:            stores.AlertLogsRepo,
		WalletRepo:               stores.WalletRepo,
		SettingsRepo:             stores.SettingsRepo,
		AddonRepo:                stores.AddonRepo,
		AddonAssociationRepo:     stores.AddonAssociationRepo,
		MeterUsageRepo:           stores.MeterUsageRepo,
		WebhookPublisher:         s.GetWebhookPublisher(),
	}
}

// -----------------------------------------------------------------------------
// M2 · Shape validation at EC-write time
// -----------------------------------------------------------------------------

func (s *EntitlementGrantSuite) TestCreateEntitlement_RejectsGrantOnMaxMeter() {
	// A grant on a MAX meter would have no meaningful per-window quota (MAX
	// tracks a peak, not additive usage) — validation must catch it before
	// the row ever lands.
	maxMeter := &meter.Meter{
		ID:        "meter-max",
		Name:      "Max Meter",
		EventName: "peak_concurrent",
		Aggregation: meter.Aggregation{
			Type: types.AggregationMax,
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.meterStore.CreateMeter(s.GetContext(), maxMeter))

	f := &feature.Feature{
		ID:        "feat-max",
		Name:      "Peak concurrent",
		Type:      types.FeatureTypeMetered,
		MeterID:   maxMeter.ID,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(s.GetContext(), f))

	p := &plan.Plan{ID: "plan-max", Name: "P", BaseModel: types.GetDefaultBaseModel(s.GetContext())}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), p))

	_, err := s.entService.CreateEntitlement(s.GetContext(), s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(10)))
	s.Error(err)
	s.Contains(err.Error(), "MAX")
}

func (s *EntitlementGrantSuite) TestCreateEntitlement_RejectsGrantOnBucketedMeter() {
	// Bucketed meters aggregate per bucket — a grant window would slice
	// across buckets ambiguously.
	bucketedMeter := &meter.Meter{
		ID:        "meter-bucketed",
		Name:      "Bucketed SUM",
		EventName: "requests",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			BucketSize: types.WindowSizeHour,
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.meterStore.CreateMeter(s.GetContext(), bucketedMeter))

	f := &feature.Feature{
		ID:        "feat-bucketed",
		Type:      types.FeatureTypeMetered,
		MeterID:   bucketedMeter.ID,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(s.GetContext(), f))
	p := &plan.Plan{ID: "plan-bucketed", BaseModel: types.GetDefaultBaseModel(s.GetContext())}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), p))

	_, err := s.entService.CreateEntitlement(s.GetContext(), s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(10)))
	s.Error(err)
	s.Contains(err.Error(), "bucketed")
}

func (s *EntitlementGrantSuite) TestCreateEntitlement_RejectsAmountLaneOnTieredPrice() {
	// Amount grants need per-event pricing to compose cleanly across the
	// grant window. Tiered / graduated pricing is stateful over the cycle
	// and doesn't compose per-grant; reject at create.
	m := s.simpleMeter("meter-tier")
	f := s.simpleFeature("feat-tier", m.ID)
	p := &plan.Plan{ID: "plan-tier", BaseModel: types.GetDefaultBaseModel(s.GetContext())}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), p))

	tiered := &price.Price{
		ID:           "price-tier",
		Amount:       decimal.NewFromFloat(0.5),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_VOLUME,
		MeterID:      m.ID,
		BaseModel:    types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), tiered))

	_, err := s.entService.CreateEntitlement(s.GetContext(), s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureAmount, 5, decimal.NewFromInt(10)))
	s.Error(err)
	s.Contains(err.Error(), "tiered")
}

func (s *EntitlementGrantSuite) TestCreateEntitlement_AcceptsFlatPriceAmountLane() {
	// Same feature/meter, flat pricing — must succeed. Confirms the guard
	// isn't over-broad.
	m := s.simpleMeter("meter-flat")
	f := s.simpleFeature("feat-flat", m.ID)
	p := &plan.Plan{ID: "plan-flat", BaseModel: types.GetDefaultBaseModel(s.GetContext())}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), p))
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), &price.Price{
		ID:           "price-flat",
		Amount:       decimal.NewFromFloat(0.01),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
		MeterID:      m.ID,
		BaseModel:    types.GetDefaultBaseModel(s.GetContext()),
	}))

	resp, err := s.entService.CreateEntitlement(s.GetContext(), s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureAmount, 5, decimal.NewFromInt(10)))
	s.NoError(err)
	s.True(resp.Entitlement.HasGrantConfig())
	s.Equal(types.EntitlementGrantMeasureAmount, resp.Entitlement.GrantMeasure)
}

func (s *EntitlementGrantSuite) TestCreateEntitlement_NoneStillWorks() {
	// Legacy entitlement path (no grant fields) is unchanged.
	m := s.simpleMeter("meter-legacy")
	f := s.simpleFeature("feat-legacy", m.ID)
	p := &plan.Plan{ID: "plan-legacy", BaseModel: types.GetDefaultBaseModel(s.GetContext())}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), p))

	usageLimit := int64(1000)
	req := dto.CreateEntitlementRequest{
		FeatureID:   f.ID,
		FeatureType: types.FeatureTypeMetered,
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    p.ID,
		IsEnabled:   true,
		UsageLimit:  &usageLimit,
	}
	resp, err := s.entService.CreateEntitlement(s.GetContext(), req)
	s.NoError(err)
	s.False(resp.Entitlement.HasGrantConfig())
}

// -----------------------------------------------------------------------------
// M3 · EnsureGrants
// -----------------------------------------------------------------------------

func (s *EntitlementGrantSuite) TestEnsureGrants_OpensGrantAnchoredAtFirstUsage() {
	f, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)

	eventAt := sub.CurrentPeriodStart.Add(20 * time.Minute)
	s.seedMeterUsage(cust.ExternalID, "meter-quantity", eventAt, 1)

	at := sub.CurrentPeriodStart.Add(30 * time.Minute)
	grants, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, at)
	s.NoError(err)
	s.Require().Len(grants, 1, "one grant EC with usage should open exactly one grant")

	g := grants[0]
	s.Equal(cust.ID, g.CustomerID)
	s.Equal(sub.ID, g.SubscriptionID)
	s.Equal(types.EntitlementGrantScopeFeature, g.ScopeEntityType)
	s.Equal(f.ID, g.ScopeEntityID)
	s.Equal(types.EntitlementGrantStatusActive, g.GrantStatus)
	s.True(g.ValidFrom.Equal(eventAt), "window must anchor at the first usage event")
	s.True(g.ValidTo.Equal(eventAt.Add(5 * time.Hour)))
	s.NotNil(meta)
}

func (s *EntitlementGrantSuite) TestEnsureGrants_NoUncoveredUsage_NoGrant() {
	// No usage on the feature's meter → no grant rows at all (lazy opening).
	_, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)

	grants, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, sub.CurrentPeriodStart.Add(30*time.Minute))
	s.NoError(err)
	s.Empty(grants, "idle features must not open grants")
	s.Nil(meta)
}

func (s *EntitlementGrantSuite) TestEnsureGrants_ReturnsExistingLiveGrantUnchanged() {
	// Second call at the same tick must not duplicate — partial unique index
	// on the slot + explicit "already live" bypass in the service.
	_, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)
	s.seedMeterUsage(cust.ExternalID, "meter-quantity", sub.CurrentPeriodStart.Add(20*time.Minute), 1)

	at := sub.CurrentPeriodStart.Add(30 * time.Minute)
	first, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, at)
	s.NoError(err)
	s.Len(first, 1)

	second, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, at.Add(5*time.Minute))
	s.NoError(err)
	s.Len(second, 1)
	s.Equal(first[0].ID, second[0].ID, "second EnsureGrants should return the same live grant")
	s.NotNil(meta)
}

func (s *EntitlementGrantSuite) TestEnsureGrants_IgnoresNoneEC() {
	// A vanilla (non-grant) EC on the same subscription must NOT produce a
	// grant row. Guards against regressing legacy entitlements.
	m := s.simpleMeter("meter-legacy-ec")
	f := s.simpleFeature("feat-legacy-ec", m.ID)
	plan := s.simplePlan("plan-legacy-ec")
	usageLimit := int64(999)
	_, err := s.entService.CreateEntitlement(s.GetContext(), dto.CreateEntitlementRequest{
		FeatureID:   f.ID,
		FeatureType: types.FeatureTypeMetered,
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    plan.ID,
		IsEnabled:   true,
		UsageLimit:  &usageLimit,
	})
	s.NoError(err)

	cust := s.simpleCustomer("cust-legacy-ec")
	sub := s.simpleSubscription("sub-legacy-ec", cust.ID, plan.ID)

	grants, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, sub.CurrentPeriodStart.Add(10*time.Minute))
	s.NoError(err)
	s.Empty(grants, "type=none EC should not open a grant")
	s.Nil(meta)
}

func (s *EntitlementGrantSuite) TestEnsureGrants_DurationAtOrOverCycle_NoGrant() {
	// A grant spanning the whole cycle is just the cycle quota — that's what
	// legacy usage_limit + usage_reset_period expresses. No grant rows opened.
	m := s.simpleMeter("meter-cyclelen")
	f := s.simpleFeature("feat-cyclelen", m.ID)
	p := s.simplePlan("plan-cyclelen")
	// simpleSubscription runs a 30-day cycle; 30 days of hours == cycle length.
	_, err := s.entService.CreateEntitlement(s.GetContext(),
		s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 24*30, decimal.NewFromInt(100)))
	s.Require().NoError(err)

	cust := s.simpleCustomer("cust-cyclelen")
	sub := s.simpleSubscription("sub-cyclelen", cust.ID, p.ID)

	grants, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, sub.CurrentPeriodStart.Add(10*time.Minute))
	s.Require().NoError(err)
	s.Empty(grants, "duration >= cycle length must not open a grant")
	s.Nil(meta)
}

func (s *EntitlementGrantSuite) TestEnsureGrants_SkipsWhenCustomerHasNoSubs() {
	cust := s.simpleCustomer("cust-no-sub")
	grants, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, time.Now())
	s.NoError(err)
	s.Empty(grants)
	s.Nil(meta)
}

// -----------------------------------------------------------------------------
// computeGrantWindow — usage-anchored window math
// -----------------------------------------------------------------------------

// windowFixture is the row set the usage-anchored window math reads: a real
// meter + feature + customer (for external-ID resolution) on a 30-day cycle.
type windowFixture struct {
	sub                  *subscription.Subscription
	ec                   *entitlement.Entitlement
	meterID, extID       string
	cycleStart, cycleEnd time.Time
}

func (s *EntitlementGrantSuite) newWindowFixture(tag string, durHours int) windowFixture {
	cycleStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	cycleEnd := cycleStart.Add(30 * 24 * time.Hour)
	m := s.simpleMeter("meter-" + tag)
	f := s.simpleFeature("feat-"+tag, m.ID)
	cust := s.simpleCustomer("cust-" + tag)
	sub := &subscription.Subscription{
		ID:                 "sub-" + tag,
		CustomerID:         cust.ID,
		CurrentPeriodStart: cycleStart,
		CurrentPeriodEnd:   cycleEnd,
	}
	ec := s.newTimeBoxedEC("ec-"+tag, f.ID, durHours, types.EntitlementGrantDurationUnitHour, decimal.NewFromInt(100))
	return windowFixture{sub: sub, ec: ec, meterID: m.ID, extID: cust.ExternalID, cycleStart: cycleStart, cycleEnd: cycleEnd}
}

func (s *EntitlementGrantSuite) seedWindowPrevGrant(fx windowFixture, id string, validFrom, validTo time.Time) {
	prev := &entitlementgrant.EntitlementGrant{
		ID:                  id,
		EntitlementConfigID: fx.ec.ID,
		CustomerID:          fx.sub.CustomerID,
		SubscriptionID:      fx.sub.ID,
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       fx.ec.FeatureID,
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(100),
		ValidFrom:           validFrom,
		ValidTo:             validTo,
		GrantStatus:         types.EntitlementGrantStatusActive,
		LastComputedAt:      &validTo, // already finalized
		EnvironmentID:       types.GetEnvironmentID(s.GetContext()),
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err := s.GetStores().EntitlementGrantRepo.Create(s.GetContext(), prev)
	s.Require().NoError(err)
}

// windowArgs resolves the inputs computeGrantWindow receives from the batched
// EnsureGrants pass: the shared meta and the slot's latest window end.
func (s *EntitlementGrantSuite) windowArgs(fx windowFixture) (*grantEvalMeta, time.Time) {
	svc := s.grantService.(*entitlementGrantService)
	meta, err := svc.buildGrantEvalMeta(s.GetContext(),
		[]*subscription.Subscription{fx.sub},
		map[string][]*entitlement.Entitlement{fx.sub.ID: {fx.ec}},
		nil,
		NewSubscriptionService(svc.ServiceParams))
	s.Require().NoError(err)
	prev, err := s.GetStores().EntitlementGrantRepo.FindLastBySlot(s.GetContext(), fx.ec.ID, fx.sub.CustomerID, fx.sub.ID)
	s.Require().NoError(err)
	var lastEnd time.Time
	if prev != nil {
		lastEnd = prev.ValidTo
	}
	return meta, lastEnd
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_AnchorsAtExactEventTime() {
	// The 2:00/2:07 case: event at T, evaluation at T+7m → window starts
	// exactly at T, not at a delay-derived approximation.
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("exact", 5)
	eventAt := fx.cycleStart.Add(2 * time.Hour)
	at := eventAt.Add(7 * time.Minute)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), fx.ec, fx.sub, meta, last, at, 5*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(eventAt), "window must anchor at the first uncovered event: got %s want %s", from, eventAt)
	s.True(to.Equal(eventAt.Add(5 * time.Hour)))
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_NoUncoveredUsage_NoGrant() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("idle", 5)

	meta, last := s.windowArgs(fx)
	_, _, ok, err := svc.computeGrantWindow(s.GetContext(), fx.ec, fx.sub, meta, last, fx.cycleStart.Add(2*time.Hour), 5*time.Hour)
	s.NoError(err)
	s.False(ok, "no uncovered usage must open no grant")
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_IdleGapHop() {
	// Previous window ended at P; no usage for 35 minutes; usage resumes at
	// P+35m → the next window anchors at P+35m, not P. Idle time opens nothing.
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("gap", 5)
	prevEnd := fx.cycleStart.Add(10 * time.Hour)
	s.seedWindowPrevGrant(fx, "eg-gap-prev", prevEnd.Add(-5*time.Hour), prevEnd)

	resumeAt := prevEnd.Add(35 * time.Minute)
	s.seedMeterUsage(fx.extID, fx.meterID, resumeAt, 1)

	meta, last := s.windowArgs(fx)
	from, _, ok, err := svc.computeGrantWindow(s.GetContext(), fx.ec, fx.sub, meta, last, resumeAt.Add(5*time.Minute), 5*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(resumeAt), "window must hop the idle gap to the first uncovered event: got %s want %s", from, resumeAt)
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_CycleBoundaryCap() {
	// A 24h duration anchored 6h before cycle_end: the full duration doesn't
	// fit, so the window becomes the cycle's last 24h — backdated start (safe:
	// [frontier, anchor) is event-free), capped end.
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("cap", 24)
	eventAt := fx.cycleEnd.Add(-6 * time.Hour)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), fx.ec, fx.sub, meta, last, eventAt.Add(10*time.Minute), 24*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(fx.cycleEnd.Add(-24*time.Hour)), "start must backdate to cycle_end-24h: got %s", from)
	s.True(to.Equal(fx.cycleEnd), "valid_to must cap at cycle_end")
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_TrailingStubAbsorbed() {
	// A window whose remainder to cycle_end would be sub-minimum stretches to
	// cycle_end — a stub can't stand alone and skipping it would orphan events.
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("stub", 5)
	prevEnd := fx.cycleEnd.Add(-5*time.Hour - 30*time.Minute)
	s.seedWindowPrevGrant(fx, "eg-stub-prev", prevEnd.Add(-5*time.Hour), prevEnd)

	eventAt := prevEnd.Add(10 * time.Minute) // 5h window from here would leave a 20m stub
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), fx.ec, fx.sub, meta, last, eventAt.Add(10*time.Minute), 5*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(eventAt))
	s.True(to.Equal(fx.cycleEnd), "final window must absorb the sub-minimum stub: got %s want %s", to, fx.cycleEnd)
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_ForcedTailBackdatedToFullDuration() {
	// First uncovered event inside the cycle's final stretch: the window
	// backdates its start to cycle_end − duration, covering the anchoring
	// event with a full-length window ending at cycle_end.
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("tail", 5)
	eventAt := fx.cycleEnd.Add(-30 * time.Minute)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), fx.ec, fx.sub, meta, last, fx.cycleEnd.Add(-10*time.Minute), 5*time.Hour)
	s.NoError(err)
	s.True(ok, "tail window must open, backdated to the full duration")
	s.True(from.Equal(fx.cycleEnd.Add(-5*time.Hour)), "start must backdate to cycle_end-duration: got %s", from)
	s.True(to.Equal(fx.cycleEnd))
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_FlushWithCycleEnd_NothingToOpen() {
	// Previous window already ends at cycle_end and the cycle hasn't rolled:
	// there is nothing left to cover, so no grant opens until rollover.
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("flush", 5)
	s.seedWindowPrevGrant(fx, "eg-flush-prev", fx.cycleEnd.Add(-5*time.Hour), fx.cycleEnd)

	meta, last := s.windowArgs(fx)
	_, _, ok, err := svc.computeGrantWindow(s.GetContext(), fx.ec, fx.sub, meta, last, fx.cycleEnd.Add(-10*time.Minute), 5*time.Hour)
	s.NoError(err)
	s.False(ok)
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_PrevCycleHistory_AnchorsAtFirstNewCycleUsage() {
	// A slot whose last grant ended in an earlier cycle anchors at the first
	// usage of the NEW cycle — coverage continues across rollover without
	// opening windows over idle time.
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("roll", 5)
	s.seedWindowPrevGrant(fx, "eg-roll-prev", fx.cycleStart.Add(-6*time.Hour), fx.cycleStart.Add(-1*time.Hour))

	firstNewCycleEvent := fx.cycleStart.Add(40 * time.Minute)
	s.seedMeterUsage(fx.extID, fx.meterID, firstNewCycleEvent, 1)

	meta, last := s.windowArgs(fx)
	from, _, ok, err := svc.computeGrantWindow(s.GetContext(), fx.ec, fx.sub, meta, last, fx.cycleStart.Add(3*time.Hour), 5*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(firstNewCycleEvent),
		"prior-cycle history must anchor at the new cycle's first usage: got %s", from)
}

// -----------------------------------------------------------------------------
// Helpers.
// -----------------------------------------------------------------------------

func (s *EntitlementGrantSuite) grantCreateRequest(
	featureID, planID string,
	measure types.EntitlementGrantMeasure,
	durationValue int,
	quota decimal.Decimal,
) dto.CreateEntitlementRequest {
	return dto.CreateEntitlementRequest{
		FeatureID:          featureID,
		FeatureType:        types.FeatureTypeMetered,
		EntityType:         types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:           planID,
		IsEnabled:          true,
		GrantMeasure:       measure,
		GrantDurationValue: lo.ToPtr(durationValue),
		GrantDurationUnit:  types.EntitlementGrantDurationUnitHour,
		GrantQuota:         &quota,
	}
}

func (s *EntitlementGrantSuite) simpleMeter(id string) *meter.Meter {
	m := &meter.Meter{
		ID:        id,
		Name:      id,
		EventName: id,
		Aggregation: meter.Aggregation{
			Type: types.AggregationSum,
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.meterStore.CreateMeter(s.GetContext(), m))
	return m
}

func (s *EntitlementGrantSuite) simpleFeature(id, meterID string) *feature.Feature {
	f := &feature.Feature{
		ID:        id,
		Name:      id,
		Type:      types.FeatureTypeMetered,
		MeterID:   meterID,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(s.GetContext(), f))
	return f
}

func (s *EntitlementGrantSuite) simplePlan(id string) *plan.Plan {
	p := &plan.Plan{ID: id, Name: id, BaseModel: types.GetDefaultBaseModel(s.GetContext())}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), p))
	return p
}

func (s *EntitlementGrantSuite) simpleCustomer(id string) *customer.Customer {
	c := &customer.Customer{ID: id, ExternalID: id, BaseModel: types.GetDefaultBaseModel(s.GetContext())}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), c))
	return c
}

func (s *EntitlementGrantSuite) simpleSubscription(id, customerID, planID string) *subscription.Subscription {
	now := time.Now().UTC().Truncate(time.Second)
	sub := &subscription.Subscription{
		ID:                 id,
		CustomerID:         customerID,
		PlanID:             planID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		StartDate:          now,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.Add(30 * 24 * time.Hour),
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))
	return sub
}

func (s *EntitlementGrantSuite) newTimeBoxedEC(id, featureID string, durationValue int, unit types.EntitlementGrantDurationUnit, quota decimal.Decimal) *entitlement.Entitlement {
	return &entitlement.Entitlement{
		ID:                 id,
		FeatureID:          featureID,
		FeatureType:        types.FeatureTypeMetered,
		IsEnabled:          true,
		GrantMeasure:       types.EntitlementGrantMeasureQuantity,
		GrantDurationValue: &durationValue,
		GrantDurationUnit:  unit,
		GrantQuota:         &quota,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
}

// setupCustomerSubWithGrantEC creates: a metered feature + flat price + plan +
// a time_boxed grant EC on that plan/feature, plus a customer with an active
// subscription to that plan. Returns the trio the tests care about.
func (s *EntitlementGrantSuite) setupCustomerSubWithGrantEC(
	measure types.EntitlementGrantMeasure,
) (*feature.Feature, *subscription.Subscription, *customer.Customer) {
	m := s.simpleMeter("meter-" + string(measure))
	f := s.simpleFeature("feat-"+string(measure), m.ID)
	p := s.simplePlan("plan-" + string(measure))
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), &price.Price{
		ID:           "price-" + string(measure),
		Amount:       decimal.NewFromFloat(0.02),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
		MeterID:      m.ID,
		BaseModel:    types.GetDefaultBaseModel(s.GetContext()),
	}))

	_, err := s.entService.CreateEntitlement(s.GetContext(), s.grantCreateRequest(f.ID, p.ID, measure, 5, decimal.NewFromInt(100)))
	s.NoError(err)

	cust := s.simpleCustomer("cust-" + string(measure))
	sub := s.simpleSubscription("sub-"+string(measure), cust.ID, p.ID)
	return f, sub, cust
}

// -----------------------------------------------------------------------------
// EnsureGrants · slot lifecycle
// -----------------------------------------------------------------------------

func (s *EntitlementGrantSuite) TestEnsureGrants_ClosedSlotReopensAndFinalizesOld() {
	f, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)

	// Find the EC opened by setup so the closed grant lands on its slot.
	ecs, err := s.GetStores().EntitlementRepo.List(s.GetContext(), types.NewNoLimitEntitlementFilter())
	s.Require().NoError(err)
	s.Require().Len(ecs, 1)
	ecID := ecs[0].ID

	at := sub.CurrentPeriodStart.Add(10 * time.Hour)
	closedTo := at.Add(-1 * time.Hour) // window closed an hour ago
	closed := &entitlementgrant.EntitlementGrant{
		ID:                  "eg-closed",
		EntitlementConfigID: ecID,
		CustomerID:          cust.ID,
		SubscriptionID:      sub.ID,
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       f.ID,
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(100),
		ValidFrom:           sub.CurrentPeriodStart,
		ValidTo:             closedTo,
		GrantStatus:         types.EntitlementGrantStatusActive,
		EnvironmentID:       types.GetEnvironmentID(s.GetContext()),
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementGrantRepo.Create(s.GetContext(), closed)
	s.Require().NoError(err)

	// Usage after the closed window is what triggers the fresh open.
	s.seedMeterUsage(cust.ExternalID, "meter-quantity", at.Add(-30*time.Minute), 1)

	grants, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 2, "fresh grant plus the closed one needing its final refresh")
	s.NotNil(meta)

	fresh, found := lo.Find(grants, func(g *entitlementgrant.EntitlementGrant) bool { return g.ID != "eg-closed" })
	s.Require().True(found, "a fresh grant must open on the freed slot")
	s.True(fresh.ValidTo.After(at))

	// The closed grant is returned (last_computed_at predates valid_to) so the
	// evaluator gives it one final usage refresh; no status write happens.
	_, found = lo.Find(grants, func(g *entitlementgrant.EntitlementGrant) bool { return g.ID == "eg-closed" })
	s.Require().True(found, "the closed grant must be returned for its final refresh")

	// Once the snapshot covers the full window, the grant drops out of the set.
	closed.LastComputedAt = &at
	s.Require().NoError(s.GetStores().EntitlementGrantRepo.UpdateSnapshot(s.GetContext(), closed))
	again, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, at.Add(time.Minute))
	s.Require().NoError(err)
	s.Require().Len(again, 1, "finalized grants must not re-enter the set")
	s.Equal(fresh.ID, again[0].ID)
	s.NotNil(meta)
}

func (s *EntitlementGrantSuite) TestOpenOneGrant_LostRaceReReadsWinner() {
	// valid_from is deterministic, so a racer that read the slot before the
	// winner inserted computes the same window, collides on the unique
	// (slot, valid_from) index, and must return the winner's row.
	_, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)
	ecs, err := s.GetStores().EntitlementRepo.List(s.GetContext(), types.NewNoLimitEntitlementFilter())
	s.Require().NoError(err)
	s.Require().Len(ecs, 1)

	eventAt := sub.CurrentPeriodStart.Add(20 * time.Minute)
	s.seedMeterUsage(cust.ExternalID, "meter-quantity", eventAt, 1)

	at := eventAt.Add(10 * time.Minute)
	winners, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, at)
	s.Require().NoError(err)
	s.Require().Len(winners, 1)
	s.NotNil(meta)

	// Racer: stale read said the slot was empty (last=nil).
	svc := s.grantService.(*entitlementGrantService)
	meta, err = svc.buildGrantEvalMeta(s.GetContext(),
		[]*subscription.Subscription{sub},
		map[string][]*entitlement.Entitlement{sub.ID: {ecs[0]}},
		nil, NewSubscriptionService(svc.ServiceParams))
	s.Require().NoError(err)

	got, err := svc.openOneGrant(s.GetContext(), sub, ecs[0], time.Time{}, meta, at, decimal.NewFromInt(100))
	s.Require().NoError(err)
	s.Require().NotNil(got)
	s.Equal(winners[0].ID, got.ID, "loser must re-read the winner, not insert a duplicate")
}

func (s *EntitlementGrantSuite) TestEnsureGrants_ParallelECs_OneGrantEach() {
	// Two time-boxed ECs on the same feature (parallel mode): each EC is its
	// own slot, so EnsureGrants must open two independent grants.
	m := s.simpleMeter("meter-par")
	f := s.simpleFeature("feat-par", m.ID)
	p := s.simplePlan("plan-par")

	for i, quota := range []int64{100, 200} {
		req := s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(quota))
		req.AggregationMode = types.EntitlementAggregationModeParallel
		_, err := s.entService.CreateEntitlement(s.GetContext(), req)
		s.Require().NoError(err, "creating parallel EC %d", i)
	}

	cust := s.simpleCustomer("cust-par")
	sub := s.simpleSubscription("sub-par", cust.ID, p.ID)
	s.seedMeterUsage(cust.ExternalID, m.ID, sub.CurrentPeriodStart.Add(5*time.Minute), 1)

	grants, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, sub.CurrentPeriodStart.Add(10*time.Minute))
	s.Require().NoError(err)
	s.Require().Len(grants, 2, "each parallel EC opens its own grant")
	s.NotEqual(grants[0].EntitlementConfigID, grants[1].EntitlementConfigID)
	quotas := []int64{grants[0].Quota.IntPart(), grants[1].Quota.IntPart()}
	s.ElementsMatch([]int64{100, 200}, quotas)
	s.NotNil(meta)
}

func (s *EntitlementGrantSuite) TestEnsureGrants_AdditiveECs_OneSummedGrant() {
	// Two additive ECs on the same feature merge into ONE grant with the summed
	// quota, opened on the lowest-ID EC's slot — downstream evaluation, alerts,
	// and billing then treat the group as a single pool.
	m := s.simpleMeter("meter-add")
	f := s.simpleFeature("feat-add", m.ID)
	p := s.simplePlan("plan-add")

	for i, quota := range []int64{100, 200} {
		req := s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(quota))
		_, err := s.entService.CreateEntitlement(s.GetContext(), req)
		s.Require().NoError(err, "creating additive EC %d", i)
	}

	cust := s.simpleCustomer("cust-add")
	sub := s.simpleSubscription("sub-add", cust.ID, p.ID)
	s.seedMeterUsage(cust.ExternalID, m.ID, sub.CurrentPeriodStart.Add(5*time.Minute), 1)

	grants, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, sub.CurrentPeriodStart.Add(10*time.Minute))
	s.Require().NoError(err)
	s.Require().Len(grants, 1, "additive group must open a single summed grant")
	s.True(grants[0].Quota.Equal(decimal.NewFromInt(300)),
		"summed quota expected 300, got %s", grants[0].Quota)
	s.NotNil(meta)

	// Idempotency across the group: second call returns the same single grant.
	again, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, sub.CurrentPeriodStart.Add(20*time.Minute))
	s.Require().NoError(err)
	s.Require().Len(again, 1)
	s.Equal(grants[0].ID, again[0].ID)
	s.NotNil(meta)
}

func (s *EntitlementGrantSuite) TestCreateEntitlement_RejectsMixedModesOnFeature() {
	// One mode per feature: an additive EC exists → creating a parallel EC on
	// the same feature must fail.
	m := s.simpleMeter("meter-mix")
	f := s.simpleFeature("feat-mix", m.ID)
	p := s.simplePlan("plan-mix")

	_, err := s.entService.CreateEntitlement(s.GetContext(), s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(100)))
	s.Require().NoError(err)

	req := s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(50))
	req.AggregationMode = types.EntitlementAggregationModeParallel
	_, err = s.entService.CreateEntitlement(s.GetContext(), req)
	s.Error(err)
	s.Contains(err.Error(), "aggregation_mode")
}

func (s *EntitlementGrantSuite) TestCreateEntitlement_RejectsMeasureMismatchOnFeature() {
	// One measure per feature: billing folds per feature and can't mix
	// quantity and amount lanes.
	m := s.simpleMeter("meter-measuremix")
	f := s.simpleFeature("feat-measuremix", m.ID)
	p := s.simplePlan("plan-measuremix")
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), &price.Price{
		ID:           "price-measuremix",
		Amount:       decimal.NewFromFloat(0.01),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
		MeterID:      m.ID,
		BaseModel:    types.GetDefaultBaseModel(s.GetContext()),
	}))

	_, err := s.entService.CreateEntitlement(s.GetContext(),
		s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(100)))
	s.Require().NoError(err)

	_, err = s.entService.CreateEntitlement(s.GetContext(),
		s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureAmount, 5, decimal.NewFromInt(50)))
	s.Error(err)
	s.Contains(err.Error(), "grant_measure")
}

func (s *EntitlementGrantSuite) TestCreateEntitlement_RejectsAdditiveDurationMismatch() {
	// Additive quotas sum into one window, so durations must match.
	m := s.simpleMeter("meter-durmix")
	f := s.simpleFeature("feat-durmix", m.ID)
	p := s.simplePlan("plan-durmix")

	_, err := s.entService.CreateEntitlement(s.GetContext(), s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(100)))
	s.Require().NoError(err)

	_, err = s.entService.CreateEntitlement(s.GetContext(), s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 10, decimal.NewFromInt(50)))
	s.Error(err)
	s.Contains(err.Error(), "grant_duration")
}

func (s *EntitlementGrantSuite) TestEnsureGrants_IdleGapHop() {
	// Previous window ended, then 35 minutes of silence, then usage resumed —
	// the next window opens at the resume timestamp, not at the previous
	// valid_to, so idle time never occupies window budget.
	f, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)

	ecs, err := s.GetStores().EntitlementRepo.List(s.GetContext(), types.NewNoLimitEntitlementFilter())
	s.Require().NoError(err)
	s.Require().Len(ecs, 1)

	prevEnd := sub.CurrentPeriodStart.Add(5 * time.Hour)
	prev := &entitlementgrant.EntitlementGrant{
		ID:                  "eg-prev",
		EntitlementConfigID: ecs[0].ID,
		CustomerID:          cust.ID,
		SubscriptionID:      sub.ID,
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       f.ID,
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(100),
		ValidFrom:           prevEnd.Add(-5 * time.Hour),
		ValidTo:             prevEnd,
		GrantStatus:         types.EntitlementGrantStatusActive,
		LastComputedAt:      &prevEnd, // already finalized
		EnvironmentID:       types.GetEnvironmentID(s.GetContext()),
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementGrantRepo.Create(s.GetContext(), prev)
	s.Require().NoError(err)

	resumeAt := prevEnd.Add(35 * time.Minute)
	s.seedMeterUsage(cust.ExternalID, "meter-quantity", resumeAt, 1)

	grants, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, resumeAt.Add(5*time.Minute))
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	s.True(grants[0].ValidFrom.Equal(resumeAt),
		"new window must anchor at the first uncovered event: got %s want %s", grants[0].ValidFrom, resumeAt)
	s.NotNil(meta)
}

func (s *EntitlementGrantSuite) TestEnsureGrants_DelayedEvaluationCoversGapUsage() {
	// Evaluation was down for an hour; usage during the outage anchors the
	// next window exactly, so nothing is lost even under delayed execution.
	f, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)

	ecs, err := s.GetStores().EntitlementRepo.List(s.GetContext(), types.NewNoLimitEntitlementFilter())
	s.Require().NoError(err)
	s.Require().Len(ecs, 1)

	at := sub.CurrentPeriodStart.Add(10 * time.Hour)
	prevEnd := at.Add(-1 * time.Hour) // window closed an hour before evaluation caught up
	prev := &entitlementgrant.EntitlementGrant{
		ID:                  "eg-prev-old",
		EntitlementConfigID: ecs[0].ID,
		CustomerID:          cust.ID,
		SubscriptionID:      sub.ID,
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       f.ID,
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(100),
		ValidFrom:           prevEnd.Add(-5 * time.Hour),
		ValidTo:             prevEnd,
		GrantStatus:         types.EntitlementGrantStatusActive,
		LastComputedAt:      &prevEnd, // already finalized
		EnvironmentID:       types.GetEnvironmentID(s.GetContext()),
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementGrantRepo.Create(s.GetContext(), prev)
	s.Require().NoError(err)

	gapEvent := prevEnd.Add(5 * time.Minute) // arrived while evaluation was down
	s.seedMeterUsage(cust.ExternalID, "meter-quantity", gapEvent, 1)

	grants, meta, err := s.grantService.EnsureGrants(s.GetContext(), cust, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	s.True(grants[0].ValidFrom.Equal(gapEvent),
		"gap usage must anchor the next window: got %s want %s", grants[0].ValidFrom, gapEvent)
	s.NotNil(meta)
}

// -----------------------------------------------------------------------------
// Domain grant-config validation
// -----------------------------------------------------------------------------

func TestEntitlementValidate_GrantConfig(t *testing.T) {
	base := func() *entitlement.Entitlement {
		return &entitlement.Entitlement{
			EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
			FeatureID:   "feat_x",
			FeatureType: types.FeatureTypeMetered,
		}
	}
	withGrant := func(e *entitlement.Entitlement) {
		e.GrantMeasure = types.EntitlementGrantMeasureQuantity
		e.GrantDurationValue = lo.ToPtr(5)
		e.GrantDurationUnit = types.EntitlementGrantDurationUnitHour
		e.GrantQuota = lo.ToPtr(decimal.NewFromInt(10))
	}
	cases := []struct {
		name    string
		mutate  func(*entitlement.Entitlement)
		wantErr bool
	}{
		{"legacy default passes", func(e *entitlement.Entitlement) {}, false},
		{"parallel without grant config rejected", func(e *entitlement.Entitlement) {
			e.AggregationMode = types.EntitlementAggregationModeParallel
		}, true},
		{"partial config (quota only) rejected", func(e *entitlement.Entitlement) {
			e.GrantQuota = lo.ToPtr(decimal.NewFromInt(10))
		}, true},
		{"missing measure rejected", func(e *entitlement.Entitlement) {
			withGrant(e)
			e.GrantMeasure = ""
		}, true},
		{"missing duration rejected", func(e *entitlement.Entitlement) {
			withGrant(e)
			e.GrantDurationValue = nil
		}, true},
		{"non-positive quota rejected", func(e *entitlement.Entitlement) {
			withGrant(e)
			e.GrantQuota = lo.ToPtr(decimal.Zero)
		}, true},
		{"grant on static feature rejected", func(e *entitlement.Entitlement) {
			withGrant(e)
			e.FeatureType = types.FeatureTypeStatic
			e.StaticValue = "on"
		}, true},
		{"valid grant config passes", withGrant, false},
		{"valid parallel grant config passes", func(e *entitlement.Entitlement) {
			withGrant(e)
			e.AggregationMode = types.EntitlementAggregationModeParallel
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := base()
			tc.mutate(e)
			err := e.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Metered aggregation: additive vs parallel vs buckets
// -----------------------------------------------------------------------------

func TestAggregateMeteredEntitlements_AdditiveSums(t *testing.T) {
	ents := []*entitlement.Entitlement{
		{ID: "e1", IsEnabled: true, UsageLimit: lo.ToPtr(int64(100))},
		{ID: "e2", IsEnabled: true, UsageLimit: lo.ToPtr(int64(250))},
	}
	agg := aggregateMeteredEntitlementsForBilling(ents)
	if agg.UsageLimit == nil || *agg.UsageLimit != 350 {
		t.Fatalf("additive must sum limits, got %v", agg.UsageLimit)
	}
	if agg.AggregationMode != types.EntitlementAggregationModeAdditive {
		t.Fatalf("mode should default to additive, got %s", agg.AggregationMode)
	}
	if agg.Buckets != nil {
		t.Fatalf("no buckets expected for additive entitlements")
	}
}

func TestAggregateMeteredEntitlements_UnlimitedWins(t *testing.T) {
	ents := []*entitlement.Entitlement{
		{ID: "e1", IsEnabled: true, UsageLimit: lo.ToPtr(int64(100))},
		{ID: "e2", IsEnabled: true, UsageLimit: nil}, // unlimited
	}
	agg := aggregateMeteredEntitlementsForBilling(ents)
	if agg.UsageLimit != nil {
		t.Fatalf("any unlimited contributor must produce a nil (unlimited) limit")
	}
}

func TestAggregateMeteredEntitlements_ParallelEmitsBuckets(t *testing.T) {
	ents := []*entitlement.Entitlement{
		{
			ID: "e1", EntityID: "plan_1", IsEnabled: true,
			GrantMeasure:    types.EntitlementGrantMeasureQuantity,
			GrantQuota:      lo.ToPtr(decimal.NewFromInt(100)),
			AggregationMode: types.EntitlementAggregationModeParallel,
		},
		{
			ID: "e2", EntityID: "addon_1", IsEnabled: true,
			GrantMeasure:    types.EntitlementGrantMeasureQuantity,
			GrantQuota:      lo.ToPtr(decimal.NewFromInt(50)),
			AggregationMode: types.EntitlementAggregationModeParallel,
		},
	}
	agg := aggregateMeteredEntitlementsForBilling(ents)
	if agg.AggregationMode != types.EntitlementAggregationModeParallel {
		t.Fatalf("mode must surface parallel, got %s", agg.AggregationMode)
	}
	if len(agg.Buckets) != 2 {
		t.Fatalf("parallel must expose one bucket per entitlement, got %d", len(agg.Buckets))
	}
	if !agg.Buckets[0].GrantQuota.Equal(decimal.NewFromInt(100)) ||
		!agg.Buckets[1].GrantQuota.Equal(decimal.NewFromInt(50)) {
		t.Fatalf("buckets must carry per-EC quotas")
	}
}

func TestAggregateMeteredEntitlements_AdditiveGrantConfig_NoBuckets(t *testing.T) {
	// Additive grant entitlements merge into one bucket — no per-EC breakdown.
	ents := []*entitlement.Entitlement{
		{
			ID: "e1", IsEnabled: true,
			GrantMeasure: types.EntitlementGrantMeasureAmount,
			GrantQuota:   lo.ToPtr(decimal.NewFromFloat(9.99)),
		},
	}
	agg := aggregateMeteredEntitlementsForBilling(ents)
	if agg.Buckets != nil {
		t.Fatalf("additive grant entitlements must not emit buckets, got %d", len(agg.Buckets))
	}
	if agg.AggregationMode != types.EntitlementAggregationModeAdditive {
		t.Fatalf("mode should be additive, got %s", agg.AggregationMode)
	}
}

func TestAggregateMeteredEntitlements_DisabledSkipped(t *testing.T) {
	ents := []*entitlement.Entitlement{
		{ID: "e1", IsEnabled: false, UsageLimit: lo.ToPtr(int64(100))},
		{ID: "e2", IsEnabled: true, UsageLimit: lo.ToPtr(int64(50))},
	}
	agg := aggregateMeteredEntitlementsForBilling(ents)
	if agg.UsageLimit == nil || *agg.UsageLimit != 50 {
		t.Fatalf("disabled entitlements must not contribute, got %v", agg.UsageLimit)
	}
}

// -----------------------------------------------------------------------------
// loadEntitlementGrantsByMeterID · scope handling
// -----------------------------------------------------------------------------

func (s *EntitlementGrantSuite) seedLoaderGrant(id string, sub *subscription.Subscription, scope types.EntitlementGrantScopeEntityType, scopeID string) {
	g := &entitlementgrant.EntitlementGrant{
		ID:                  id,
		EntitlementConfigID: "ec_" + id,
		CustomerID:          sub.CustomerID,
		SubscriptionID:      sub.ID,
		ScopeEntityType:     scope,
		ScopeEntityID:       scopeID,
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(100),
		ValidFrom:           sub.CurrentPeriodStart,
		ValidTo:             sub.CurrentPeriodStart.Add(5 * time.Hour),
		GrantStatus:         types.EntitlementGrantStatusActive,
		EnvironmentID:       types.GetEnvironmentID(s.GetContext()),
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err := s.GetStores().EntitlementGrantRepo.Create(s.GetContext(), g)
	s.Require().NoError(err)
}

func (s *EntitlementGrantSuite) aggFeature(featureID, meterID, groupID string) *dto.AggregatedFeature {
	return &dto.AggregatedFeature{
		Feature: &dto.FeatureResponse{Feature: &feature.Feature{
			ID:      featureID,
			MeterID: meterID,
			GroupID: groupID,
		}},
	}
}

func (s *EntitlementGrantSuite) loaderBillingService() *billingService {
	return &billingService{ServiceParams: ServiceParams{
		Logger:               s.GetLogger(),
		EntitlementGrantRepo: s.GetStores().EntitlementGrantRepo,
	}}
}

func (s *EntitlementGrantSuite) TestLoader_FeatureGrantsBucketedByMeter() {
	cust := s.simpleCustomer("cust-loader")
	sub := s.simpleSubscription("sub-loader", cust.ID, "plan-loader")
	s.seedLoaderGrant("eg_f1", sub, types.EntitlementGrantScopeFeature, "feat_1")
	s.seedLoaderGrant("eg_f2", sub, types.EntitlementGrantScopeFeature, "feat_2")

	features := []*dto.AggregatedFeature{
		s.aggFeature("feat_1", "meter_1", ""),
		s.aggFeature("feat_2", "meter_2", ""),
	}
	out, err := s.loaderBillingService().loadEntitlementGrantsByMeterID(
		s.GetContext(), sub, features, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	s.Require().NoError(err)
	s.Require().Len(out["meter_1"], 1)
	s.Require().Len(out["meter_2"], 1)
	s.Equal("eg_f1", out["meter_1"][0].ID)
	s.Equal("eg_f2", out["meter_2"][0].ID)
}

func (s *EntitlementGrantSuite) TestLoader_GroupAndSubGrantsNotFoldedPerMeter() {
	// A group/sub grant spans meters — folding it per meter would count its
	// overage once per meter. Until an invoice-level allocation exists, the
	// loader must exclude them.
	cust := s.simpleCustomer("cust-loader-grp")
	sub := s.simpleSubscription("sub-loader-grp", cust.ID, "plan-loader-grp")
	s.seedLoaderGrant("eg_group", sub, types.EntitlementGrantScopeGroup, "group_1")
	s.seedLoaderGrant("eg_sub", sub, types.EntitlementGrantScopeSubscription, sub.ID)

	features := []*dto.AggregatedFeature{
		s.aggFeature("feat_1", "meter_1", "group_1"),
		s.aggFeature("feat_2", "meter_2", "group_1"),
	}
	out, err := s.loaderBillingService().loadEntitlementGrantsByMeterID(
		s.GetContext(), sub, features, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	s.Require().NoError(err)
	s.Empty(out, "non-feature scopes must not be folded per meter")
}

func (s *EntitlementGrantSuite) TestLoader_ClosedGrantInCycleStillLoaded() {
	// Billing must see grants whose window closed mid-cycle — their overage
	// still bills. The loader is status-free; overlap is purely time-based.
	cust := s.simpleCustomer("cust-loader-exp")
	sub := s.simpleSubscription("sub-loader-exp", cust.ID, "plan-loader-exp")
	s.seedLoaderGrant("eg_closed", sub, types.EntitlementGrantScopeFeature, "feat_1")

	features := []*dto.AggregatedFeature{s.aggFeature("feat_1", "meter_1", "")}
	out, err := s.loaderBillingService().loadEntitlementGrantsByMeterID(
		s.GetContext(), sub, features, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	s.Require().NoError(err)
	s.Require().Len(out["meter_1"], 1, "closed-in-cycle grants must still fold into billing")
}

// -----------------------------------------------------------------------------
// End-to-end grant evaluation: usage refresh → snapshot → exhaustion alert
// -----------------------------------------------------------------------------

func (s *EntitlementGrantSuite) evalAlertService() *alertService {
	return &alertService{ServiceParams: s.buildServiceParams()}
}

func (s *EntitlementGrantSuite) seedMeterUsage(extCustomerID, meterID string, at time.Time, qty int64) {
	rec := &events.MeterUsage{
		Event: events.Event{
			ID:                 types.GenerateUUIDWithPrefix("ev"),
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			ExternalCustomerID: extCustomerID,
			EventName:          "api_call",
			Timestamp:          at,
		},
		MeterID:  meterID,
		QtyTotal: decimal.NewFromInt(qty),
	}
	s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(s.GetContext(), []*events.MeterUsage{rec}))
}

func (s *EntitlementGrantSuite) TestEvaluate_OverQuota_FlipsExhaustedAndFiresAlert() {
	f, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)
	_ = f

	// Quota is 100 (setupCustomerSubWithGrantEC); push 110 units, then open the
	// grant — the window anchors at the first event and covers both.
	first := sub.CurrentPeriodStart.Add(30 * time.Minute)
	s.seedMeterUsage(cust.ExternalID, "meter-quantity", first, 60)
	s.seedMeterUsage(cust.ExternalID, "meter-quantity", first.Add(15*time.Minute), 50)

	at := sub.CurrentPeriodStart.Add(2 * time.Hour)
	grants, meta, err := s.grantService.EnsureGrantsForSubscriptions(s.GetContext(), cust, []*subscription.Subscription{sub}, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	g := grants[0]
	s.True(g.ValidFrom.Equal(first))

	s.Require().NoError(s.evalAlertService().evaluateEntitlementGrantsForCustomer(
		s.GetContext(), cust, meta, []*entitlementgrant.EntitlementGrant{g}, at))

	stored, err := s.GetStores().EntitlementGrantRepo.Get(s.GetContext(), g.ID)
	s.Require().NoError(err)
	s.True(stored.Usage.Equal(decimal.NewFromInt(110)), "usage snapshot expected 110, got %s", stored.Usage)
	s.Equal(types.EntitlementGrantStatusExhausted, stored.GrantStatus)
	s.Require().NotNil(stored.LastComputedAt)

	logs, err := s.GetStores().AlertLogsRepo.ListByEntity(s.GetContext(), types.AlertEntityTypeEntitlementGrant, g.ID, 10)
	s.Require().NoError(err)
	s.Require().Len(logs, 1, "exhaustion must write exactly one alert log")
	s.Equal(types.AlertStateInAlarm, logs[0].AlertStatus)
	s.Equal(types.AlertTypeEntitlementGrantExhausted, logs[0].AlertType)
}

func (s *EntitlementGrantSuite) TestEvaluate_UnderQuota_StaysActiveNoAlert() {
	f, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)
	_ = f

	s.seedMeterUsage(cust.ExternalID, "meter-quantity", sub.CurrentPeriodStart.Add(30*time.Minute), 40)

	at := sub.CurrentPeriodStart.Add(2 * time.Hour)
	grants, meta, err := s.grantService.EnsureGrantsForSubscriptions(s.GetContext(), cust, []*subscription.Subscription{sub}, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	g := grants[0]

	s.Require().NoError(s.evalAlertService().evaluateEntitlementGrantsForCustomer(
		s.GetContext(), cust, meta, []*entitlementgrant.EntitlementGrant{g}, at))

	stored, err := s.GetStores().EntitlementGrantRepo.Get(s.GetContext(), g.ID)
	s.Require().NoError(err)
	s.True(stored.Usage.Equal(decimal.NewFromInt(40)))
	s.Equal(types.EntitlementGrantStatusActive, stored.GrantStatus)

	logs, err := s.GetStores().AlertLogsRepo.ListByEntity(s.GetContext(), types.AlertEntityTypeEntitlementGrant, g.ID, 10)
	s.Require().NoError(err)
	s.Empty(logs, "under-quota grants must not alert")
}

// -----------------------------------------------------------------------------
// Alert-log + webhook wiring for grant exhaustion
// -----------------------------------------------------------------------------

func TestGrantExhaustionWebhookMappingExists(t *testing.T) {
	// Regression guard: the grant alert type must map to a webhook event or
	// exhaustion alerts write a log row but never notify anyone.
	m, ok := alertWebhookMapping[types.AlertTypeEntitlementGrantExhausted]
	if !ok {
		t.Fatalf("AlertTypeEntitlementGrantExhausted missing from alertWebhookMapping")
	}
	entry, ok := m[types.AlertStateInAlarm]
	if !ok || entry.WebhookEvent == "" {
		t.Fatalf("in_alarm must map to a webhook event, got %+v", m)
	}
	if entry.WebhookEvent != types.WebhookEventEntitlementGrantExhausted {
		t.Fatalf("unexpected event name %q", entry.WebhookEvent)
	}
}

func (s *EntitlementGrantSuite) TestLogAlert_GrantExhaustion_CreatesLogRow() {
	alertLogsSvc := NewAlertLogsService(s.buildServiceParams())
	parent := string(types.AlertEntityTypeSubscription)
	custID := "cust_wh"
	err := alertLogsSvc.LogAlert(s.GetContext(), &LogAlertRequest{
		EntityType:       types.AlertEntityTypeEntitlementGrant,
		EntityID:         "eg_wh",
		ParentEntityType: &parent,
		ParentEntityID:   lo.ToPtr("sub_wh"),
		CustomerID:       &custID,
		AlertType:        types.AlertTypeEntitlementGrantExhausted,
		AlertStatus:      types.AlertStateInAlarm,
		AlertInfo: types.AlertInfo{
			ValueAtTime: decimal.NewFromFloat(1.2),
			Timestamp:   time.Now().UTC(),
		},
	})
	s.Require().NoError(err)

	latest, err := alertLogsSvc.GetLatestAlert(s.GetContext(),
		types.AlertEntityTypeEntitlementGrant, "eg_wh", nil, nil, nil, nil, nil)
	s.Require().NoError(err)
	s.Require().NotNil(latest)
	s.Equal(types.AlertStateInAlarm, latest.AlertStatus)

	// Repeat delivery with the same state must dedupe (no second log row / webhook).
	err = alertLogsSvc.LogAlert(s.GetContext(), &LogAlertRequest{
		EntityType:       types.AlertEntityTypeEntitlementGrant,
		EntityID:         "eg_wh",
		ParentEntityType: &parent,
		ParentEntityID:   lo.ToPtr("sub_wh"),
		CustomerID:       &custID,
		AlertType:        types.AlertTypeEntitlementGrantExhausted,
		AlertStatus:      types.AlertStateInAlarm,
		AlertInfo: types.AlertInfo{
			ValueAtTime: decimal.NewFromFloat(1.3),
			Timestamp:   time.Now().UTC(),
		},
	})
	s.Require().NoError(err)

	logs, err := alertLogsSvc.ListAlertsByEntity(s.GetContext(), types.AlertEntityTypeEntitlementGrant, "eg_wh", 10)
	s.Require().NoError(err)
	s.Len(logs, 1, "same-state repeat must not create a second alert log")
}

func TestEntitlementGrantOverage(t *testing.T) {
	over := &entitlementgrant.EntitlementGrant{
		Quota: decimal.NewFromInt(100),
		Usage: decimal.NewFromInt(130),
	}
	if !over.Overage().Equal(decimal.NewFromInt(30)) {
		t.Fatalf("overage = usage - quota, got %s", over.Overage())
	}
	under := &entitlementgrant.EntitlementGrant{
		Quota: decimal.NewFromInt(100),
		Usage: decimal.NewFromInt(70),
	}
	if !under.Overage().IsZero() {
		t.Fatalf("under-quota overage must be zero, got %s", under.Overage())
	}
}
