package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
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

// entitlementCustomerCastForTests exposes the internal service type — used to
// reach the `computeGrantWindow` helper for direct-unit tests. Keeping the
// helper unexported at the domain boundary is intentional; tests get access via
// a type assertion in the same package.
var _ = func() *entitlementGrantService { return &entitlementGrantService{} }
