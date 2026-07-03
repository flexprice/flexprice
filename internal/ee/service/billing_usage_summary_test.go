package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
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

// BillingUsageSummarySuite covers GetCustomerUsageSummary and the small billing
// helpers around it: getUsagePercent, calculateRemainingCommitment,
// aggregateStaticEntitlementsForBilling and calculateNeverResetUsage.
type BillingUsageSummarySuite struct {
	testutil.BaseServiceTestSuite
	service   BillingService
	billing   *billingService
	eventRepo *testutil.InMemoryEventStore
	customer  *customer.Customer
	now       time.Time
}

func TestBillingUsageSummary(t *testing.T) {
	suite.Run(t, new(BillingUsageSummarySuite))
}

func (s *BillingUsageSummarySuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.eventRepo = s.GetStores().EventRepo.(*testutil.InMemoryEventStore)
	s.service = NewBillingService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.billing = s.service.(*billingService)
	s.now = time.Now().UTC()

	s.customer = &customer.Customer{
		ID:         "cust_bus",
		ExternalID: "ext_bus",
		Name:       "Usage Summary Customer",
		Email:      "bus@test.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.customer))
}

// summaryFixture bundles the entities for one metered feature on one subscription.
type summaryFixture struct {
	plan    *plan.Plan
	meterM  *meter.Meter
	feat    *feature.Feature
	priceP  *price.Price
	sub     *subscription.Subscription
	item    *subscription.SubscriptionLineItem
	eventNm string
}

// newSummaryFixture builds a plan + SUM meter + feature + price + entitlement +
// active subscription with one usage line item, and seeds one feature_usage row
// with chargeQty so GetFeatureUsageBySubscription produces a charge.
func (s *BillingUsageSummarySuite) newSummaryFixture(
	suffix string,
	billingPeriod types.BillingPeriod,
	reset types.EntitlementUsageResetPeriod,
	limit *int64,
	chargeQty float64,
) *summaryFixture {
	ctx := s.GetContext()
	fx := &summaryFixture{eventNm: "evt_bus_" + suffix}

	fx.plan = &plan.Plan{
		ID:        "plan_bus_" + suffix,
		Name:      "Summary Plan " + suffix,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, fx.plan))

	fx.meterM = &meter.Meter{
		ID:        "meter_bus_" + suffix,
		Name:      "Summary Meter " + suffix,
		EventName: fx.eventNm,
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "qty",
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, fx.meterM))

	fx.feat = &feature.Feature{
		ID:        "feat_bus_" + suffix,
		Name:      "Summary Feature " + suffix,
		Type:      types.FeatureTypeMetered,
		MeterID:   fx.meterM.ID,
		LookupKey: "lk_bus_" + suffix,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, fx.feat))

	upTo1000 := uint64(1000)
	fx.priceP = &price.Price{
		ID:                 "price_bus_" + suffix,
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           fx.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      billingPeriod,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		TierMode:           types.BILLING_TIER_SLAB,
		MeterID:            fx.meterM.ID,
		Tiers: []price.PriceTier{
			{UpTo: &upTo1000, UnitAmount: decimal.RequireFromString("0.02")},
			{UpTo: nil, UnitAmount: decimal.RequireFromString("0.01")},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, fx.priceP))

	ent := &entitlement.Entitlement{
		ID:               "ent_bus_" + suffix,
		EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:         fx.plan.ID,
		FeatureID:        fx.feat.ID,
		FeatureType:      types.FeatureTypeMetered,
		IsEnabled:        true,
		UsageLimit:       limit,
		UsageResetPeriod: reset,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().EntitlementRepo.Create(ctx, ent)
	s.NoError(err)

	// Period windows: short window for monthly billing, wide window for annual.
	periodStart := s.now.Add(-48 * time.Hour)
	periodEnd := s.now.Add(6 * 24 * time.Hour)
	startDate := s.now.Add(-30 * 24 * time.Hour)
	if billingPeriod == types.BILLING_PERIOD_ANNUAL {
		periodStart = s.now.Add(-60 * 24 * time.Hour)
		periodEnd = s.now.Add(300 * 24 * time.Hour)
		startDate = periodStart
	}

	fx.sub = &subscription.Subscription{
		ID:                 "sub_bus_" + suffix,
		PlanID:             fx.plan.ID,
		CustomerID:         s.customer.ID,
		StartDate:          startDate,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      billingPeriod,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	fx.item = &subscription.SubscriptionLineItem{
		ID:               "li_bus_" + suffix,
		SubscriptionID:   fx.sub.ID,
		CustomerID:       s.customer.ID,
		EntityID:         fx.plan.ID,
		EntityType:       types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:  fx.plan.Name,
		PriceID:          fx.priceP.ID,
		PriceType:        types.PRICE_TYPE_USAGE,
		MeterID:          fx.meterM.ID,
		MeterDisplayName: fx.meterM.Name,
		DisplayName:      "Summary Usage " + suffix,
		Quantity:         decimal.Zero,
		Currency:         "usd",
		BillingPeriod:    billingPeriod,
		InvoiceCadence:   types.InvoiceCadenceArrear,
		StartDate:        startDate,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, fx.sub,
		[]*subscription.SubscriptionLineItem{fx.item}))

	// One feature_usage row so a charge exists for this meter.
	featureUsageStore := s.GetStores().FeatureUsageRepo.(*testutil.InMemoryFeatureUsageStore)
	s.NoError(featureUsageStore.InsertProcessedEvent(ctx, &events.FeatureUsage{
		Event: events.Event{
			ID:                 s.GetUUID(),
			TenantID:           types.GetTenantID(ctx),
			EnvironmentID:      types.GetEnvironmentID(ctx),
			EventName:          fx.eventNm,
			CustomerID:         s.customer.ID,
			ExternalCustomerID: s.customer.ExternalID,
			Timestamp:          s.now.Add(-1 * time.Hour),
		},
		SubscriptionID: fx.sub.ID,
		SubLineItemID:  fx.item.ID,
		PriceID:        fx.priceP.ID,
		FeatureID:      fx.feat.ID,
		MeterID:        fx.meterM.ID,
		QtyTotal:       decimal.NewFromFloat(chargeQty),
	}))

	return fx
}

// insertEvent seeds one raw event with the given qty for a fixture's meter.
func (s *BillingUsageSummarySuite) insertEvent(fx *summaryFixture, ts time.Time, qty float64) {
	ctx := s.GetContext()
	s.NoError(s.eventRepo.InsertEvent(ctx, &events.Event{
		ID:                 s.GetUUID(),
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		EventName:          fx.eventNm,
		ExternalCustomerID: s.customer.ExternalID,
		Timestamp:          ts,
		Properties:         map[string]interface{}{"qty": qty},
	}))
}

func (s *BillingUsageSummarySuite) featureSummary(resp *dto.CustomerUsageSummaryResponse, featureID string) *dto.FeatureUsageSummary {
	for _, f := range resp.Features {
		if f.Feature != nil && f.Feature.ID == featureID {
			return f
		}
	}
	return nil
}

func (s *BillingUsageSummarySuite) TestGetCustomerUsageSummary_Validation() {
	testCases := []struct {
		name       string
		customerID string
	}{
		{name: "empty_customer_id_returns_error", customerID: ""},
		{name: "unknown_customer_returns_error", customerID: "cust_does_not_exist"},
	}
	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.GetCustomerUsageSummary(s.GetContext(), tc.customerID, nil)
			s.Error(err)
			s.Nil(resp)
		})
	}
}

func (s *BillingUsageSummarySuite) TestGetCustomerUsageSummary_NoSubscriptionsReturnsEmptyFeatures() {
	resp, err := s.service.GetCustomerUsageSummary(s.GetContext(), s.customer.ID, nil)
	s.NoError(err)
	s.Equal(s.customer.ID, resp.CustomerID)
	s.Empty(resp.Features)
}

func (s *BillingUsageSummarySuite) TestGetCustomerUsageSummary_BillingPeriodReset() {
	fx := s.newSummaryFixture("bp", types.BILLING_PERIOD_MONTHLY,
		types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY, lo.ToPtr(int64(1000)), 500)

	resp, err := s.service.GetCustomerUsageSummary(s.GetContext(), s.customer.ID, nil)
	s.NoError(err)
	s.Len(resp.Features, 1)

	f := s.featureSummary(resp, fx.feat.ID)
	s.NotNil(f)
	// Reset period == billing period → usage aggregated from charge quantities.
	s.True(f.CurrentUsage.Equal(decimal.NewFromInt(500)), "usage should be 500, got %s", f.CurrentUsage)
	s.True(f.UsagePercent.Equal(decimal.RequireFromString("0.5")), "percent should be 0.5, got %s", f.UsagePercent)
	s.Equal(int64(1000), lo.FromPtr(f.TotalLimit))
	s.False(f.IsUnlimited)
	s.True(f.IsEnabled)
	s.NotNil(f.NextUsageResetAt, "metered features must carry a next usage reset timestamp")
	s.Len(f.Sources, 1)
	s.Equal(fx.sub.ID, f.Sources[0].SubscriptionID)
}

func (s *BillingUsageSummarySuite) TestGetCustomerUsageSummary_DailyResetUsesTodaysUsage() {
	fx := s.newSummaryFixture("daily", types.BILLING_PERIOD_MONTHLY,
		types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY, lo.ToPtr(int64(100)), 50)

	todayStart := time.Date(s.now.Year(), s.now.Month(), s.now.Day(), 0, 0, 0, 0, time.UTC)
	s.insertEvent(fx, todayStart.Add(-2*time.Hour), 30) // yesterday
	s.insertEvent(fx, todayStart.Add(1*time.Hour), 20)  // today

	resp, err := s.service.GetCustomerUsageSummary(s.GetContext(), s.customer.ID, nil)
	s.NoError(err)

	f := s.featureSummary(resp, fx.feat.ID)
	s.NotNil(f)
	s.True(f.CurrentUsage.Equal(decimal.NewFromInt(20)),
		"daily reset must only count today's usage (20), got %s", f.CurrentUsage)
	s.True(f.UsagePercent.Equal(decimal.RequireFromString("0.2")))
}

func (s *BillingUsageSummarySuite) TestGetCustomerUsageSummary_MonthlyResetOnAnnualSub() {
	fx := s.newSummaryFixture("monthly", types.BILLING_PERIOD_ANNUAL,
		types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY, lo.ToPtr(int64(200)), 150)

	// Mid-month timestamps keep both events safely inside their calendar months.
	currentMonth15 := time.Date(s.now.Year(), s.now.Month(), 15, 12, 0, 0, 0, time.UTC)
	prevMonth15 := currentMonth15.AddDate(0, -1, 0)
	s.insertEvent(fx, prevMonth15, 100)   // previous month
	s.insertEvent(fx, currentMonth15, 50) // current month

	resp, err := s.service.GetCustomerUsageSummary(s.GetContext(), s.customer.ID, nil)
	s.NoError(err)

	f := s.featureSummary(resp, fx.feat.ID)
	s.NotNil(f)
	s.True(f.CurrentUsage.Equal(decimal.NewFromInt(50)),
		"monthly reset must only count the current month's usage (50), got %s", f.CurrentUsage)
}

func (s *BillingUsageSummarySuite) TestGetCustomerUsageSummary_NeverResetUsesCumulativeUsage() {
	fx := s.newSummaryFixture("never", types.BILLING_PERIOD_MONTHLY,
		types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER, lo.ToPtr(int64(1000)), 50)

	s.insertEvent(fx, s.now.Add(-10*24*time.Hour), 100) // before current period, after sub start
	s.insertEvent(fx, s.now.Add(-1*time.Hour), 50)      // in current period

	resp, err := s.service.GetCustomerUsageSummary(s.GetContext(), s.customer.ID, nil)
	s.NoError(err)

	f := s.featureSummary(resp, fx.feat.ID)
	s.NotNil(f)
	s.True(f.CurrentUsage.Equal(decimal.NewFromInt(150)),
		"never reset must count cumulative usage since subscription start (150), got %s", f.CurrentUsage)
}

func (s *BillingUsageSummarySuite) TestGetCustomerUsageSummary_FeatureFilters() {
	ctx := s.GetContext()
	// Two boolean features on one plan.
	pl := &plan.Plan{ID: "plan_bus_filters", Name: "Filter Plan", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))
	mkBool := func(id, lookupKey string) *feature.Feature {
		f := &feature.Feature{
			ID: id, Name: "Feature " + id, Type: types.FeatureTypeBoolean,
			LookupKey: lookupKey, BaseModel: types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().FeatureRepo.Create(ctx, f))
		e := &entitlement.Entitlement{
			ID: "ent_" + id, EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN, EntityID: pl.ID,
			FeatureID: f.ID, FeatureType: types.FeatureTypeBoolean, IsEnabled: true,
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		_, err := s.GetStores().EntitlementRepo.Create(ctx, e)
		s.NoError(err)
		return f
	}
	f1 := mkBool("feat_bus_f1", "lk_bus_f1")
	mkBool("feat_bus_f2", "lk_bus_f2")

	sub := &subscription.Subscription{
		ID: "sub_bus_filters", PlanID: pl.ID, CustomerID: s.customer.ID,
		StartDate:          s.now.Add(-30 * 24 * time.Hour),
		BillingAnchor:      s.now.Add(-48 * time.Hour),
		CurrentPeriodStart: s.now.Add(-48 * time.Hour),
		CurrentPeriodEnd:   s.now.Add(6 * 24 * time.Hour),
		Currency:           "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

	s.Run("feature_ids_filter_returns_only_matching_features", func() {
		resp, err := s.service.GetCustomerUsageSummary(ctx, s.customer.ID, &dto.GetCustomerUsageSummaryRequest{
			FeatureIDs: []string{f1.ID},
		})
		s.NoError(err)
		s.Len(resp.Features, 1)
		s.Equal(f1.ID, resp.Features[0].Feature.ID)
		// Boolean features have no limits and no usage.
		s.True(resp.Features[0].IsUnlimited)
		s.True(resp.Features[0].CurrentUsage.IsZero())
		s.Nil(resp.Features[0].NextUsageResetAt)
	})

	s.Run("feature_lookup_keys_are_resolved_to_feature_ids", func() {
		resp, err := s.service.GetCustomerUsageSummary(ctx, s.customer.ID, &dto.GetCustomerUsageSummaryRequest{
			FeatureLookupKeys: []string{"lk_bus_f1"},
		})
		s.NoError(err)
		f := s.featureSummary(resp, f1.ID)
		s.NotNil(f, "feature resolved via lookup key must be present")
		s.True(f.IsEnabled)
	})
}

func (s *BillingUsageSummarySuite) TestGetCustomerUsageSummary_SortsFeaturesByType() {
	ctx := s.GetContext()
	// Metered feature via fixture (sorted first) …
	fx := s.newSummaryFixture("sort", types.BILLING_PERIOD_MONTHLY,
		types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY, lo.ToPtr(int64(1000)), 10)

	// … plus one static and one boolean feature on the same plan.
	statFeat := &feature.Feature{
		ID: "feat_bus_static", Name: "A Static Feature", Type: types.FeatureTypeStatic,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, statFeat))
	_, err := s.GetStores().EntitlementRepo.Create(ctx, &entitlement.Entitlement{
		ID: "ent_bus_static", EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN, EntityID: fx.plan.ID,
		FeatureID: statFeat.ID, FeatureType: types.FeatureTypeStatic, IsEnabled: true,
		StaticValue: "gold", BaseModel: types.GetDefaultBaseModel(ctx),
	})
	s.NoError(err)

	boolFeat := &feature.Feature{
		ID: "feat_bus_bool", Name: "A Boolean Feature", Type: types.FeatureTypeBoolean,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, boolFeat))
	_, err = s.GetStores().EntitlementRepo.Create(ctx, &entitlement.Entitlement{
		ID: "ent_bus_bool", EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN, EntityID: fx.plan.ID,
		FeatureID: boolFeat.ID, FeatureType: types.FeatureTypeBoolean, IsEnabled: true,
		BaseModel: types.GetDefaultBaseModel(ctx),
	})
	s.NoError(err)

	resp, err := s.service.GetCustomerUsageSummary(ctx, s.customer.ID, nil)
	s.NoError(err)
	s.Len(resp.Features, 3)
	// Order: metered < static < boolean.
	s.Equal(fx.feat.ID, resp.Features[0].Feature.ID)
	s.Equal(statFeat.ID, resp.Features[1].Feature.ID)
	s.Equal(boolFeat.ID, resp.Features[2].Feature.ID)
}

func (s *BillingUsageSummarySuite) TestGetUsagePercent() {
	testCases := []struct {
		name     string
		usage    decimal.Decimal
		limit    *int64
		expected decimal.Decimal
	}{
		{
			name:     "nil_limit_returns_zero",
			usage:    decimal.NewFromInt(50),
			limit:    nil,
			expected: decimal.Zero,
		},
		{
			name:     "zero_limit_returns_hundred",
			usage:    decimal.NewFromInt(50),
			limit:    lo.ToPtr(int64(0)),
			expected: decimal.NewFromInt(100),
		},
		{
			name:     "negative_limit_returns_hundred",
			usage:    decimal.NewFromInt(50),
			limit:    lo.ToPtr(int64(-5)),
			expected: decimal.NewFromInt(100),
		},
		{
			name:     "usage_divided_by_limit",
			usage:    decimal.NewFromInt(50),
			limit:    lo.ToPtr(int64(200)),
			expected: decimal.RequireFromString("0.25"),
		},
	}
	for _, tc := range testCases {
		s.Run(tc.name, func() {
			got := s.billing.getUsagePercent(tc.usage, tc.limit)
			s.True(got.Equal(tc.expected), "want %s got %s", tc.expected, got)
		})
	}
}

func (s *BillingUsageSummarySuite) TestCalculateRemainingCommitment() {
	testCases := []struct {
		name       string
		usage      *dto.GetUsageBySubscriptionResponse
		commitment decimal.Decimal
		expected   decimal.Decimal
	}{
		{
			name:       "nil_usage_returns_zero",
			usage:      nil,
			commitment: decimal.NewFromInt(100),
			expected:   decimal.Zero,
		},
		{
			name:       "partial_utilization_returns_difference_with_decimals",
			usage:      &dto.GetUsageBySubscriptionResponse{CommitmentUtilized: 10.25},
			commitment: decimal.RequireFromString("25.75"),
			expected:   decimal.RequireFromString("15.5"),
		},
		{
			name:       "over_utilization_clamps_to_zero",
			usage:      &dto.GetUsageBySubscriptionResponse{CommitmentUtilized: 30},
			commitment: decimal.NewFromInt(20),
			expected:   decimal.Zero,
		},
	}
	for _, tc := range testCases {
		s.Run(tc.name, func() {
			got := s.billing.calculateRemainingCommitment(tc.usage, tc.commitment)
			s.True(got.Equal(tc.expected), "want %s got %s", tc.expected, got)
		})
	}
}

func (s *BillingUsageSummarySuite) TestAggregateStaticEntitlementsForBilling() {
	mkEnt := func(enabled bool, value string) *entitlement.Entitlement {
		return &entitlement.Entitlement{IsEnabled: enabled, StaticValue: value}
	}
	testCases := []struct {
		name            string
		entitlements    []*entitlement.Entitlement
		expectedEnabled bool
		expectedValues  []string
	}{
		{
			name:            "all_disabled_returns_disabled_with_no_values",
			entitlements:    []*entitlement.Entitlement{mkEnt(false, "silver")},
			expectedEnabled: false,
			expectedValues:  []string{},
		},
		{
			name:            "duplicate_values_are_deduplicated",
			entitlements:    []*entitlement.Entitlement{mkEnt(true, "gold"), mkEnt(true, "gold"), mkEnt(true, "silver")},
			expectedEnabled: true,
			expectedValues:  []string{"gold", "silver"},
		},
		{
			name:            "empty_static_values_are_skipped",
			entitlements:    []*entitlement.Entitlement{mkEnt(true, ""), mkEnt(true, "gold")},
			expectedEnabled: true,
			expectedValues:  []string{"gold"},
		},
	}
	for _, tc := range testCases {
		s.Run(tc.name, func() {
			got := aggregateStaticEntitlementsForBilling(tc.entitlements)
			s.Equal(tc.expectedEnabled, got.IsEnabled)
			s.Equal(tc.expectedValues, got.StaticValues)
		})
	}
}

func (s *BillingUsageSummarySuite) TestCalculateNeverResetUsage() {
	ctx := s.GetContext()
	fx := s.newSummaryFixture("nru", types.BILLING_PERIOD_MONTHLY,
		types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER, lo.ToPtr(int64(100)), 0)

	// 100 units before the current period (already billed), 500 in-period.
	s.insertEvent(fx, s.now.Add(-10*24*time.Hour), 100)
	s.insertEvent(fx, s.now.Add(-1*time.Hour), 500)

	eventService := NewEventService(
		s.billing.EventRepo, s.billing.MeterRepo, s.billing.EventPublisher,
		s.billing.Logger, s.billing.Config, s.billing.TracingSvc)

	testCases := []struct {
		name     string
		allowed  decimal.Decimal
		expected decimal.Decimal
	}{
		{
			// (600 - 100) - 50 = 450
			name:     "period_usage_minus_allowance_is_billable",
			allowed:  decimal.NewFromInt(50),
			expected: decimal.NewFromInt(450),
		},
		{
			// allowance larger than period usage → clamp at zero
			name:     "allowance_covering_usage_bills_zero",
			allowed:  decimal.NewFromInt(600),
			expected: decimal.Zero,
		},
	}
	for _, tc := range testCases {
		s.Run(tc.name, func() {
			got, err := s.billing.calculateNeverResetUsage(
				ctx, fx.sub, fx.item, []string{s.customer.ExternalID}, eventService,
				fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd, tc.allowed)
			s.NoError(err)
			s.True(got.Equal(tc.expected), "want %s got %s", tc.expected, got)
		})
	}
}
