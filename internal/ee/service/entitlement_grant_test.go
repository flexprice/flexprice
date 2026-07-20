package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
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
	s.Equal(types.EntitlementGrantTypeTimeBoxed, resp.Entitlement.GrantType)
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
	s.Equal(types.EntitlementGrantTypeNone, resp.Entitlement.GrantType)
}

// -----------------------------------------------------------------------------
// M3 · EnsureGrants
// -----------------------------------------------------------------------------

func (s *EntitlementGrantSuite) TestEnsureGrants_OpensGrantWhenNoneExists() {
	f, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)
	_ = f

	at := sub.CurrentPeriodStart.Add(30 * time.Minute)
	grants, err := s.grantService.EnsureGrants(s.GetContext(), cust, at)
	s.NoError(err)
	s.Len(grants, 1, "one time_boxed EC should open exactly one grant")

	g := grants[0]
	s.Equal(cust.ID, g.CustomerID)
	s.Equal(sub.ID, g.SubscriptionID)
	s.Equal(types.EntitlementGrantScopeFeature, g.ScopeEntityType)
	s.Equal(f.ID, g.ScopeEntityID)
	s.Equal(types.EntitlementGrantStatusActive, g.GrantStatus)
	s.True(g.ValidTo.After(g.ValidFrom))
	s.True(g.ValidTo.Sub(g.ValidFrom) >= time.Hour, "min 1h window")
	s.True(!g.ValidTo.After(sub.CurrentPeriodEnd), "cycle-boundary cap")
}

func (s *EntitlementGrantSuite) TestEnsureGrants_ReturnsExistingLiveGrantUnchanged() {
	// Second call at the same tick must not duplicate — partial unique index
	// on the slot + explicit "already live" bypass in the service.
	f, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)
	_ = f

	at := sub.CurrentPeriodStart.Add(30 * time.Minute)
	first, err := s.grantService.EnsureGrants(s.GetContext(), cust, at)
	s.NoError(err)
	s.Len(first, 1)

	second, err := s.grantService.EnsureGrants(s.GetContext(), cust, at.Add(5*time.Minute))
	s.NoError(err)
	s.Len(second, 1)
	s.Equal(first[0].ID, second[0].ID, "second EnsureGrants should return the same live grant")
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

	grants, err := s.grantService.EnsureGrants(s.GetContext(), cust, sub.CurrentPeriodStart.Add(10*time.Minute))
	s.NoError(err)
	s.Empty(grants, "type=none EC should not open a grant")
}

func (s *EntitlementGrantSuite) TestEnsureGrants_SkipsWhenCustomerHasNoSubs() {
	cust := s.simpleCustomer("cust-no-sub")
	grants, err := s.grantService.EnsureGrants(s.GetContext(), cust, time.Now())
	s.NoError(err)
	s.Empty(grants)
}

// -----------------------------------------------------------------------------
// M3 · computeGrantWindow (unit — no repo needed)
// -----------------------------------------------------------------------------

func (s *EntitlementGrantSuite) TestComputeGrantWindow_CycleBoundaryCap() {
	// A 24h grant with only 6h left in the cycle must truncate at cycle_end.
	svc := s.grantService.(*entitlementGrantService)
	cycleStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	cycleEnd := cycleStart.Add(30 * 24 * time.Hour)
	at := cycleEnd.Add(-6 * time.Hour)

	sub := &subscription.Subscription{
		ID:                 "sub-cap",
		CurrentPeriodStart: cycleStart,
		CurrentPeriodEnd:   cycleEnd,
	}
	ec := s.newTimeBoxedEC("ec-cap", "feat-cap", 24, types.EntitlementGrantDurationUnitHour, decimal.NewFromInt(100))

	from, to, ok, err := svc.computeGrantWindow(s.GetContext(), ec, sub, at, 24*time.Hour)
	s.NoError(err)
	s.True(ok)
	s.WithinDuration(cycleEnd, to, time.Second, "valid_to should be capped at cycle_end")
	s.WithinDuration(at, from, time.Minute, "valid_from should anchor near `at`")
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_SubHourWindowIsSkipped() {
	// 30 minutes left in the cycle for a 5h grant duration → returns ok=false
	// so the workflow leaves the slot empty until the next cycle.
	svc := s.grantService.(*entitlementGrantService)
	cycleStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	cycleEnd := cycleStart.Add(30 * 24 * time.Hour)
	at := cycleEnd.Add(-30 * time.Minute)

	sub := &subscription.Subscription{CurrentPeriodStart: cycleStart, CurrentPeriodEnd: cycleEnd}
	ec := s.newTimeBoxedEC("ec-short", "feat-short", 5, types.EntitlementGrantDurationUnitHour, decimal.NewFromInt(10))

	_, _, ok, err := svc.computeGrantWindow(s.GetContext(), ec, sub, at, 5*time.Hour)
	s.NoError(err)
	s.False(ok, "sub-1-hour trailing window must be skipped")
}

func (s *EntitlementGrantSuite) TestComputeGrantWindow_DriftCap() {
	// If `at` is far past cycle_start with no prior grant, valid_from lands
	// at `max(at, cycle_start)` — not far in the past (which would credit
	// unpriced historical usage).
	svc := s.grantService.(*entitlementGrantService)
	cycleStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	cycleEnd := cycleStart.Add(30 * 24 * time.Hour)
	at := cycleStart.Add(5 * time.Hour)

	sub := &subscription.Subscription{CurrentPeriodStart: cycleStart, CurrentPeriodEnd: cycleEnd}
	ec := s.newTimeBoxedEC("ec-drift", "feat-drift", 5, types.EntitlementGrantDurationUnitHour, decimal.NewFromInt(10))

	from, _, ok, err := svc.computeGrantWindow(s.GetContext(), ec, sub, at, 5*time.Hour)
	s.NoError(err)
	s.True(ok)
	// The drift cap kicks in only when valid_from is < at-5min. Here we have
	// no prior grant, so valid_from starts at max(at, cycle_start) = at, well
	// after the 5min drift threshold. Verify it's not far in the past.
	s.False(from.Before(at.Add(-6*time.Minute)), "valid_from must not drift more than 5min behind `at`")
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
		GrantType:          types.EntitlementGrantTypeTimeBoxed,
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
		GrantType:          types.EntitlementGrantTypeTimeBoxed,
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

func (s *EntitlementGrantSuite) TestEnsureGrants_StaleLiveSlotExpiredAndReopened() {
	f, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)

	// Find the EC opened by setup so the stale grant lands on its slot.
	ecs, err := s.GetStores().EntitlementRepo.List(s.GetContext(), types.NewNoLimitEntitlementFilter())
	s.Require().NoError(err)
	s.Require().Len(ecs, 1)
	ecID := ecs[0].ID

	at := sub.CurrentPeriodStart.Add(10 * time.Hour)
	stale := &entitlementgrant.EntitlementGrant{
		ID:                  "eg-stale",
		EntitlementConfigID: ecID,
		CustomerID:          cust.ID,
		SubscriptionID:      sub.ID,
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       f.ID,
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(100),
		ValidFrom:           sub.CurrentPeriodStart,
		ValidTo:             at.Add(-1 * time.Hour), // window closed an hour ago
		GrantStatus:         types.EntitlementGrantStatusActive,
		EnvironmentID:       types.GetEnvironmentID(s.GetContext()),
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementGrantRepo.Create(s.GetContext(), stale)
	s.Require().NoError(err)

	grants, err := s.grantService.EnsureGrants(s.GetContext(), cust, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	s.NotEqual("eg-stale", grants[0].ID, "a fresh grant must open on the freed slot")

	old, err := s.GetStores().EntitlementGrantRepo.Get(s.GetContext(), "eg-stale")
	s.Require().NoError(err)
	s.Equal(types.EntitlementGrantStatusExpired, old.GrantStatus, "stale live grant must flip to expired")
}

func (s *EntitlementGrantSuite) TestEnsureGrants_ParallelECs_OneGrantEach() {
	// Two time-boxed ECs on the same feature (parallel mode): each EC is its
	// own slot, so EnsureGrants must open two independent grants.
	m := s.simpleMeter("meter-par")
	f := s.simpleFeature("feat-par", m.ID)
	p := s.simplePlan("plan-par")

	for i, quota := range []int64{100, 200} {
		req := s.grantCreateRequest(f.ID, p.ID, types.EntitlementGrantMeasureQuantity, 5, decimal.NewFromInt(quota))
		req.AggregationMode = types.EntitlementGrantAggregationModeParallel
		_, err := s.entService.CreateEntitlement(s.GetContext(), req)
		s.Require().NoError(err, "creating parallel EC %d", i)
	}

	cust := s.simpleCustomer("cust-par")
	sub := s.simpleSubscription("sub-par", cust.ID, p.ID)

	grants, err := s.grantService.EnsureGrants(s.GetContext(), cust, sub.CurrentPeriodStart.Add(10*time.Minute))
	s.Require().NoError(err)
	s.Require().Len(grants, 2, "each parallel EC opens its own grant")
	s.NotEqual(grants[0].EntitlementConfigID, grants[1].EntitlementConfigID)
	quotas := []int64{grants[0].Quota.IntPart(), grants[1].Quota.IntPart()}
	s.ElementsMatch([]int64{100, 200}, quotas)
}

func (s *EntitlementGrantSuite) TestEnsureGrants_AnchorsToPreviousValidTo() {
	f, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)

	ecs, err := s.GetStores().EntitlementRepo.List(s.GetContext(), types.NewNoLimitEntitlementFilter())
	s.Require().NoError(err)
	s.Require().Len(ecs, 1)

	at := sub.CurrentPeriodStart.Add(10 * time.Hour)
	prevEnd := at.Add(-2 * time.Minute) // inside the 5-minute drift guard
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
		GrantStatus:         types.EntitlementGrantStatusExpired,
		EnvironmentID:       types.GetEnvironmentID(s.GetContext()),
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementGrantRepo.Create(s.GetContext(), prev)
	s.Require().NoError(err)

	grants, err := s.grantService.EnsureGrants(s.GetContext(), cust, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	s.True(grants[0].ValidFrom.Equal(prevEnd),
		"new window must butt against previous valid_to: got %s want %s", grants[0].ValidFrom, prevEnd)
}

func (s *EntitlementGrantSuite) TestEnsureGrants_DriftGuardClampsOldAnchor() {
	f, sub, cust := s.setupCustomerSubWithGrantEC(types.EntitlementGrantMeasureQuantity)

	ecs, err := s.GetStores().EntitlementRepo.List(s.GetContext(), types.NewNoLimitEntitlementFilter())
	s.Require().NoError(err)
	s.Require().Len(ecs, 1)

	at := sub.CurrentPeriodStart.Add(10 * time.Hour)
	prevEnd := at.Add(-1 * time.Hour) // stale anchor far beyond the drift guard
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
		GrantStatus:         types.EntitlementGrantStatusExpired,
		EnvironmentID:       types.GetEnvironmentID(s.GetContext()),
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementGrantRepo.Create(s.GetContext(), prev)
	s.Require().NoError(err)

	grants, err := s.grantService.EnsureGrants(s.GetContext(), cust, at)
	s.Require().NoError(err)
	s.Require().Len(grants, 1)
	s.WithinDuration(at.Add(-5*time.Minute), grants[0].ValidFrom, 2*time.Second,
		"valid_from must clamp to at-5min, not the hour-old anchor")
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
		e.GrantType = types.EntitlementGrantTypeTimeBoxed
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
		{"parallel without time_boxed rejected", func(e *entitlement.Entitlement) {
			e.AggregationMode = types.EntitlementGrantAggregationModeParallel
		}, true},
		{"grant fields without time_boxed rejected", func(e *entitlement.Entitlement) {
			e.GrantQuota = lo.ToPtr(decimal.NewFromInt(10))
		}, true},
		{"time_boxed missing measure rejected", func(e *entitlement.Entitlement) {
			withGrant(e)
			e.GrantMeasure = ""
		}, true},
		{"time_boxed missing duration rejected", func(e *entitlement.Entitlement) {
			withGrant(e)
			e.GrantDurationValue = nil
		}, true},
		{"time_boxed non-positive quota rejected", func(e *entitlement.Entitlement) {
			withGrant(e)
			e.GrantQuota = lo.ToPtr(decimal.Zero)
		}, true},
		{"time_boxed on static feature rejected", func(e *entitlement.Entitlement) {
			withGrant(e)
			e.FeatureType = types.FeatureTypeStatic
			e.StaticValue = "on"
		}, true},
		{"valid time_boxed passes", withGrant, false},
		{"valid parallel time_boxed passes", func(e *entitlement.Entitlement) {
			withGrant(e)
			e.AggregationMode = types.EntitlementGrantAggregationModeParallel
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
	if agg.AggregationMode != types.EntitlementGrantAggregationModeAdditive {
		t.Fatalf("mode should default to additive, got %s", agg.AggregationMode)
	}
	if agg.Buckets != nil {
		t.Fatalf("no buckets expected for plain additive entitlements")
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
			GrantType:       types.EntitlementGrantTypeTimeBoxed,
			GrantMeasure:    types.EntitlementGrantMeasureQuantity,
			GrantQuota:      lo.ToPtr(decimal.NewFromInt(100)),
			AggregationMode: types.EntitlementGrantAggregationModeParallel,
		},
		{
			ID: "e2", EntityID: "addon_1", IsEnabled: true,
			GrantType:       types.EntitlementGrantTypeTimeBoxed,
			GrantMeasure:    types.EntitlementGrantMeasureQuantity,
			GrantQuota:      lo.ToPtr(decimal.NewFromInt(50)),
			AggregationMode: types.EntitlementGrantAggregationModeParallel,
		},
	}
	agg := aggregateMeteredEntitlementsForBilling(ents)
	if agg.AggregationMode != types.EntitlementGrantAggregationModeParallel {
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

func TestAggregateMeteredEntitlements_GrantConfigEmitsBucketsEvenWhenAdditive(t *testing.T) {
	ents := []*entitlement.Entitlement{
		{
			ID: "e1", IsEnabled: true,
			GrantType:    types.EntitlementGrantTypeTimeBoxed,
			GrantMeasure: types.EntitlementGrantMeasureAmount,
			GrantQuota:   lo.ToPtr(decimal.NewFromFloat(9.99)),
		},
	}
	agg := aggregateMeteredEntitlementsForBilling(ents)
	if len(agg.Buckets) != 1 {
		t.Fatalf("time-boxed EC must emit its bucket, got %d", len(agg.Buckets))
	}
	if agg.Buckets[0].GrantMeasure != types.EntitlementGrantMeasureAmount {
		t.Fatalf("bucket must carry the amount measure")
	}
	if !agg.Buckets[0].GrantQuota.Equal(decimal.NewFromFloat(9.99)) {
		t.Fatalf("bucket must carry the decimal quota for amount grants")
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

func (s *EntitlementGrantSuite) seedLoaderGrant(id string, sub *subscription.Subscription, scope types.EntitlementGrantScopeEntityType, scopeID string, status types.EntitlementGrantStatus) {
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
		GrantStatus:         status,
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
	s.seedLoaderGrant("eg_f1", sub, types.EntitlementGrantScopeFeature, "feat_1", types.EntitlementGrantStatusActive)
	s.seedLoaderGrant("eg_f2", sub, types.EntitlementGrantScopeFeature, "feat_2", types.EntitlementGrantStatusActive)

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
	s.seedLoaderGrant("eg_group", sub, types.EntitlementGrantScopeGroup, "group_1", types.EntitlementGrantStatusActive)
	s.seedLoaderGrant("eg_sub", sub, types.EntitlementGrantScopeSubscription, sub.ID, types.EntitlementGrantStatusActive)

	features := []*dto.AggregatedFeature{
		s.aggFeature("feat_1", "meter_1", "group_1"),
		s.aggFeature("feat_2", "meter_2", "group_1"),
	}
	out, err := s.loaderBillingService().loadEntitlementGrantsByMeterID(
		s.GetContext(), sub, features, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	s.Require().NoError(err)
	s.Empty(out, "non-feature scopes must not be folded per meter")
}

func (s *EntitlementGrantSuite) TestLoader_ExpiredGrantInCycleStillLoaded() {
	// Billing must see grants that expired mid-cycle — their overage still bills.
	cust := s.simpleCustomer("cust-loader-exp")
	sub := s.simpleSubscription("sub-loader-exp", cust.ID, "plan-loader-exp")
	s.seedLoaderGrant("eg_expired", sub, types.EntitlementGrantScopeFeature, "feat_1", types.EntitlementGrantStatusExpired)

	features := []*dto.AggregatedFeature{s.aggFeature("feat_1", "meter_1", "")}
	out, err := s.loaderBillingService().loadEntitlementGrantsByMeterID(
		s.GetContext(), sub, features, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	s.Require().NoError(err)
	s.Require().Len(out["meter_1"], 1, "expired-in-cycle grants must still fold into billing")
}

// -----------------------------------------------------------------------------
// snapshotEntitlementGrant · exhaustion flip
// -----------------------------------------------------------------------------

func (s *EntitlementGrantSuite) snapshotAlertService() *alertService {
	return &alertService{ServiceParams: ServiceParams{
		Logger:               s.GetLogger(),
		EntitlementGrantRepo: s.GetStores().EntitlementGrantRepo,
	}}
}

func (s *EntitlementGrantSuite) seedSnapshotGrant(id string, quota int64) *entitlementgrant.EntitlementGrant {
	g := &entitlementgrant.EntitlementGrant{
		ID:                  id,
		EntitlementConfigID: "ec_" + id,
		CustomerID:          "cust_snap",
		SubscriptionID:      "sub_snap",
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       "feat_snap",
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(quota),
		ValidFrom:           time.Now().Add(-time.Hour),
		ValidTo:             time.Now().Add(4 * time.Hour),
		GrantStatus:         types.EntitlementGrantStatusActive,
		EnvironmentID:       types.GetEnvironmentID(s.GetContext()),
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	created, err := s.GetStores().EntitlementGrantRepo.Create(s.GetContext(), g)
	s.Require().NoError(err)
	return created
}

func (s *EntitlementGrantSuite) TestSnapshot_UnderQuotaStaysActive() {
	g := s.seedSnapshotGrant("eg_under", 100)
	at := time.Now().UTC()
	s.Require().NoError(s.snapshotAlertService().snapshotEntitlementGrant(s.GetContext(), g, decimal.NewFromInt(40), at))

	stored, err := s.GetStores().EntitlementGrantRepo.Get(s.GetContext(), g.ID)
	s.Require().NoError(err)
	s.Equal(types.EntitlementGrantStatusActive, stored.GrantStatus)
	s.True(stored.Usage.Equal(decimal.NewFromInt(40)))
	s.NotNil(stored.LastComputedAt)
}

func (s *EntitlementGrantSuite) TestSnapshot_AtQuotaFlipsExhausted() {
	g := s.seedSnapshotGrant("eg_at", 100)
	s.Require().NoError(s.snapshotAlertService().snapshotEntitlementGrant(s.GetContext(), g, decimal.NewFromInt(100), time.Now().UTC()))

	stored, err := s.GetStores().EntitlementGrantRepo.Get(s.GetContext(), g.ID)
	s.Require().NoError(err)
	s.Equal(types.EntitlementGrantStatusExhausted, stored.GrantStatus)
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
