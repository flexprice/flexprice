package service

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
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
	// The grant path requires full tenant+env scoping on usage queries.
	s.WithEnvironment("env_grant_test")
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

func (s *EntitlementGrantSuite) TestCreateEntitlement_RejectsQuantityLaneOnTieredPrice() {
	// grantPricingGuard declines to fold either lane on a tiered price, and a
	// grant-based entitlement carries no usage_limit — so the legacy fallback
	// would read nil as "unlimited" and bill nothing. Reject at create instead.
	m := s.simpleMeter("meter-tier-qty")
	f := s.simpleFeature("feat-tier-qty", m.ID)
	p := &plan.Plan{ID: "plan-tier-qty", BaseModel: types.GetDefaultBaseModel(s.GetContext())}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), p))
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), &price.Price{
		ID:           "price-tier-qty",
		Amount:       decimal.NewFromFloat(0.5),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		MeterID:      m.ID,
		BaseModel:    types.GetDefaultBaseModel(s.GetContext()),
	}))

	_, err := s.entService.CreateEntitlement(s.GetContext(), s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(10)))
	s.Error(err)
	s.Contains(err.Error(), "tiered")
}

func (s *EntitlementGrantSuite) TestCreateEntitlement_AcceptsFlatPriceQuantityLane() {
	// Flat pricing on the quantity lane must still succeed — confirms the
	// widened tiered guard is not over-broad.
	m := s.simpleMeter("meter-flat-qty")
	f := s.simpleFeature("feat-flat-qty", m.ID)
	p := &plan.Plan{ID: "plan-flat-qty", BaseModel: types.GetDefaultBaseModel(s.GetContext())}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), p))
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), &price.Price{
		ID:           "price-flat-qty",
		Amount:       decimal.NewFromFloat(0.01),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
		MeterID:      m.ID,
		BaseModel:    types.GetDefaultBaseModel(s.GetContext()),
	}))

	resp, err := s.entService.CreateEntitlement(s.GetContext(), s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(10)))
	s.NoError(err)
	s.True(resp.Entitlement.HasGrantConfig())
	s.Equal(types.EntitlementGrantMeasureQuantity, resp.Entitlement.GrantMeasure)
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

// A new metered entitlement goes onto the grant model even when the caller sends only
// a usage_limit, so the legacy set stops growing while the backfill deals with the rest.
func (s *EntitlementGrantSuite) TestCreateEntitlement_DerivesGrantFromUsageLimit() {
	m := s.simpleMeter("meter-derive")
	f := s.simpleFeature("feat-derive", m.ID)
	p := &plan.Plan{ID: "plan-derive", BaseModel: types.GetDefaultBaseModel(s.GetContext())}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), p))

	base := dto.CreateEntitlementRequest{
		FeatureID:   f.ID,
		FeatureType: types.FeatureTypeMetered,
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    p.ID,
		IsEnabled:   true,
	}

	req := base
	req.UsageLimit = lo.ToPtr(int64(1000))
	resp, err := s.entService.CreateEntitlement(s.GetContext(), req)
	s.NoError(err)
	s.True(resp.Entitlement.HasGrantConfig())
	s.Equal("1000", resp.Entitlement.GrantQuota.String())
	s.Equal(types.EntitlementGrantDurationUnitSubscriptionPeriod, resp.Entitlement.GrantDurationUnit,
		"the billing-period window reproduces legacy behaviour exactly")
	s.Nil(resp.Entitlement.UsageLimit, "one row, one answer")
}

// A feature whose meter cannot carry an allowance keeps the legacy shape: forcing a
// grant there would leave it with no entitlement at all.
func (s *EntitlementGrantSuite) TestCreateEntitlement_IneligibleMeterStaysLegacy() {
	m := s.maxMeter("meter-legacy")
	f := s.simpleFeature("feat-legacy", m.ID)
	p := &plan.Plan{ID: "plan-legacy", BaseModel: types.GetDefaultBaseModel(s.GetContext())}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), p))

	resp, err := s.entService.CreateEntitlement(s.GetContext(), dto.CreateEntitlementRequest{
		FeatureID:   f.ID,
		FeatureType: types.FeatureTypeMetered,
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    p.ID,
		IsEnabled:   true,
		UsageLimit:  lo.ToPtr(int64(1000)),
	})
	s.NoError(err)
	s.False(resp.Entitlement.HasGrantConfig())
	s.Equal(int64(1000), *resp.Entitlement.UsageLimit)
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

	second, _, err := s.grantService.EnsureGrants(s.GetContext(), cust, at.Add(5*time.Minute))
	s.NoError(err)
	s.Len(second, 1)
	s.Equal(first[0].ID, second[0].ID, "second EnsureGrants should return the same live grant")
	s.NotNil(meta)
}

func (s *EntitlementGrantSuite) TestEnsureGrants_IgnoresNoneEC() {
	// A vanilla (non-grant) EC on the same subscription must NOT produce a
	// grant row. Guards against regressing legacy entitlements.
	m := s.maxMeter("meter-legacy-ec")
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

// A slot whose configs only became live mid-cycle — an addon attached on the 10th —
// must not open a window that reaches back to the cycle start, or the first tick
// charges it for usage recorded before the customer had the entitlement.
func (s *EntitlementGrantSuite) TestComputeGrantWindow_HonoursCandidateStartDate() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("startdate", 5)
	attachedAt := fx.cycleStart.Add(10 * 24 * time.Hour)

	// Usage on day 2, long before the addon was attached.
	s.seedMeterUsage(fx.extID, fx.meterID, fx.cycleStart.Add(48*time.Hour), 1)
	// Usage after the attach, which is the one the window should anchor on.
	eventAfter := attachedAt.Add(3 * time.Hour)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAfter, 1)

	meta, last := s.windowArgs(fx)
	from, _, ok, err := svc.computeGrantWindow(s.GetContext(),
		grantCandidate{ec: fx.ec, startDate: attachedAt}, fx.sub, meta, last,
		eventAfter.Add(time.Minute), 5*time.Hour)
	s.NoError(err)
	s.Require().True(ok)
	s.True(from.Equal(eventAfter),
		"window must anchor after the config went live, not on pre-attach usage: got %s want %s", from, eventAfter)
}

// subscription_period spans the cycle, so a mid-cycle start is the one thing that
// can move its left edge.
func (s *EntitlementGrantSuite) TestComputeGrantWindow_SubscriptionPeriod_StartsAtCandidateStartDate() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("startdate-subper", 5)
	attachedAt := fx.cycleStart.Add(10 * 24 * time.Hour)
	ec := s.newTimeBoxedEC("ec-subper-start", fx.ec.FeatureID, 0,
		types.EntitlementGrantDurationUnitSubscriptionPeriod, decimal.NewFromInt(400))

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(),
		grantCandidate{ec: ec, startDate: attachedAt}, fx.sub, meta, last,
		attachedAt.Add(time.Hour), 0)
	s.NoError(err)
	s.Require().True(ok)
	s.True(from.Equal(attachedAt), "got %s want %s", from, attachedAt)
	s.True(to.Equal(fx.cycleEnd))

	// Without a start date the whole cycle is covered, as before.
	from, _, ok, err = svc.computeGrantWindow(s.GetContext(),
		grantCandidate{ec: ec}, fx.sub, meta, last, attachedAt.Add(time.Hour), 0)
	s.NoError(err)
	s.Require().True(ok)
	s.True(from.Equal(fx.cycleStart), "got %s want %s", from, fx.cycleStart)
}

// The pooled window opens as soon as any contributor is live, so a mid-cycle
// addition must not push back quota that was already running.
func (s *EntitlementGrantSuite) TestGrantCandidatesForFeature_AdditiveStartDate() {
	cycleStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	day10 := cycleStart.Add(10 * 24 * time.Hour)
	day20 := cycleStart.Add(20 * 24 * time.Hour)

	planEC := &entitlement.Entitlement{ID: "ent_a", FeatureID: "f", GrantQuota: lo.ToPtr(decimal.NewFromInt(1000))}
	addonEC := &entitlement.Entitlement{ID: "ent_b", FeatureID: "f", GrantQuota: lo.ToPtr(decimal.NewFromInt(600)), StartDate: &day10}
	lateEC := &entitlement.Entitlement{ID: "ent_c", FeatureID: "f", GrantQuota: lo.ToPtr(decimal.NewFromInt(300)), StartDate: &day20}

	// One contributor runs the whole cycle → the pool is unconstrained.
	got := grantCandidatesForFeature([]*entitlement.Entitlement{planEC, addonEC})
	s.Require().Len(got, 1)
	s.True(got[0].startDate.IsZero(), "a whole-cycle contributor leaves the pool unconstrained, got %s", got[0].startDate)
	s.True(got[0].quota.Equal(decimal.NewFromInt(1600)))

	// Every contributor starts late → the pool opens at the earliest of them.
	got = grantCandidatesForFeature([]*entitlement.Entitlement{lateEC, addonEC})
	s.Require().Len(got, 1)
	s.True(got[0].startDate.Equal(day10), "expected the earliest start %s, got %s", day10, got[0].startDate)

	// Parallel: each EC carries its own start.
	parPlan := &entitlement.Entitlement{ID: "ent_p1", FeatureID: "f", GrantQuota: lo.ToPtr(decimal.NewFromInt(500)), AggregationMode: types.EntitlementAggregationModeParallel}
	parAddon := &entitlement.Entitlement{ID: "ent_p2", FeatureID: "f", GrantQuota: lo.ToPtr(decimal.NewFromInt(400)), AggregationMode: types.EntitlementAggregationModeParallel, StartDate: &day10}
	got = grantCandidatesForFeature([]*entitlement.Entitlement{parPlan, parAddon})
	s.Require().Len(got, 2)
	s.True(got[0].startDate.IsZero())
	s.True(got[1].startDate.Equal(day10))
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
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, at, 5*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(eventAt), "window must anchor at the first uncovered event: got %s want %s", from, eventAt)
	s.True(to.Equal(eventAt.Add(5 * time.Hour)))
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_NoUncoveredUsage_NoGrant() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("idle", 5)

	meta, last := s.windowArgs(fx)
	_, _, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, fx.cycleStart.Add(2*time.Hour), 5*time.Hour)
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
	from, _, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, resumeAt.Add(5*time.Minute), 5*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(resumeAt), "window must hop the idle gap to the first uncovered event: got %s want %s", from, resumeAt)
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_CycleBoundaryCap() {
	// A 24h duration anchored 6h before cycle_end: the window keeps its exact
	// usage anchor and the end caps at cycle_end (6h window — best-effort
	// minimum, coverage beats window-length aesthetics).
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("cap", 24)
	eventAt := fx.cycleEnd.Add(-6 * time.Hour)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, eventAt.Add(10*time.Minute), 24*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(eventAt), "window must anchor at the event: got %s", from)
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
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, eventAt.Add(10*time.Minute), 5*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(eventAt))
	s.True(to.Equal(fx.cycleEnd), "final window must absorb the sub-minimum stub: got %s want %s", to, fx.cycleEnd)
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_ForcedTailShortWindow() {
	// First uncovered event inside the cycle's last hour: a short boundary
	// window [event, cycle_end) opens — the 1h minimum is best-effort and
	// coverage wins at the boundary.
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("tail", 5)
	eventAt := fx.cycleEnd.Add(-30 * time.Minute)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, fx.cycleEnd.Add(-10*time.Minute), 5*time.Hour)
	s.NoError(err)
	s.True(ok, "tail window must open even when shorter than the config minimum")
	s.True(from.Equal(eventAt), "window must anchor at the event: got %s", from)
	s.True(to.Equal(fx.cycleEnd))
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_FlushWithCycleEnd_NothingToOpen() {
	// Previous window already ends at cycle_end and the cycle hasn't rolled:
	// there is nothing left to cover, so no grant opens until rollover.
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("flush", 5)
	s.seedWindowPrevGrant(fx, "eg-flush-prev", fx.cycleEnd.Add(-5*time.Hour), fx.cycleEnd)

	meta, last := s.windowArgs(fx)
	_, _, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, fx.cycleEnd.Add(-10*time.Minute), 5*time.Hour)
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
	from, _, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, fx.cycleStart.Add(3*time.Hour), 5*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(firstNewCycleEvent),
		"prior-cycle history must anchor at the new cycle's first usage: got %s", from)
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_SubscriptionPeriod_UsesCycle() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("subperiod", 0) // durHours unused for subscription_period
	fx.ec.GrantDurationUnit = types.EntitlementGrantDurationUnitSubscriptionPeriod
	fx.ec.GrantDurationValue = nil
	fx.ec.GrantAllocationBehavior = ""

	// Seed one event mid-cycle so the presence gate opens.
	s.seedMeterUsage(fx.extID, fx.meterID, fx.cycleStart.Add(2*time.Hour), 1)

	meta, last := s.windowArgs(fx)
	// dur param is ignored for this unit; pass zero.
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, fx.cycleStart.Add(3*time.Hour), 0)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(fx.cycleStart), "validFrom should equal cycleStart: got %s want %s", from, fx.cycleStart)
	s.True(to.Equal(fx.cycleEnd), "validTo should equal cycleEnd: got %s want %s", to, fx.cycleEnd)
}

// subscription_period grants are unconditionally opened at cycle boundaries —
// they carry no first-event-anchored validFrom, so there is nothing to gate on.
// This spares a per-slot ClickHouse round-trip (no earliestUncoveredUsage call)
// and gives billing/reporting a stable [cycleStart, cycleEnd) window even when
// the customer never emits usage for the feature.
func (s *EntitlementGrantSuite) TestComputeGrantWindow_SubscriptionPeriod_NoUsage_StillOpensCycleWindow() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("subperiod-idle", 0)
	fx.ec.GrantDurationUnit = types.EntitlementGrantDurationUnitSubscriptionPeriod
	fx.ec.GrantDurationValue = nil

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, fx.cycleStart.Add(1*time.Hour), 0)
	s.NoError(err)
	s.True(ok, "subscription_period should open even without usage")
	s.True(from.Equal(fx.cycleStart), "validFrom should equal cycleStart: got %s want %s", from, fx.cycleStart)
	s.True(to.Equal(fx.cycleEnd), "validTo should equal cycleEnd: got %s want %s", to, fx.cycleEnd)
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_DayUnitStart_UTC_FloorsToStartOfDay() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("day-unitstart-utc", 24)
	// newWindowFixture's cycle already starts at 2026-07-01T00:00Z; keep it. Event in mid-cycle.
	fx.sub.Timezone = "UTC"
	fx.ec.GrantDurationUnit = types.EntitlementGrantDurationUnitDay
	val := 1
	fx.ec.GrantDurationValue = &val
	fx.ec.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart

	eventAt := time.Date(2026, 7, 13, 14, 37, 0, 0, time.UTC)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, eventAt.Add(1*time.Minute), 24*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)), "expected 2026-07-13T00:00Z, got %s", from)
	s.True(to.Equal(time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)), "expected 2026-07-14T00:00Z, got %s", to)
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_DayUnitStart_ClampToCycleStart() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("day-unitstart-clamp-cycle", 24)
	// Rebase the cycle to a mid-day boundary so the day-floor lands *before* cycleStart.
	fx.sub.CurrentPeriodStart = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	fx.sub.CurrentPeriodEnd = fx.sub.CurrentPeriodStart.Add(30 * 24 * time.Hour)
	fx.sub.Timezone = "UTC"
	fx.cycleStart = fx.sub.CurrentPeriodStart
	fx.cycleEnd = fx.sub.CurrentPeriodEnd

	fx.ec.GrantDurationUnit = types.EntitlementGrantDurationUnitDay
	val := 1
	fx.ec.GrantDurationValue = &val
	fx.ec.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart

	eventAt := fx.cycleStart.Add(30 * time.Minute) // 2026-08-01T12:30Z; floor is 2026-08-01T00:00Z (< cycleStart)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, eventAt.Add(1*time.Minute), 24*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(fx.cycleStart), "validFrom should clamp to cycleStart: got %s want %s", from, fx.cycleStart)
	s.True(to.Equal(fx.cycleStart.Add(24*time.Hour)), "validTo should be cycleStart+24h: got %s", to)
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_DayUnitStart_ClampToPrevValidTo() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("day-unitstart-clamp-prev", 24)
	fx.sub.Timezone = "UTC"
	fx.ec.GrantDurationUnit = types.EntitlementGrantDurationUnitDay
	val := 1
	fx.ec.GrantDurationValue = &val
	fx.ec.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart

	// Previous grant ended mid-day 2026-07-13; new event later same day → floor is before prevValidTo.
	prevValidFrom := time.Date(2026, 7, 12, 15, 0, 0, 0, time.UTC)
	prevValidTo := time.Date(2026, 7, 13, 15, 0, 0, 0, time.UTC)
	s.seedWindowPrevGrant(fx, "prev-clamp", prevValidFrom, prevValidTo)

	eventAt := time.Date(2026, 7, 13, 20, 0, 0, 0, time.UTC)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, eventAt.Add(1*time.Minute), 24*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(prevValidTo), "validFrom should clamp to prevValidTo: got %s want %s", from, prevValidTo)
	s.True(to.Equal(prevValidTo.Add(24*time.Hour)), "validTo should be prevValidTo+24h: got %s", to)
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

// maxMeter cannot carry an allowance, so entitlements on it stay on the legacy model.
func (s *EntitlementGrantSuite) maxMeter(id string) *meter.Meter {
	m := &meter.Meter{
		ID:          id,
		Name:        id,
		EventName:   id,
		Aggregation: meter.Aggregation{Type: types.AggregationMax},
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
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

	got, err := svc.openOneGrant(s.GetContext(), sub, grantCandidate{ec: ecs[0], quota: decimal.NewFromInt(100)}, time.Time{}, meta, at)
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
	cust := s.simpleCustomer("cust-add")
	sub := s.simpleSubscription("sub-add", cust.ID, p.ID)

	// Additive groups span entities: the DB allows one published non-parallel
	// entitlement per (entity, feature), so the second EC lives on the
	// subscription (override-style addition), like plan + addon in production.
	_, err := s.entService.CreateEntitlement(s.GetContext(),
		s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(100)))
	s.Require().NoError(err, "creating plan-level additive EC")

	subReq := s.grantCreateRequest(f.ID, sub.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(200))
	subReq.EntityType = types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION
	_, err = s.entService.CreateEntitlement(s.GetContext(), subReq)
	s.Require().NoError(err, "creating subscription-level additive EC")

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

func (s *EntitlementGrantSuite) TestCreateEntitlement_RejectsDuplicateAdditiveOnSameEntityFeature() {
	// The partial unique index allows one published non-parallel entitlement
	// per (entity, feature); a bigger additive grant is a quota edit, not a
	// second row. Parallel is the stacking mode (covered by ParallelECs test).
	m := s.simpleMeter("meter-dup")
	f := s.simpleFeature("feat-dup", m.ID)
	p := s.simplePlan("plan-dup")

	_, err := s.entService.CreateEntitlement(s.GetContext(),
		s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(100)))
	s.Require().NoError(err)

	_, err = s.entService.CreateEntitlement(s.GetContext(),
		s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(200)))
	s.Require().Error(err, "second additive EC on the same (plan, feature) must be rejected")
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

func (s *EntitlementGrantSuite) TestEnsureGrants_CatchUpOpensAllMissedWindows() {
	// Rollover/outage backlog: evaluation resumes hours late with usage spread
	// across several would-be windows. ONE tick must walk every missed window —
	// draining the backlog must not depend on future events arriving.
	_, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity) // 5h duration

	// Events at +1h, +7h, +13h → three 5h windows: [1h,6h) [7h,12h) [13h,18h).
	for _, offset := range []time.Duration{1 * time.Hour, 7 * time.Hour, 13 * time.Hour} {
		s.seedMeterUsage(cust.ExternalID, "meter-quantity", sub.CurrentPeriodStart.Add(offset), 1)
	}

	at := sub.CurrentPeriodStart.Add(14 * time.Hour)
	grants, _, err := s.grantService.EnsureGrants(s.GetContext(), cust, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 3, "one pass must open every missed window")

	froms := lo.Map(grants, func(g *entitlementgrant.EntitlementGrant, _ int) time.Duration {
		return g.ValidFrom.Sub(sub.CurrentPeriodStart)
	})
	s.ElementsMatch([]time.Duration{1 * time.Hour, 7 * time.Hour, 13 * time.Hour}, froms,
		"each window must anchor at its usage event")

	// Idempotent: the caught-up slot opens nothing new on the next pass.
	knownIDs := lo.Map(grants, func(g *entitlementgrant.EntitlementGrant, _ int) string { return g.ID })
	again, _, err := s.grantService.EnsureGrants(s.GetContext(), cust, at.Add(time.Minute))
	s.Require().NoError(err)
	for _, g := range again {
		s.Contains(knownIDs, g.ID)
	}
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

	// Quota exhaustion recorded once, at the evaluation time that first saw
	// usage > quota (not the exact event time — see the note in the evaluator).
	s.Require().NotNil(stored.QuotaCrossedAt, "exhaustion must be recorded once usage exceeds quota")
	s.True(stored.QuotaCrossedAt.Equal(at), "quota_crossed_at = evaluation time, got %s", stored.QuotaCrossedAt)

	logs, err := s.GetStores().AlertLogsRepo.ListByEntity(s.GetContext(), types.AlertEntityTypeEntitlementGrant, g.ID, 10)
	s.Require().NoError(err)
	s.Require().Len(logs, 1, "exhaustion must write exactly one alert log")
	s.Equal(types.AlertStateInAlarm, logs[0].AlertStatus)
	s.Equal(types.AlertTypeEntitlementGrantExhausted, logs[0].AlertType)
}

func (s *EntitlementGrantSuite) TestEvaluate_ExactQuota_FlipsExhaustedAndRecordsCrossing() {
	// usage == quota: quota fully consumed, every later unit is overage. The
	// crossing must be recorded with the same >= the exhausted flip uses —
	// exhausted with a NULL quota_crossed_at is an illegal state.
	_, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)

	first := sub.CurrentPeriodStart.Add(30 * time.Minute)
	s.seedMeterUsage(cust.ExternalID, "meter-quantity", first, 100)

	at := sub.CurrentPeriodStart.Add(2 * time.Hour)
	grants, meta, err := s.grantService.EnsureGrantsForSubscriptions(s.GetContext(), cust, []*subscription.Subscription{sub}, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)

	s.Require().NoError(s.evalAlertService().evaluateEntitlementGrantsForCustomer(
		s.GetContext(), cust, meta, grants, at))

	stored, err := s.GetStores().EntitlementGrantRepo.Get(s.GetContext(), grants[0].ID)
	s.Require().NoError(err)
	s.True(stored.Usage.Equal(decimal.NewFromInt(100)))
	s.Equal(types.EntitlementGrantStatusExhausted, stored.GrantStatus)
	s.Require().NotNil(stored.QuotaCrossedAt, "usage == quota must record the crossing alongside the exhausted flip")
	s.True(stored.QuotaCrossedAt.Equal(at))
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

func (s *EntitlementGrantSuite) TestComputeGrantWindow_HourUnitStart_UTC_FloorsToTopOfHour() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("hour-unitstart", 1)
	fx.sub.Timezone = "UTC"
	fx.ec.GrantDurationUnit = types.EntitlementGrantDurationUnitHour
	val := 1
	fx.ec.GrantDurationValue = &val
	fx.ec.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart

	eventAt := time.Date(2026, 7, 13, 14, 37, 15, 0, time.UTC)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, eventAt.Add(1*time.Minute), 1*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(time.Date(2026, 7, 13, 14, 0, 0, 0, time.UTC)), "expected 14:00Z, got %s", from)
	s.True(to.Equal(time.Date(2026, 7, 13, 15, 0, 0, 0, time.UTC)), "expected 15:00Z, got %s", to)
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_WeekUnitStart_UTC_FloorsToMonday() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("week-unitstart", 168) // 7*24
	fx.sub.Timezone = "UTC"
	// Ensure the cycle covers the whole week so no clamp bites.
	fx.sub.CurrentPeriodStart = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	fx.sub.CurrentPeriodEnd = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	fx.cycleStart = fx.sub.CurrentPeriodStart
	fx.cycleEnd = fx.sub.CurrentPeriodEnd
	fx.ec.GrantDurationUnit = types.EntitlementGrantDurationUnitWeek
	val := 1
	fx.ec.GrantDurationValue = &val
	fx.ec.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart

	// 2026-07-16 is a Thursday. ISO Monday of that week is 2026-07-13.
	eventAt := time.Date(2026, 7, 16, 14, 37, 0, 0, time.UTC)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, eventAt.Add(1*time.Minute), 7*24*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)), "expected Mon 2026-07-13T00:00Z, got %s", from)
	s.True(to.Equal(time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)), "expected Mon 2026-07-20T00:00Z, got %s", to)
}

// value=2 with unit=week: 2-week buckets anchored at the ISO Monday containing cycleStart.
// cycleStart = 2026-07-01 (Wednesday) → anchor = Mon 2026-06-29.
// Buckets: [06-29, 07-13), [07-13, 07-27), [07-27, 08-10), ...
// Event on 2026-07-22 lands in [07-13, 07-27), aligned = 2026-07-13.
func (s *EntitlementGrantSuite) TestComputeGrantWindow_WeekUnitStart_Value2_UsesBiweeklyBuckets() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("week-unitstart-n2", 2*7*24)
	fx.sub.Timezone = "UTC"
	fx.sub.CurrentPeriodStart = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	fx.sub.CurrentPeriodEnd = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	fx.cycleStart = fx.sub.CurrentPeriodStart
	fx.cycleEnd = fx.sub.CurrentPeriodEnd
	fx.ec.GrantDurationUnit = types.EntitlementGrantDurationUnitWeek
	val := 2
	fx.ec.GrantDurationValue = &val
	fx.ec.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart

	eventAt := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, eventAt.Add(1*time.Minute), 2*7*24*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)), "expected Mon 2026-07-13T00:00Z, got %s", from)
	s.True(to.Equal(time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)), "expected Mon 2026-07-27T00:00Z, got %s", to)
}

// value=3 with unit=day: 3-day buckets anchored at start-of-day containing cycleStart.
// cycleStart = 2026-07-01T00:00Z → anchor = 2026-07-01.
// Buckets: [07-01, 07-04), [07-04, 07-07), [07-07, 07-10), ...
// Event on 2026-07-08 lands in [07-07, 07-10), aligned = 2026-07-07.
func (s *EntitlementGrantSuite) TestComputeGrantWindow_DayUnitStart_Value3_UsesThreeDayBuckets() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("day-unitstart-n3", 3*24)
	fx.sub.Timezone = "UTC"
	fx.sub.CurrentPeriodStart = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	fx.sub.CurrentPeriodEnd = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	fx.cycleStart = fx.sub.CurrentPeriodStart
	fx.cycleEnd = fx.sub.CurrentPeriodEnd
	fx.ec.GrantDurationUnit = types.EntitlementGrantDurationUnitDay
	val := 3
	fx.ec.GrantDurationValue = &val
	fx.ec.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart

	eventAt := time.Date(2026, 7, 8, 14, 0, 0, 0, time.UTC)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, eventAt.Add(1*time.Minute), 3*24*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)), "expected 2026-07-07T00:00Z, got %s", from)
	s.True(to.Equal(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)), "expected 2026-07-10T00:00Z, got %s", to)
}

// value=2 with unit=week: if the aligned bucket start lands before cycleStart,
// clamp to cycleStart (first-bucket-of-cycle can be partial).
// cycleStart = 2026-07-08 (Wednesday) → anchor = Mon 2026-07-06.
// Buckets: [07-06, 07-20), [07-20, 08-03), ...
// Event on 2026-07-10 → bucket [07-06, 07-20), aligned=07-06 → clamped to 07-08 (cycleStart).
func (s *EntitlementGrantSuite) TestComputeGrantWindow_WeekUnitStart_Value2_ClampsToCycleStart() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("week-unitstart-n2-clamp", 2*7*24)
	fx.sub.Timezone = "UTC"
	fx.sub.CurrentPeriodStart = time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	fx.sub.CurrentPeriodEnd = time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	fx.cycleStart = fx.sub.CurrentPeriodStart
	fx.cycleEnd = fx.sub.CurrentPeriodEnd
	fx.ec.GrantDurationUnit = types.EntitlementGrantDurationUnitWeek
	val := 2
	fx.ec.GrantDurationValue = &val
	fx.ec.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart

	eventAt := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, eventAt.Add(1*time.Minute), 2*7*24*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(fx.cycleStart), "expected clamp to cycleStart 2026-07-08, got %s", from)
	s.True(to.Equal(fx.cycleStart.Add(2*7*24*time.Hour)), "expected cycleStart+2w = 2026-07-22, got %s", to)
}

// DST spring-forward: strides must land on local midnight, not drift by 1h.
// cycleStart = Fri 2026-03-06T00:00 EST (T05:00Z), DST forward on Sun 2026-03-08.
// Event Tue 2026-03-10T10:00 EDT (T14:00Z) — 5 calendar days after cycleStart.
// Aligned = 2026-03-10T00:00 EDT (T04:00Z). Fixed 24h*5 math would give T05:00Z.
func (s *EntitlementGrantSuite) TestComputeGrantWindow_DayUnitStart_DSTSpringForward() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("day-unitstart-dst-spring", 24)
	fx.sub.Timezone = "America/New_York"
	fx.sub.CurrentPeriodStart = time.Date(2026, 3, 6, 5, 0, 0, 0, time.UTC)
	fx.sub.CurrentPeriodEnd = time.Date(2026, 4, 6, 4, 0, 0, 0, time.UTC)
	fx.cycleStart = fx.sub.CurrentPeriodStart
	fx.cycleEnd = fx.sub.CurrentPeriodEnd
	fx.ec.GrantDurationUnit = types.EntitlementGrantDurationUnitDay
	val := 1
	fx.ec.GrantDurationValue = &val
	fx.ec.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart

	eventAt := time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC)
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, _, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, eventAt.Add(1*time.Minute), 24*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(time.Date(2026, 3, 10, 4, 0, 0, 0, time.UTC)),
		"expected 2026-03-10T04:00Z (00:00 EDT, calendar-safe across DST), got %s", from)
}

// User semantic: value=2 week, unit_start.
// cycleStart = Mon 2026-07-06T00:00Z (week-aligned so buckets start on cycleStart).
// Buckets: [07-06, 07-20), [07-20, 08-03), [08-03, 08-17), [08-17, 08-31), ...
// Event on Tue 2026-07-07 → bucket 0 → aligned = cycleStart (07-06).
// Event on Tue 2026-07-14 → bucket 0 → aligned = cycleStart (07-06).
// Event on Tue 2026-07-21 → bucket 1 → aligned = 07-20.
// Event on Tue 2026-08-25 → bucket 3 → aligned = 08-17.
func (s *EntitlementGrantSuite) TestComputeGrantWindow_WeekUnitStart_Value2_WalksMultipleBuckets_Week1() {
	s.assertWeekValue2BucketAligned(
		time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC), // event Tue of week 1
		time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),  // → bucket 0
	)
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_WeekUnitStart_Value2_WalksMultipleBuckets_Week2() {
	s.assertWeekValue2BucketAligned(
		time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC), // event Tue of week 2
		time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),   // → bucket 0 (same as week 1)
	)
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_WeekUnitStart_Value2_WalksMultipleBuckets_Week3() {
	s.assertWeekValue2BucketAligned(
		time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC), // event Tue of week 3
		time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),  // → bucket 1
	)
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_WeekUnitStart_Value2_WalksMultipleBuckets_Week8() {
	s.assertWeekValue2BucketAligned(
		time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC), // event Tue of week 8
		time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),  // → bucket 3
	)
}

// Shared setup for the value=2 week bucket-position tests above. cycleStart is
// week-aligned (Mon 2026-07-06) so buckets begin at cycleStart, matching the
// user's "start from subscription current period start" semantic.
func (s *EntitlementGrantSuite) assertWeekValue2BucketAligned(eventAt, wantFrom time.Time) {
	svc := s.grantService.(*entitlementGrantService)
	tag := "week-v2-" + eventAt.Format("0102")
	fx := s.newWindowFixture(tag, 2*7*24)
	fx.sub.Timezone = "UTC"
	fx.sub.CurrentPeriodStart = time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)
	fx.sub.CurrentPeriodEnd = time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	fx.cycleStart = fx.sub.CurrentPeriodStart
	fx.cycleEnd = fx.sub.CurrentPeriodEnd
	fx.ec.GrantDurationUnit = types.EntitlementGrantDurationUnitWeek
	val := 2
	fx.ec.GrantDurationValue = &val
	fx.ec.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart

	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, _, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, eventAt.Add(1*time.Minute), 2*7*24*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(wantFrom), "event=%s: expected validFrom=%s, got %s", eventAt, wantFrom, from)
}

// DST + multi-iteration: value=2 week, cycleStart pre-DST (EDT), event several
// buckets in and post-DST (EST). Verifies AdvanceDays is invoked once per
// stride and each stride handles the DST offset change correctly.
// cycleStart = Mon 2026-09-07T00:00 EDT = T04:00Z. Buckets:
//
//	[09-07 EDT, 09-21 EDT), [09-21 EDT, 10-05 EDT), [10-05 EDT, 10-19 EDT),
//	[10-19 EDT, 11-02 EST)  — crosses DST fall-back on 11-01
//	[11-02 EST, 11-16 EST), [11-16 EST, 11-30 EST), ...
//
// Event Tue 2026-11-10T20:00 EST (= 2026-11-11T01:00Z) is in bucket 4.
// Aligned = Mon 2026-11-02T00:00 EST = 2026-11-02T05:00Z.
// Fixed-duration math (cycleStart + 4*14*24h) would give 2026-11-02T04:00Z,
// which is 23:00 EST on Nov 1 — an hour off due to DST fall-back drift.
func (s *EntitlementGrantSuite) TestComputeGrantWindow_WeekUnitStart_Value2_DSTFallBackAcrossMultipleBuckets() {
	svc := s.grantService.(*entitlementGrantService)
	fx := s.newWindowFixture("week-v2-dst-fallback", 2*7*24)
	fx.sub.Timezone = "America/New_York"
	fx.sub.CurrentPeriodStart = time.Date(2026, 9, 7, 4, 0, 0, 0, time.UTC) // Mon 00:00 EDT
	fx.sub.CurrentPeriodEnd = time.Date(2026, 12, 7, 5, 0, 0, 0, time.UTC)  // ~3 months later, safely post-DST
	fx.cycleStart = fx.sub.CurrentPeriodStart
	fx.cycleEnd = fx.sub.CurrentPeriodEnd
	fx.ec.GrantDurationUnit = types.EntitlementGrantDurationUnitWeek
	val := 2
	fx.ec.GrantDurationValue = &val
	fx.ec.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart

	eventAt := time.Date(2026, 11, 11, 1, 0, 0, 0, time.UTC) // Tue Nov 10 20:00 EST
	s.seedMeterUsage(fx.extID, fx.meterID, eventAt, 1)

	meta, last := s.windowArgs(fx)
	from, _, ok, err := svc.computeGrantWindow(s.GetContext(), grantCandidate{ec: fx.ec}, fx.sub, meta, last, eventAt.Add(1*time.Minute), 2*7*24*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.True(from.Equal(time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC)),
		"expected 2026-11-02T05:00Z (Mon 00:00 EST, DST-safe across multiple strides), got %s", from)
}

// M4 · subscription-scoped overrides must not silently drop grant config
// -----------------------------------------------------------------------------

// grantOverrideFixture returns a subscription plus the plan-level grant EC it
// resolves to, ready for ProcessSubscriptionEntitlementOverrides.
func (s *EntitlementGrantSuite) grantOverrideFixture(tag string) (*subscription.Subscription, *entitlement.Entitlement) {
	_, sub, _ := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasure(tag))
	ecs, err := s.GetStores().EntitlementRepo.List(s.GetContext(), types.NewNoLimitEntitlementFilter())
	s.Require().NoError(err)
	s.Require().Len(ecs, 1)
	return sub, ecs[0]
}

func (s *EntitlementGrantSuite) subScopedRows(sub *subscription.Subscription) []*entitlement.Entitlement {
	filter := types.NewNoLimitEntitlementFilter()
	filter.WithEntityIDs([]string{sub.ID}).WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION)
	rows, err := s.GetStores().EntitlementRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return rows
}

func (s *EntitlementGrantSuite) TestSubscriptionOverride_InheritsGrantConfig() {
	sub, ec := s.grantOverrideFixture("quantity")
	subSvc := NewSubscriptionService(s.buildServiceParams()).(*subscriptionService)

	s.NoError(subSvc.ProcessSubscriptionEntitlementOverrides(s.GetContext(), sub, []dto.OverrideEntitlementRequest{
		{EntitlementID: ec.ID, UsageLimit: lo.ToPtr(int64(50))},
	}))

	rows := s.subScopedRows(sub)
	s.Require().Len(rows, 1)
	// The override replaces the parent in the resolved set, so losing the grant
	// config here would silently downgrade the feature to a legacy entitlement.
	s.True(rows[0].HasGrantConfig(), "override must inherit the parent grant config")
	s.Equal(ec.GrantMeasure, rows[0].GrantMeasure)
	s.Equal(lo.FromPtr(ec.GrantQuota).String(), lo.FromPtr(rows[0].GrantQuota).String())
	s.Equal(ec.GrantDurationUnit, rows[0].GrantDurationUnit)
}

func (s *EntitlementGrantSuite) TestSubscriptionOverride_OverridesGrantQuota() {
	sub, ec := s.grantOverrideFixture("quantity")
	subSvc := NewSubscriptionService(s.buildServiceParams()).(*subscriptionService)

	s.NoError(subSvc.ProcessSubscriptionEntitlementOverrides(s.GetContext(), sub, []dto.OverrideEntitlementRequest{
		{EntitlementID: ec.ID, GrantConfigPatch: dto.GrantConfigPatch{GrantQuota: lo.ToPtr(decimal.NewFromInt(250))}},
	}))

	rows := s.subScopedRows(sub)
	s.Require().Len(rows, 1)
	s.Equal("250", lo.FromPtr(rows[0].GrantQuota).String())
	s.Equal(ec.GrantDurationUnit, rows[0].GrantDurationUnit, "untouched fields still inherit")
}

// M5 · unlimited allowances
// -----------------------------------------------------------------------------

func (s *EntitlementGrantSuite) unlimitedCreateRequest(featureID, planID string) dto.CreateEntitlementRequest {
	req := s.grantCreateRequest(featureID, planID, types.EntitlementGrantMeasureQuantity, 1, decimal.NewFromInt(1))
	req.GrantQuota = nil
	req.GrantDurationValue = nil
	req.GrantAllocationBehavior = ""
	req.GrantDurationUnit = types.EntitlementGrantDurationUnitSubscriptionPeriod
	req.GrantUnlimited = true
	return req
}

func (s *EntitlementGrantSuite) TestCreateEntitlement_UnlimitedRequiresSubscriptionPeriod() {
	m := s.simpleMeter("meter-unl-bad")
	f := s.simpleFeature("feat-unl-bad", m.ID)
	p := s.simplePlan("plan-unl-bad")

	req := s.unlimitedCreateRequest(f.ID, p.ID)
	req.GrantDurationUnit = types.EntitlementGrantDurationUnitHour
	req.GrantDurationValue = lo.ToPtr(1)

	_, err := s.entService.CreateEntitlement(s.GetContext(), req)
	s.Error(err)
	s.Contains(err.Error(), "subscription_period")
}

func (s *EntitlementGrantSuite) TestCreateEntitlement_UnlimitedAccepted() {
	m := s.simpleMeter("meter-unl")
	f := s.simpleFeature("feat-unl", m.ID)
	p := s.simplePlan("plan-unl")

	resp, err := s.entService.CreateEntitlement(s.GetContext(), s.unlimitedCreateRequest(f.ID, p.ID))
	s.NoError(err)
	s.True(resp.Entitlement.HasGrantConfig())
	s.True(resp.Entitlement.IsUnlimitedGrant())
	s.Nil(resp.Entitlement.GrantQuota)
}

// Unlimited and bounded entitlements may sit on one feature. Additive pools them and
// the pool has no ceiling, because unlimited absorbs any finite addition; parallel gives
// each its own window, so they never meet.
func (s *EntitlementGrantSuite) TestCreateEntitlement_AllowsMixingUnlimitedAndBounded() {
	ctx := s.GetContext()
	m := s.simpleMeter("meter-unl-mix")
	f := s.simpleFeature("feat-unl-mix", m.ID)
	p := s.simplePlan("plan-unl-mix")
	a := &addon.Addon{ID: "addon-unl-mix", Name: "Mix", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().AddonRepo.Create(ctx, a))

	unlimited, err := s.entService.CreateEntitlement(ctx, s.unlimitedCreateRequest(f.ID, p.ID))
	s.Require().NoError(err)

	// Additive siblings share a duration, and unlimited requires the cycle-long one, so
	// the bounded sibling has to be cycle-long too.
	addonReq := dto.CreateEntitlementRequest{
		EntityType:        types.ENTITLEMENT_ENTITY_TYPE_ADDON,
		EntityID:          a.ID,
		FeatureID:         f.ID,
		FeatureType:       types.FeatureTypeMetered,
		IsEnabled:         true,
		GrantMeasure:      types.EntitlementGrantMeasureQuantity,
		GrantQuota:        lo.ToPtr(decimal.NewFromInt(100)),
		GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod,
		AggregationMode:   types.EntitlementAggregationModeAdditive,
	}
	bounded, err := s.entService.CreateEntitlement(ctx, addonReq)
	s.Require().NoError(err, "a bounded sibling may join an unlimited pool")

	candidates := grantCandidatesForFeature([]*entitlement.Entitlement{
		unlimited.Entitlement, bounded.Entitlement,
	})
	s.Require().Len(candidates, 1, "additive pools into one window")
	s.True(candidates[0].unlimited, "one unlimited contributor removes the pool's ceiling")
	s.True(candidates[0].quota.IsZero(), "a pool with no ceiling records no quota")
}

func (s *EntitlementGrantSuite) TestUnlimitedGrant_NeverExhaustsOrBills() {
	g := &entitlementgrant.EntitlementGrant{
		Unlimited: true,
		Quota:     decimal.Zero,
		Usage:     decimal.NewFromInt(1_000_000),
	}
	s.False(g.IsExhausted(g.Usage), "unlimited window has no ceiling to cross")
	s.True(g.Overage().IsZero(), "unlimited window never contributes overage")
	// The signature makes the branch unavoidable: an unlimited window reports that it
	// has no ceiling, rather than a zero balance that reads as exhausted.
	remaining, hasCeiling := g.Remaining()
	s.False(hasCeiling, "an unlimited window has no ceiling to measure against")
	s.True(remaining.IsZero())

	bounded := &entitlementgrant.EntitlementGrant{
		Quota: decimal.NewFromInt(100),
		Usage: decimal.NewFromInt(150),
	}
	s.True(bounded.IsExhausted(bounded.Usage))
	s.Equal("50", bounded.Overage().String())
}

// M6 · review findings
// -----------------------------------------------------------------------------

func (s *EntitlementGrantSuite) TestCreateEntitlement_RejectsImplicitUnlimited() {
	// An absent quota alongside grant config used to provision a feature that
	// never bills. A dropped field or a typo'd key should not do that silently.
	m := s.simpleMeter("meter-implicit-unl")
	f := s.simpleFeature("feat-implicit-unl", m.ID)
	p := s.simplePlan("plan-implicit-unl")

	req := s.unlimitedCreateRequest(f.ID, p.ID)
	req.GrantUnlimited = false

	_, err := s.entService.CreateEntitlement(s.GetContext(), req)
	s.Error(err)
	s.Contains(err.Error(), "grant_quota is required")
}

func (s *EntitlementGrantSuite) TestUpdateEntitlement_UnlimitedFlagClearsTheQuota() {
	// GrantQuota nil means "leave alone" on update, so removing a ceiling needs
	// its own signal — without it a bounded allowance could never become
	// unlimited through the API.
	m := s.simpleMeter("meter-unl-update")
	f := s.simpleFeature("feat-unl-update", m.ID)
	p := s.simplePlan("plan-unl-update")

	created, err := s.entService.CreateEntitlement(s.GetContext(),
		s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 1, decimal.NewFromInt(100)))
	s.NoError(err)
	s.NotNil(created.Entitlement.GrantQuota)

	// Moving to the cycle window must also clear the hourly duration value, which
	// nil cannot express — the service does it when the unit says so.
	updated, err := s.entService.UpdateEntitlement(s.GetContext(), created.Entitlement.ID, dto.UpdateEntitlementRequest{
		GrantUnlimited:    lo.ToPtr(true),
		GrantDurationUnit: lo.ToPtr(types.EntitlementGrantDurationUnitSubscriptionPeriod),
	})
	s.NoError(err)
	s.Nil(updated.Entitlement.GrantQuota)
	s.True(updated.Entitlement.IsUnlimitedGrant())
}

// is_active is "spendable right now": an allowance scheduled to start later in the cycle
// carries a balance the customer cannot touch yet. Asserted through the read itself, so a
// change to the predicate there cannot pass by re-implementing it here.
func (s *EntitlementGrantSuite) TestGrantState_FutureDatedIsNotActive() {
	ctx := s.GetContext()
	fx := s.newWindowFixture("future-active", 1)

	p := s.simplePlan("plan-future-active")
	fx.sub.PlanID = p.ID
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, fx.sub))

	planEC := s.newTimeBoxedEC("ec-future-active", fx.ec.FeatureID, 1,
		types.EntitlementGrantDurationUnitHour, decimal.NewFromInt(100))
	planEC.EntityType, planEC.EntityID = types.ENTITLEMENT_ENTITY_TYPE_PLAN, p.ID
	planEC.FeatureType = types.FeatureTypeMetered
	planEC.IsEnabled = true
	_, err := s.GetStores().EntitlementRepo.Create(ctx, planEC)
	s.Require().NoError(err)

	at := fx.cycleStart.Add(4 * time.Hour)
	for _, w := range []struct {
		id    string
		from  time.Time
		hours int
	}{
		{"eg-future-open", at.Add(-time.Hour), 2},
		{"eg-future-later", at.Add(2 * time.Hour), 1},
	} {
		_, err := s.GetStores().EntitlementGrantRepo.Create(ctx, &entitlementgrant.EntitlementGrant{
			ID:                  w.id,
			EntitlementConfigID: planEC.ID,
			CustomerID:          fx.sub.CustomerID,
			SubscriptionID:      fx.sub.ID,
			ScopeEntityType:     types.EntitlementGrantScopeFeature,
			ScopeEntityID:       fx.ec.FeatureID,
			Measure:             types.EntitlementGrantMeasureQuantity,
			Quota:               decimal.NewFromInt(100),
			ValidFrom:           w.from,
			ValidTo:             w.from.Add(time.Duration(w.hours) * time.Hour),
			GrantStatus:         types.EntitlementGrantStatusActive,
			EnvironmentID:       types.GetEnvironmentID(ctx),
			BaseModel:           types.GetDefaultBaseModel(ctx),
		})
		s.Require().NoError(err)
	}

	states, err := s.grantService.GrantStateByFeature(ctx, fx.sub, at)
	s.Require().NoError(err)
	state := states[fx.ec.FeatureID]
	s.Require().NotNil(state)

	byID := map[string]*dto.GrantAllowanceState{}
	for _, a := range state.Allowances {
		byID[a.GrantID] = a
	}
	s.Require().Len(byID, 2)
	s.True(byID["eg-future-open"].IsActive, "started and not yet ended")
	s.False(byID["eg-future-later"].IsActive, "starts later in the cycle")
}

// -----------------------------------------------------------------------------
// supersede: an entitlement edit re-issues the live window at the new allowance
// -----------------------------------------------------------------------------

func (s *EntitlementGrantSuite) seedLiveWindow(
	fx windowFixture,
	id string,
	quota decimal.Decimal,
	usage decimal.Decimal,
	crossed *time.Time,
) *entitlementgrant.EntitlementGrant {
	return s.seedWindowFrom(fx, id, fx.cycleStart, quota, usage, crossed)
}

func (s *EntitlementGrantSuite) seedWindowFrom(
	fx windowFixture,
	id string,
	validFrom time.Time,
	quota decimal.Decimal,
	usage decimal.Decimal,
	crossed *time.Time,
) *entitlementgrant.EntitlementGrant {
	ctx := s.GetContext()
	g := &entitlementgrant.EntitlementGrant{
		ID:                  id,
		EntitlementConfigID: fx.ec.ID,
		CustomerID:          fx.sub.CustomerID,
		SubscriptionID:      fx.sub.ID,
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       fx.ec.FeatureID,
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               quota,
		Usage:               usage,
		ValidFrom:           validFrom,
		ValidTo:             fx.cycleEnd,
		GrantStatus:         types.EntitlementGrantStatusActive,
		LastComputedAt:      lo.ToPtr(validFrom.Add(time.Hour)),
		QuotaCrossedAt:      crossed,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	created, err := s.GetStores().EntitlementGrantRepo.Create(ctx, g)
	s.Require().NoError(err)
	return created
}

// An unlimited allowance only exists on the billing-period cadence. The caller is
// told to send the cadence rather than having it moved for them: dropping someone's
// hourly window is not a decision to make on their behalf.
func (s *EntitlementGrantSuite) TestUpdateEntitlement_UnlimitedRequiresBillingPeriodCadence() {
	ctx := s.GetContext()
	m := s.simpleMeter("meter-unl-cadence")
	f := s.simpleFeature("feat-unl-cadence", m.ID)
	p := s.simplePlan("plan-unl-cadence")

	hourly, err := s.entService.CreateEntitlement(ctx, dto.CreateEntitlementRequest{
		FeatureID:               f.ID,
		FeatureType:             types.FeatureTypeMetered,
		EntityType:              types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:                p.ID,
		IsEnabled:               true,
		GrantMeasure:            types.EntitlementGrantMeasureQuantity,
		GrantQuota:              lo.ToPtr(decimal.NewFromInt(1000)),
		GrantDurationValue:      lo.ToPtr(1),
		GrantDurationUnit:       types.EntitlementGrantDurationUnitHour,
		GrantAllocationBehavior: types.EntitlementGrantAllocationBehaviorFirstUsage,
		AggregationMode:         types.EntitlementAggregationModeAdditive,
	})
	s.Require().NoError(err)

	_, err = s.entService.UpdateEntitlement(ctx, hourly.ID, dto.UpdateEntitlementRequest{
		GrantUnlimited: lo.ToPtr(true),
	})
	s.Error(err, "unlimited on an hourly cadence must be rejected, not silently re-cadenced")

	// Stated together, it is accepted.
	updated, err := s.entService.UpdateEntitlement(ctx, hourly.ID, dto.UpdateEntitlementRequest{
		GrantUnlimited:    lo.ToPtr(true),
		GrantDurationUnit: lo.ToPtr(types.EntitlementGrantDurationUnitSubscriptionPeriod),
	})
	s.Require().NoError(err)
	s.True(updated.Entitlement.IsUnlimitedGrant())
}

// Restating the stored config is not a change, so the meter and price rules are not
// re-run — a row whose meter was since made ineligible still accepts unrelated edits.
func (s *EntitlementGrantSuite) TestUpdateEntitlement_UnchangedConfigSkipsShapeCheck() {
	ctx := s.GetContext()
	m := s.simpleMeter("meter-nochange")
	f := s.simpleFeature("feat-nochange", m.ID)
	p := s.simplePlan("plan-nochange")

	ec, err := s.entService.CreateEntitlement(ctx, dto.CreateEntitlementRequest{
		FeatureID:               f.ID,
		FeatureType:             types.FeatureTypeMetered,
		EntityType:              types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:                p.ID,
		IsEnabled:               true,
		GrantMeasure:            types.EntitlementGrantMeasureQuantity,
		GrantQuota:              lo.ToPtr(decimal.NewFromInt(1000)),
		GrantDurationValue:      lo.ToPtr(1),
		GrantDurationUnit:       types.EntitlementGrantDurationUnitHour,
		GrantAllocationBehavior: types.EntitlementGrantAllocationBehaviorFirstUsage,
		AggregationMode:         types.EntitlementAggregationModeAdditive,
	})
	s.Require().NoError(err)

	// The meter becomes one that cannot carry an allowance — the shape that used to
	// make every unrelated edit on this row fail.
	stored, err := s.meterStore.GetMeter(ctx, m.ID)
	s.Require().NoError(err)
	stored.Aggregation.Type = types.AggregationMax
	s.Require().NoError(s.meterStore.InMemoryStore.Update(ctx, stored.ID, stored))

	// Restating the same quota is not a change, so nothing is re-validated.
	_, err = s.entService.UpdateEntitlement(ctx, ec.ID, dto.UpdateEntitlementRequest{
		GrantQuota: lo.ToPtr(decimal.NewFromInt(1000)),
	})
	s.NoError(err)

	// Moving it is, and the now-ineligible meter rejects it.
	_, err = s.entService.UpdateEntitlement(ctx, ec.ID, dto.UpdateEntitlementRequest{
		GrantQuota: lo.ToPtr(decimal.NewFromInt(2000)),
	})
	s.Error(err)
}

// Carrying an unlimited window forward used to collapse it to the incoming quota,
// because Remaining() reported zero and the successor was built from that number.
func (s *EntitlementGrantSuite) TestOpenGrants_UnlimitedSuccessorStaysUnlimited() {
	ctx := s.GetContext()
	fx := s.newWindowFixture("carry-unl", 5)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, fx.sub))

	// The config feeding the feature is what decides: a successor is unlimited because
	// an unlimited config still funds the feature, not because its predecessor was.
	fx.ec.GrantQuota = nil
	s.Require().True(fx.ec.IsUnlimitedGrant())

	closed := s.seedLiveWindow(fx, "eg-carry-unl", decimal.Zero, decimal.NewFromInt(900), nil)
	closed.Unlimited = true
	closed.ValidTo = fx.cycleStart.Add(2 * time.Hour)
	_, err := s.GetStores().EntitlementGrantRepo.Update(ctx, closed)
	s.Require().NoError(err)

	opened, err := s.grantService.OpenFeatureBasedEntitlementGrants(ctx, []OpenFeatureBasedEntitlementGrantsRequest{{
		FeatureID: fx.ec.FeatureID,
		Closed:    closed,
		New: entitlementgrant.NewEntitlementGrantBuilder(closed).
			WithQuota(decimal.Zero).
			WithWindow(closed.ValidTo, fx.cycleEnd).
			Build(),
		ExistingECs: []*entitlement.Entitlement{fx.ec},
	}})
	s.Require().NoError(err)
	s.Require().Len(opened, 1, "an unlimited successor must open; a zero quota used to drop it")
	s.True(opened[0].Unlimited)
}

// Three ECs pool into one window. Deleting one that is not an override of another
// should shrink the pool — the deleted EC's quota must stop counting.
func (s *EntitlementGrantSuite) TestDeleteEntitlement_ShrinksPooledWindow() {
	ctx := s.GetContext()
	fx := s.newWindowFixture("pool-delete", 5)

	// The plan's EC is what survives the deletion. Without a survivor the feature is no
	// longer funded at all, which is the other case: there the window is left to run out
	// rather than cut, since granted quota is never taken back.
	p := s.simplePlan("plan-pool-delete")
	fx.sub.PlanID = p.ID
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, fx.sub))

	pooledEC := func(quota int64) dto.CreateEntitlementRequest {
		return dto.CreateEntitlementRequest{
			FeatureID:               fx.ec.FeatureID,
			FeatureType:             types.FeatureTypeMetered,
			IsEnabled:               true,
			GrantMeasure:            types.EntitlementGrantMeasureQuantity,
			GrantQuota:              lo.ToPtr(decimal.NewFromInt(quota)),
			GrantDurationValue:      lo.ToPtr(5),
			GrantDurationUnit:       types.EntitlementGrantDurationUnitHour,
			GrantAllocationBehavior: types.EntitlementGrantAllocationBehaviorFirstUsage,
			AggregationMode:         types.EntitlementAggregationModeAdditive,
		}
	}

	planReq := pooledEC(1000)
	planReq.EntityType, planReq.EntityID = types.ENTITLEMENT_ENTITY_TYPE_PLAN, p.ID
	_, err := s.entService.CreateEntitlement(ctx, planReq)
	s.Require().NoError(err)

	// A net-new subscription EC on the same feature: no parent, so it pools.
	subReq := pooledEC(500)
	subReq.EntityType, subReq.EntityID = types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION, fx.sub.ID
	netNew, err := s.entService.CreateEntitlement(ctx, subReq)
	s.Require().NoError(err)

	now := time.Now().UTC()
	pooled := s.seedWindowFrom(fx, "eg-pool-delete", now.Add(-time.Hour), decimal.NewFromInt(1500), decimal.NewFromInt(100), nil)
	pooled.ValidTo = now.Add(4 * time.Hour)
	_, err = s.GetStores().EntitlementGrantRepo.Update(ctx, pooled)
	s.Require().NoError(err)

	s.Require().NoError(s.entService.DeleteEntitlement(ctx, netNew.ID))

	// The window is cut at the deletion, so the deleted EC's quota stops running.
	// What the pool had not spent is carried into a successor by the same path an
	// addon detach takes, once the surviving ECs resolve.
	after, err := s.GetStores().EntitlementGrantRepo.Get(ctx, pooled.ID)
	s.Require().NoError(err)
	s.True(after.ValidTo.Before(now.Add(4*time.Hour)),
		"before this, deleting a pooled EC left its quota running to the end of the window")
}

// A quota edit closes the live window and opens a successor for the rest of it. The
// balance travels as quota, never as a usage figure: the evaluator re-measures usage
// over each window's own span and would overwrite a carried one.
//
// Remaining + delta is the same number as recalculating from scratch —
// (old − usage) + (new − old) = new − usage — so each row bills only its own span and
// nothing has to be excluded from the fold.
func (s *EntitlementGrantSuite) TestReissue_BalanceTravelsAsQuota() {
	ctx := s.GetContext()
	fx := s.newWindowFixture("reissue", 5)
	now := time.Now().UTC()

	// The service resolves the live windows and the entitlements funding them itself,
	// so both have to be readable for this subscription.
	p := s.simplePlan("plan-reissue")
	fx.sub.PlanID = p.ID
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, fx.sub))

	var err error
	planEC := s.newTimeBoxedEC("ec-reissue-plan", fx.ec.FeatureID, 5,
		types.EntitlementGrantDurationUnitHour, decimal.NewFromInt(1000))
	planEC.EntityType, planEC.EntityID = types.ENTITLEMENT_ENTITY_TYPE_PLAN, p.ID
	planEC.FeatureType = types.FeatureTypeMetered
	planEC.IsEnabled = true
	_, err = s.GetStores().EntitlementRepo.Create(ctx, planEC)
	s.Require().NoError(err)

	live := s.seedWindowFrom(fx, "eg-reissue", now.Add(-time.Hour), decimal.NewFromInt(1000), decimal.NewFromInt(800), nil)
	live.ValidTo = now.Add(4 * time.Hour)
	_, err = s.GetStores().EntitlementGrantRepo.Update(ctx, live)
	s.Require().NoError(err)

	opened, err := s.grantService.ReissueEntitlementGrants(ctx, &dto.ReissueEntitlementGrantsRequest{
		SubscriptionID: fx.sub.ID,
		FeatureID:      fx.ec.FeatureID,
		Delta:          decimal.NewFromInt(4000), // 5,000 − 1,000
		At:             now,
		Source:         "entitlement_updated",
	})
	s.Require().NoError(err)
	s.Require().Len(opened, 1)

	// 200 unspent + 4,000 added = 4,200, which is 5,000 less the 800 already used.
	s.Equal("4200", opened[0].Quota.String())
	s.True(opened[0].Usage.IsZero(), "the successor measures its own span from zero")
	s.True(opened[0].ValidFrom.Equal(now))
	s.True(opened[0].ValidTo.Equal(live.ValidTo))

	closed, err := s.GetStores().EntitlementGrantRepo.Get(ctx, live.ID)
	s.Require().NoError(err)
	s.Equal(types.EntitlementGrantStatusActive, closed.GrantStatus,
		"the closed window keeps billing its own span; there is no replaced state")
	s.True(closed.ValidTo.Equal(now))
	s.Equal("1000", closed.Quota.String())
	s.True(closed.Overage().IsZero(), "800 against 1,000 owes nothing")
}

// A ceiling appearing mid-window leaves nothing for the rest of it: the customer spent
// that window without one, so there is no balance to hand over and no reason to issue a
// fresh allowance on top. The surviving config funds the next window on its own cadence.
func (s *EntitlementGrantSuite) TestRemoveUnlimitedEC_SuccessorHoldsSlotAtZero() {
	ctx := s.GetContext()
	fx := s.newWindowFixture("unl-fallback", 5)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, fx.sub))

	// fx.ec is the unlimited one and is about to leave; a bounded 100 survives.
	fx.ec.GrantQuota = nil
	survivor := s.newTimeBoxedEC("ec-unl-survivor", fx.ec.FeatureID, 5,
		types.EntitlementGrantDurationUnitHour, decimal.NewFromInt(100))

	// The live pooled window as grantCandidatesForFeature builds it: unlimited from
	// fx.ec, and zero quota, since a pool with no ceiling has none to record.
	closed := s.seedLiveWindow(fx, "eg-unl-fallback", decimal.Zero, decimal.NewFromInt(40), nil)
	closed.Unlimited = true
	closed.ValidTo = fx.cycleStart.Add(2 * time.Hour)
	_, err := s.GetStores().EntitlementGrantRepo.Update(ctx, closed)
	s.Require().NoError(err)

	opened, err := s.grantService.OpenFeatureBasedEntitlementGrants(ctx, []OpenFeatureBasedEntitlementGrantsRequest{{
		FeatureID: fx.ec.FeatureID,
		Closed:    closed,
		New: entitlementgrant.NewEntitlementGrantBuilder(closed).
			WithQuota(decimal.Zero).
			WithWindow(closed.ValidTo, fx.cycleEnd).
			Build(),
		ExistingECs: []*entitlement.Entitlement{survivor},
	}})
	s.Require().NoError(err)
	s.Require().Len(opened, 1, "the survivor still funds the feature, so a row must hold the slot")
	s.False(opened[0].Unlimited, "the unlimited config left")
	s.Equal("0", opened[0].Quota.String(), "nothing is carried from a window that had no ceiling")
	s.Equal(types.EntitlementGrantStatusExhausted, opened[0].GrantStatus)
}

// An hourly allowance on a monthly cycle produces hundreds of windows. A read returns
// the newest few of each entitlement, so a parallel feature's busiest series cannot
// crowd the others out of the response.
func (s *EntitlementGrantSuite) TestGrantState_CapsWindowsPerSlot() {
	ctx := s.GetContext()
	fx := s.newWindowFixture("cap-windows", 1)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, fx.sub))

	// Two entitlements on one feature, eight windows each, interleaved in time.
	other := s.newTimeBoxedEC("ec-cap-other", fx.ec.FeatureID, 1,
		types.EntitlementGrantDurationUnitHour, decimal.NewFromInt(50))

	for i := 0; i < 8; i++ {
		for _, ecID := range []string{fx.ec.ID, other.ID} {
			from := fx.cycleStart.Add(time.Duration(i) * time.Hour)
			_, err := s.GetStores().EntitlementGrantRepo.Create(ctx, &entitlementgrant.EntitlementGrant{
				ID:                  fmt.Sprintf("eg-cap-%s-%d", ecID, i),
				EntitlementConfigID: ecID,
				CustomerID:          fx.sub.CustomerID,
				SubscriptionID:      fx.sub.ID,
				ScopeEntityType:     types.EntitlementGrantScopeFeature,
				ScopeEntityID:       fx.ec.FeatureID,
				Measure:             types.EntitlementGrantMeasureQuantity,
				Quota:               decimal.NewFromInt(100),
				Usage:               decimal.NewFromInt(int64(i)),
				ValidFrom:           from,
				ValidTo:             from.Add(time.Hour),
				GrantStatus:         types.EntitlementGrantStatusActive,
				EnvironmentID:       types.GetEnvironmentID(ctx),
				BaseModel:           types.GetDefaultBaseModel(ctx),
			})
			s.Require().NoError(err)
		}
	}

	states, err := s.grantService.GrantStateByFeature(ctx, fx.sub, fx.cycleStart.Add(9*time.Hour))
	s.Require().NoError(err)
	state := states[fx.ec.FeatureID]
	s.Require().NotNil(state)

	byEC := map[string][]*dto.GrantAllowanceState{}
	for _, w := range state.Allowances {
		byEC[w.EntitlementID] = append(byEC[w.EntitlementID], w)
	}
	s.Len(byEC, 2, "both entitlements keep a series")
	for ecID, windows := range byEC {
		s.Len(windows, GrantWindowsPerSlot, "capped per entitlement: %s", ecID)
		s.Equal(fx.cycleStart.Add(7*time.Hour), windows[len(windows)-1].ValidFrom,
			"the most recent window of each slot is kept: %s", ecID)
		s.True(sort.SliceIsSorted(windows, func(a, b int) bool {
			return windows[a].ValidFrom.Before(windows[b].ValidFrom)
		}), "still oldest first: %s", ecID)
	}
}

// A create-time override inherits the parent's grant config, so nil means "keep this".
// That leaves grant_unlimited as the only way to say the customer has no ceiling.
func (s *EntitlementGrantSuite) TestSubscriptionOverride_GrantUnlimitedClearsInheritedQuota() {
	sub, ec := s.grantOverrideFixture("quantity")
	s.Require().NotNil(ec.GrantQuota, "the plan's allowance is bounded")

	subSvc := NewSubscriptionService(s.buildServiceParams()).(*subscriptionService)
	s.NoError(subSvc.ProcessSubscriptionEntitlementOverrides(s.GetContext(), sub,
		[]dto.OverrideEntitlementRequest{{
			EntitlementID: ec.ID,
			GrantConfigPatch: dto.GrantConfigPatch{
				GrantUnlimited:    lo.ToPtr(true),
				GrantDurationUnit: lo.ToPtr(types.EntitlementGrantDurationUnitSubscriptionPeriod),
			},
		}}))

	rows := s.subScopedRows(sub)
	s.Require().Len(rows, 1)
	s.True(rows[0].IsUnlimitedGrant(), "the inherited ceiling is gone")
}

// grant_unlimited: false has nothing to restore unless a quota comes with it — the
// parent's is what was just being replaced.
func (s *EntitlementGrantSuite) TestSubscriptionOverride_BoundedNeedsAQuota() {
	sub, ec := s.grantOverrideFixture("quantity")

	subSvc := NewSubscriptionService(s.buildServiceParams()).(*subscriptionService)
	err := subSvc.ProcessSubscriptionEntitlementOverrides(s.GetContext(), sub,
		[]dto.OverrideEntitlementRequest{{
			EntitlementID: ec.ID,
			GrantConfigPatch: dto.GrantConfigPatch{
				GrantUnlimited: lo.ToPtr(true),
				GrantQuota:     lo.ToPtr(decimal.NewFromInt(50)),
			},
		}})
	s.Error(err, "a ceiling and no ceiling cannot both be asked for")
}

// A reader asking why a window is exhausted wants the crossing, not ValidTo:
// overage is billed from the crossing onward.
func (s *EntitlementGrantSuite) TestGrantState_ReportsQuotaCrossedAt() {
	ctx := s.GetContext()
	fx := s.newWindowFixture("state-crossed", 24)
	crossed := fx.cycleStart.Add(90 * time.Minute)

	s.seedLiveWindow(fx, "eg-crossed", decimal.NewFromInt(100), decimal.NewFromInt(140), &crossed)
	s.seedWindowFrom(fx, "eg-open", fx.cycleStart.Add(2*time.Hour), decimal.NewFromInt(100), decimal.NewFromInt(10), nil)

	states, err := s.grantService.GrantStateByFeature(ctx, fx.sub, fx.cycleStart.Add(3*time.Hour))
	s.Require().NoError(err)
	state := states[fx.ec.FeatureID]
	s.Require().NotNil(state)

	byID := map[string]*dto.GrantAllowanceState{}
	for _, a := range state.Allowances {
		byID[a.GrantID] = a
	}

	s.Require().NotNil(byID["eg-crossed"].QuotaCrossedAt, "a window over its quota must report when it crossed")
	s.True(byID["eg-crossed"].QuotaCrossedAt.Equal(crossed))
	s.Nil(byID["eg-open"].QuotaCrossedAt, "a window with room left has not crossed")
}
