package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
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

// BillingMeterUsageSuite exercises the meter_usage-backed billing path
// (billing_meter_usage.go): CalculateMeterUsageCharges and its helpers, plus
// the calculateMeterUsageCharges / calculateAllMeterUsageCharges pipeline.
//
// All fixtures use fixed dates so window bucketing (daily / monthly) is
// deterministic and independent of wall-clock time.
type BillingMeterUsageSuite struct {
	testutil.BaseServiceTestSuite
	service   BillingService
	billing   *billingService
	usageRepo *testutil.InMemoryMeterUsageStore
	data      struct {
		customer    *customer.Customer
		plan        *plan.Plan
		meterSum    *meter.Meter
		meterBucket *meter.Meter
		feature     *feature.Feature
		priceSum    *price.Price
		priceBucket *price.Price
		priceFixed  *price.Price
		sub         *subscription.Subscription
		usageItem   *subscription.SubscriptionLineItem
		bucketItem  *subscription.SubscriptionLineItem
		fixedItem   *subscription.SubscriptionLineItem
		periodStart time.Time
		periodEnd   time.Time
	}
}

func TestBillingMeterUsage(t *testing.T) {
	suite.Run(t, new(BillingMeterUsageSuite))
}

func (s *BillingMeterUsageSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.usageRepo = s.GetStores().MeterUsageRepo.(*testutil.InMemoryMeterUsageStore)
	s.service = NewBillingService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.billing = s.service.(*billingService)
	s.setupTestData()
}

func (s *BillingMeterUsageSuite) setupTestData() {
	ctx := s.GetContext()

	s.data.periodStart = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	s.data.periodEnd = time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)

	s.data.customer = &customer.Customer{
		ID:         "cust_bmu",
		ExternalID: "ext_bmu",
		Name:       "BMU Customer",
		Email:      "bmu@test.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.data.customer))

	s.data.plan = &plan.Plan{
		ID:        "plan_bmu",
		Name:      "BMU Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.data.plan))

	s.data.meterSum = &meter.Meter{
		ID:        "meter_bmu_sum",
		Name:      "BMU Sum Meter",
		EventName: "bmu_event",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "qty",
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.data.meterSum))

	s.data.meterBucket = &meter.Meter{
		ID:        "meter_bmu_bucket",
		Name:      "BMU Bucketed Meter",
		EventName: "bmu_bucket_event",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "qty",
			BucketSize: types.WindowSizeDay,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.data.meterBucket))

	s.data.feature = &feature.Feature{
		ID:        "feat_bmu_sum",
		Name:      "BMU Sum Feature",
		Type:      types.FeatureTypeMetered,
		MeterID:   s.data.meterSum.ID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, s.data.feature))

	// Tiered slab price: $0.02/unit up to 1000, $0.01 after.
	upTo1000 := uint64(1000)
	s.data.priceSum = &price.Price{
		ID:                 "price_bmu_sum",
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.data.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		TierMode:           types.BILLING_TIER_SLAB,
		MeterID:            s.data.meterSum.ID,
		Tiers: []price.PriceTier{
			{UpTo: &upTo1000, UnitAmount: decimal.RequireFromString("0.02")},
			{UpTo: nil, UnitAmount: decimal.RequireFromString("0.01")},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.data.priceSum))

	s.data.priceBucket = &price.Price{
		ID:                 "price_bmu_bucket",
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.data.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		TierMode:           types.BILLING_TIER_SLAB,
		MeterID:            s.data.meterBucket.ID,
		Tiers: []price.PriceTier{
			{UpTo: &upTo1000, UnitAmount: decimal.RequireFromString("0.02")},
			{UpTo: nil, UnitAmount: decimal.RequireFromString("0.01")},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.data.priceBucket))

	s.data.priceFixed = &price.Price{
		ID:                 "price_bmu_fixed",
		Amount:             decimal.NewFromInt(5),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.data.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.data.priceFixed))

	startDate := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	s.data.sub = &subscription.Subscription{
		ID:                 "sub_bmu",
		PlanID:             s.data.plan.ID,
		CustomerID:         s.data.customer.ID,
		StartDate:          startDate,
		BillingAnchor:      s.data.periodStart,
		CurrentPeriodStart: s.data.periodStart,
		CurrentPeriodEnd:   s.data.periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	s.data.usageItem = &subscription.SubscriptionLineItem{
		ID:               "li_bmu_sum",
		SubscriptionID:   s.data.sub.ID,
		CustomerID:       s.data.customer.ID,
		EntityID:         s.data.plan.ID,
		EntityType:       types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:  s.data.plan.Name,
		PriceID:          s.data.priceSum.ID,
		PriceType:        types.PRICE_TYPE_USAGE,
		MeterID:          s.data.meterSum.ID,
		MeterDisplayName: s.data.meterSum.Name,
		DisplayName:      "Sum Usage",
		Quantity:         decimal.Zero,
		Currency:         "usd",
		BillingPeriod:    types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence:   types.InvoiceCadenceArrear,
		StartDate:        startDate,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	s.data.bucketItem = &subscription.SubscriptionLineItem{
		ID:               "li_bmu_bucket",
		SubscriptionID:   s.data.sub.ID,
		CustomerID:       s.data.customer.ID,
		EntityID:         s.data.plan.ID,
		EntityType:       types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:  s.data.plan.Name,
		PriceID:          s.data.priceBucket.ID,
		PriceType:        types.PRICE_TYPE_USAGE,
		MeterID:          s.data.meterBucket.ID,
		MeterDisplayName: s.data.meterBucket.Name,
		DisplayName:      "Bucketed Usage",
		Quantity:         decimal.Zero,
		Currency:         "usd",
		BillingPeriod:    types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence:   types.InvoiceCadenceArrear,
		StartDate:        startDate,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	s.data.fixedItem = &subscription.SubscriptionLineItem{
		ID:              "li_bmu_fixed",
		SubscriptionID:  s.data.sub.ID,
		CustomerID:      s.data.customer.ID,
		EntityID:        s.data.plan.ID,
		EntityType:      types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName: s.data.plan.Name,
		PriceID:         s.data.priceFixed.ID,
		PriceType:       types.PRICE_TYPE_FIXED,
		DisplayName:     "Fixed Fee",
		Quantity:        decimal.NewFromInt(1),
		Currency:        "usd",
		BillingPeriod:   types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence:  types.InvoiceCadenceArrear,
		StartDate:       startDate,
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}

	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.data.sub,
		[]*subscription.SubscriptionLineItem{s.data.usageItem, s.data.fixedItem, s.data.bucketItem}))
	s.data.sub.LineItems = []*subscription.SubscriptionLineItem{s.data.usageItem, s.data.fixedItem, s.data.bucketItem}
}

// insertMeterUsage seeds one meter_usage row.
func (s *BillingMeterUsageSuite) insertMeterUsage(meterID, eventName string, ts time.Time, qty float64) {
	ctx := s.GetContext()
	s.NoError(s.usageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		{
			Event: events.Event{
				ID:                 types.GenerateUUID(),
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				ExternalCustomerID: s.data.customer.ExternalID,
				EventName:          eventName,
				Timestamp:          ts,
			},
			MeterID:  meterID,
			QtyTotal: decimal.NewFromFloat(qty),
		},
	}))
}

// createEntitlement creates a plan-level metered entitlement for the sum meter feature.
func (s *BillingMeterUsageSuite) createEntitlement(id string, limit *int64, reset types.EntitlementUsageResetPeriod) {
	ent := &entitlement.Entitlement{
		ID:               id,
		EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:         s.data.plan.ID,
		FeatureID:        s.data.feature.ID,
		FeatureType:      types.FeatureTypeMetered,
		IsEnabled:        true,
		UsageLimit:       limit,
		UsageResetPeriod: reset,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err := s.GetStores().EntitlementRepo.Create(s.GetContext(), ent)
	s.NoError(err)
}

// usageWithCharge builds a GetUsageBySubscriptionResponse carrying a single charge.
func usageWithCharge(charge *dto.SubscriptionUsageByMetersResponse) *dto.GetUsageBySubscriptionResponse {
	return &dto.GetUsageBySubscriptionResponse{
		Charges: []*dto.SubscriptionUsageByMetersResponse{charge},
	}
}

func (s *BillingMeterUsageSuite) sumCharge(qty float64) *dto.SubscriptionUsageByMetersResponse {
	return &dto.SubscriptionUsageByMetersResponse{
		SubscriptionLineItemID: s.data.usageItem.ID,
		MeterID:                s.data.meterSum.ID,
		MeterDisplayName:       s.data.meterSum.Name,
		Quantity:               qty,
		Currency:               "usd",
		Price:                  s.data.priceSum,
	}
}

// subWithItems returns a shallow copy of the base subscription restricted to
// the given line items — so tests never mutate the shared fixture.
func (s *BillingMeterUsageSuite) subWithItems(items ...*subscription.SubscriptionLineItem) *subscription.Subscription {
	subCopy := *s.data.sub
	subCopy.LineItems = items
	return &subCopy
}

func (s *BillingMeterUsageSuite) TestCalculateMeterUsageCharges_Basics() {
	ctx := s.GetContext()
	ps, pe := s.data.periodStart, s.data.periodEnd

	testCases := []struct {
		name          string
		usage         *dto.GetUsageBySubscriptionResponse
		expectedLen   int
		expectedTotal decimal.Decimal
		validate      func(charges []dto.CreateInvoiceLineItemRequest)
	}{
		{
			name:          "nil_usage_returns_no_charges_and_zero_total",
			usage:         nil,
			expectedLen:   0,
			expectedTotal: decimal.Zero,
		},
		{
			name: "charge_for_unknown_line_item_is_skipped",
			usage: usageWithCharge(&dto.SubscriptionUsageByMetersResponse{
				SubscriptionLineItemID: "li_does_not_exist",
				MeterID:                s.data.meterSum.ID,
				Quantity:               100,
				Price:                  s.data.priceSum,
			}),
			expectedLen:   0,
			expectedTotal: decimal.Zero,
		},
		{
			name: "charge_without_price_keeps_provided_amount",
			usage: usageWithCharge(&dto.SubscriptionUsageByMetersResponse{
				SubscriptionLineItemID: s.data.usageItem.ID,
				MeterID:                s.data.meterSum.ID,
				Quantity:               100,
				Amount:                 7.5,
			}),
			expectedLen:   1,
			expectedTotal: decimal.RequireFromString("7.5"),
			validate: func(charges []dto.CreateInvoiceLineItemRequest) {
				s.True(charges[0].Amount.Equal(decimal.RequireFromString("7.5")))
				s.True(charges[0].Quantity.Equal(decimal.NewFromInt(100)))
			},
		},
		{
			name: "recalculates_amount_from_price_when_no_entitlement",
			usage: usageWithCharge(func() *dto.SubscriptionUsageByMetersResponse {
				c := s.sumCharge(500)
				c.Amount = 999 // deliberately wrong; must be recomputed as 500 * $0.02
				return c
			}()),
			expectedLen:   1,
			expectedTotal: decimal.NewFromInt(10),
			validate: func(charges []dto.CreateInvoiceLineItemRequest) {
				c := charges[0]
				s.True(c.Amount.Equal(decimal.NewFromInt(10)), "amount should be 500 * 0.02 = 10, got %s", c.Amount)
				s.True(c.Quantity.Equal(decimal.NewFromInt(500)))
				s.Equal("Sum Usage (Usage Charge)", c.Metadata["description"])
				s.Equal(s.data.priceSum.ID, lo.FromPtr(c.PriceID))
				s.Equal(s.data.meterSum.ID, lo.FromPtr(c.MeterID))
				s.True(ps.Equal(lo.FromPtr(c.PeriodStart)))
				s.True(pe.Equal(lo.FromPtr(c.PeriodEnd)))
				s.Nil(c.AdjustedEntitlementQuantity)
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			charges, total, err := s.billing.CalculateMeterUsageCharges(
				ctx, s.subWithItems(s.data.usageItem), tc.usage, ps, pe, types.UsageSourceInvoiceCreation)
			s.NoError(err)
			s.Len(charges, tc.expectedLen)
			s.True(total.Equal(tc.expectedTotal), "expected total %s, got %s", tc.expectedTotal, total)
			if tc.validate != nil {
				tc.validate(charges)
			}
		})
	}
}

func (s *BillingMeterUsageSuite) TestCalculateMeterUsageCharges_MeterNotFoundReturnsError() {
	ctx := s.GetContext()
	missingMeterItem := &subscription.SubscriptionLineItem{
		ID:             "li_bmu_missing_meter",
		SubscriptionID: s.data.sub.ID,
		PriceID:        s.data.priceSum.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        "meter_missing",
		DisplayName:    "Missing Meter",
		Currency:       "usd",
		StartDate:      s.data.sub.StartDate,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	usage := usageWithCharge(&dto.SubscriptionUsageByMetersResponse{
		SubscriptionLineItemID: missingMeterItem.ID,
		MeterID:                "meter_missing",
		Quantity:               10,
		Price:                  s.data.priceSum,
	})

	charges, _, err := s.billing.CalculateMeterUsageCharges(
		ctx, s.subWithItems(missingMeterItem), usage, s.data.periodStart, s.data.periodEnd,
		types.UsageSourceInvoiceCreation)
	s.Error(err)
	s.Nil(charges)
	s.ErrorContains(err, "meter not found")
}

func (s *BillingMeterUsageSuite) TestCalculateMeterUsageCharges_EntitlementBillingPeriodReset() {
	ctx := s.GetContext()
	// Reset period == billing period (MONTHLY): billable = usage - allowance.
	s.createEntitlement("ent_bmu_bp", lo.ToPtr(int64(200)), types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)

	charges, total, err := s.billing.CalculateMeterUsageCharges(
		ctx, s.subWithItems(s.data.usageItem), usageWithCharge(s.sumCharge(500)),
		s.data.periodStart, s.data.periodEnd, types.UsageSourceInvoiceCreation)
	s.NoError(err)
	s.Len(charges, 1)

	// billable = 500 - 200 = 300 → 300 * $0.02 = $6
	s.True(charges[0].Quantity.Equal(decimal.NewFromInt(300)), "quantity should be 300, got %s", charges[0].Quantity)
	s.True(charges[0].Amount.Equal(decimal.NewFromInt(6)), "amount should be 6, got %s", charges[0].Amount)
	s.True(total.Equal(decimal.NewFromInt(6)))
	s.NotNil(charges[0].AdjustedEntitlementQuantity)
	s.True(charges[0].AdjustedEntitlementQuantity.Equal(decimal.NewFromInt(200)),
		"adjusted entitlement quantity should be 200, got %s", charges[0].AdjustedEntitlementQuantity)
}

func (s *BillingMeterUsageSuite) TestCalculateMeterUsageCharges_EntitlementUnlimitedZeroesCharge() {
	ctx := s.GetContext()
	s.createEntitlement("ent_bmu_unlimited", nil, types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)

	charges, total, err := s.billing.CalculateMeterUsageCharges(
		ctx, s.subWithItems(s.data.usageItem), usageWithCharge(s.sumCharge(500)),
		s.data.periodStart, s.data.periodEnd, types.UsageSourceInvoiceCreation)
	s.NoError(err)
	s.Len(charges, 1)
	s.True(charges[0].Amount.IsZero(), "unlimited entitlement must zero the amount, got %s", charges[0].Amount)
	s.True(charges[0].Quantity.IsZero())
	s.True(total.IsZero())
	s.NotNil(charges[0].AdjustedEntitlementQuantity)
	s.True(charges[0].AdjustedEntitlementQuantity.Equal(decimal.NewFromInt(500)))
}

func (s *BillingMeterUsageSuite) TestCalculateMeterUsageCharges_EntitlementDailyReset() {
	ctx := s.GetContext()
	s.createEntitlement("ent_bmu_daily", lo.ToPtr(int64(100)), types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY)

	// Day 1: 150 (overage 50), Day 2: 90 (no overage) → billable = 50.
	s.insertMeterUsage(s.data.meterSum.ID, "bmu_event", time.Date(2025, 6, 3, 10, 0, 0, 0, time.UTC), 150)
	s.insertMeterUsage(s.data.meterSum.ID, "bmu_event", time.Date(2025, 6, 10, 10, 0, 0, 0, time.UTC), 90)

	charges, total, err := s.billing.CalculateMeterUsageCharges(
		ctx, s.subWithItems(s.data.usageItem), usageWithCharge(s.sumCharge(240)),
		s.data.periodStart, s.data.periodEnd, types.UsageSourceInvoiceCreation)
	s.NoError(err)
	s.Len(charges, 1)
	s.True(charges[0].Quantity.Equal(decimal.NewFromInt(50)), "daily overage should be 50, got %s", charges[0].Quantity)
	s.True(charges[0].Amount.Equal(decimal.NewFromInt(1)), "amount should be 50 * 0.02 = 1, got %s", charges[0].Amount)
	s.True(total.Equal(decimal.NewFromInt(1)))
	s.Equal("daily", charges[0].Metadata["usage_reset_period"])
	s.NotNil(charges[0].AdjustedEntitlementQuantity)
	s.True(charges[0].AdjustedEntitlementQuantity.Equal(decimal.NewFromInt(190)),
		"adjusted qty should be 240 - 50 = 190, got %s", charges[0].AdjustedEntitlementQuantity)
}

func (s *BillingMeterUsageSuite) TestCalculateMeterUsageCharges_EntitlementMonthlyResetOnAnnualSub() {
	ctx := s.GetContext()
	s.createEntitlement("ent_bmu_monthly", lo.ToPtr(int64(100)), types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)

	// Annual subscription so MONTHLY reset != billing period → per-month overage path.
	annualStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	annualEnd := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Line item must start with the annual period so Jan/Feb usage is in range.
	annualItem := *s.data.usageItem
	annualItem.StartDate = annualStart
	subCopy := *s.data.sub
	subCopy.BillingPeriod = types.BILLING_PERIOD_ANNUAL
	subCopy.StartDate = annualStart
	subCopy.BillingAnchor = annualStart
	subCopy.CurrentPeriodStart = annualStart
	subCopy.CurrentPeriodEnd = annualEnd
	subCopy.LineItems = []*subscription.SubscriptionLineItem{&annualItem}

	// Jan: 150 (overage 50), Feb: 80 (no overage) → billable = 50.
	s.insertMeterUsage(s.data.meterSum.ID, "bmu_event", time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC), 150)
	s.insertMeterUsage(s.data.meterSum.ID, "bmu_event", time.Date(2025, 2, 15, 12, 0, 0, 0, time.UTC), 80)

	charges, total, err := s.billing.CalculateMeterUsageCharges(
		ctx, &subCopy, usageWithCharge(s.sumCharge(230)), annualStart, annualEnd,
		types.UsageSourceInvoiceCreation)
	s.NoError(err)
	s.Len(charges, 1)
	s.True(charges[0].Quantity.Equal(decimal.NewFromInt(50)), "monthly overage should be 50, got %s", charges[0].Quantity)
	s.True(charges[0].Amount.Equal(decimal.NewFromInt(1)), "amount should be 1, got %s", charges[0].Amount)
	s.True(total.Equal(decimal.NewFromInt(1)))
	s.Equal("monthly", charges[0].Metadata["usage_reset_period"])
}

func (s *BillingMeterUsageSuite) TestCalculateMeterUsageCharges_EntitlementNeverReset() {
	ctx := s.GetContext()
	s.createEntitlement("ent_bmu_never", lo.ToPtr(int64(100)), types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER)

	// Pre-period usage 300 (already billed), in-period usage 500.
	// billable = (800 - 300) - 100 = 400 → $8.
	s.insertMeterUsage(s.data.meterSum.ID, "bmu_event", time.Date(2025, 4, 10, 0, 0, 0, 0, time.UTC), 300)
	s.insertMeterUsage(s.data.meterSum.ID, "bmu_event", time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC), 500)

	charges, total, err := s.billing.CalculateMeterUsageCharges(
		ctx, s.subWithItems(s.data.usageItem), usageWithCharge(s.sumCharge(500)),
		s.data.periodStart, s.data.periodEnd, types.UsageSourceInvoiceCreation)
	s.NoError(err)
	s.Len(charges, 1)
	s.True(charges[0].Quantity.Equal(decimal.NewFromInt(400)), "never-reset billable should be 400, got %s", charges[0].Quantity)
	s.True(charges[0].Amount.Equal(decimal.NewFromInt(8)), "amount should be 8, got %s", charges[0].Amount)
	s.True(total.Equal(decimal.NewFromInt(8)))
	s.Equal("never", charges[0].Metadata["usage_reset_period"])
	s.NotNil(charges[0].AdjustedEntitlementQuantity)
	s.True(charges[0].AdjustedEntitlementQuantity.Equal(decimal.NewFromInt(100)))
}

func (s *BillingMeterUsageSuite) TestCalculateMeterUsageCharges_LineItemCommitment() {
	ctx := s.GetContext()
	ps, pe := s.data.periodStart, s.data.periodEnd

	// Bucketed usage for the windowed commitment case: Jun 3 max 10, Jun 4 max 20.
	s.insertMeterUsage(s.data.meterBucket.ID, "bmu_bucket_event", time.Date(2025, 6, 3, 8, 0, 0, 0, time.UTC), 10)
	s.insertMeterUsage(s.data.meterBucket.ID, "bmu_bucket_event", time.Date(2025, 6, 4, 8, 0, 0, 0, time.UTC), 20)

	commitItem := func(amount string, trueUp, windowed bool) *subscription.SubscriptionLineItem {
		item := *s.data.usageItem
		item.CommitmentAmount = lo.ToPtr(decimal.RequireFromString(amount))
		item.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
		item.CommitmentOverageFactor = lo.ToPtr(decimal.NewFromInt(2))
		item.CommitmentTrueUpEnabled = trueUp
		item.CommitmentWindowed = windowed
		return &item
	}

	testCases := []struct {
		name             string
		item             *subscription.SubscriptionLineItem
		charge           *dto.SubscriptionUsageByMetersResponse
		expectedAmount   decimal.Decimal
		expectedUtilized decimal.Decimal
		expectedOverage  decimal.Decimal
		expectedTrueUp   decimal.Decimal
	}{
		{
			// usage $10 over $5 commitment at 2x → 5 + (10-5)*2 = 15
			name:             "flat_commitment_overage_billed_at_overage_rate",
			item:             commitItem("5", false, false),
			charge:           s.sumCharge(500),
			expectedAmount:   decimal.NewFromInt(15),
			expectedUtilized: decimal.NewFromInt(5),
			expectedOverage:  decimal.NewFromInt(10),
			expectedTrueUp:   decimal.Zero,
		},
		{
			// usage $10 under $15 commitment with true-up → billed the full $15
			name:             "flat_commitment_trueup_bills_commitment",
			item:             commitItem("15", true, false),
			charge:           s.sumCharge(500),
			expectedAmount:   decimal.NewFromInt(15),
			expectedUtilized: decimal.NewFromInt(10),
			expectedOverage:  decimal.Zero,
			expectedTrueUp:   decimal.NewFromInt(5),
		},
		{
			// usage $10 under $15 commitment without true-up → billed usage only
			name:             "flat_commitment_without_trueup_bills_usage",
			item:             commitItem("15", false, false),
			charge:           s.sumCharge(500),
			expectedAmount:   decimal.NewFromInt(10),
			expectedUtilized: decimal.NewFromInt(10),
			expectedOverage:  decimal.Zero,
			expectedTrueUp:   decimal.Zero,
		},
		{
			// per-window commitment $0.30 at 2x: window1 $0.20 (under, no true-up) +
			// window2 $0.40 → 0.30 + 0.10*2 = $0.50 → total $0.70
			name: "windowed_commitment_applies_per_bucket",
			item: func() *subscription.SubscriptionLineItem {
				item := *s.data.bucketItem
				item.CommitmentAmount = lo.ToPtr(decimal.RequireFromString("0.3"))
				item.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
				item.CommitmentOverageFactor = lo.ToPtr(decimal.NewFromInt(2))
				item.CommitmentWindowed = true
				return &item
			}(),
			charge: &dto.SubscriptionUsageByMetersResponse{
				SubscriptionLineItemID: s.data.bucketItem.ID,
				MeterID:                s.data.meterBucket.ID,
				Quantity:               0,
				Currency:               "usd",
				Price:                  s.data.priceBucket,
			},
			expectedAmount:   decimal.RequireFromString("0.7"),
			expectedUtilized: decimal.RequireFromString("0.5"),
			expectedOverage:  decimal.RequireFromString("0.2"),
			expectedTrueUp:   decimal.Zero,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			charges, total, err := s.billing.CalculateMeterUsageCharges(
				ctx, s.subWithItems(tc.item), usageWithCharge(tc.charge), ps, pe,
				types.UsageSourceInvoiceCreation)
			s.NoError(err)
			s.Len(charges, 1)
			s.True(charges[0].Amount.Equal(tc.expectedAmount), "amount: want %s got %s", tc.expectedAmount, charges[0].Amount)
			s.True(total.Equal(tc.expectedAmount))
			info := charges[0].CommitmentInfo
			s.NotNil(info)
			s.True(info.ComputedCommitmentUtilizedAmount.Equal(tc.expectedUtilized),
				"utilized: want %s got %s", tc.expectedUtilized, info.ComputedCommitmentUtilizedAmount)
			s.True(info.ComputedOverageAmount.Equal(tc.expectedOverage),
				"overage: want %s got %s", tc.expectedOverage, info.ComputedOverageAmount)
			s.True(info.ComputedTrueUpAmount.Equal(tc.expectedTrueUp),
				"true-up: want %s got %s", tc.expectedTrueUp, info.ComputedTrueUpAmount)
		})
	}
}

func (s *BillingMeterUsageSuite) TestCalculateMeterUsageCharges_SubscriptionTrueUp() {
	ctx := s.GetContext()
	ps, pe := s.data.periodStart, s.data.periodEnd

	testCases := []struct {
		name           string
		enableTrueUp   bool
		hasOverage     bool
		utilized       float64
		expectedLen    int
		expectedTotal  decimal.Decimal
		validateTrueUp func(li dto.CreateInvoiceLineItemRequest)
	}{
		{
			name:          "trueup_added_when_commitment_underutilized",
			enableTrueUp:  true,
			utilized:      10,
			expectedLen:   2,
			expectedTotal: decimal.NewFromInt(20),
			validateTrueUp: func(li dto.CreateInvoiceLineItemRequest) {
				s.True(li.Amount.Equal(decimal.NewFromInt(10)), "true-up amount should be 20-10=10, got %s", li.Amount)
				s.Equal("true", li.Metadata["is_commitment_trueup"])
				s.Equal("20", li.Metadata["commitment_amount"])
				s.Equal("10", li.Metadata["commitment_utilized"])
				s.Equal("BMU Plan True Up", lo.FromPtr(li.DisplayName))
				s.True(li.Quantity.Equal(decimal.NewFromInt(1)))
			},
		},
		{
			name:          "no_trueup_when_usage_has_overage",
			enableTrueUp:  true,
			hasOverage:    true,
			utilized:      10,
			expectedLen:   1,
			expectedTotal: decimal.NewFromInt(10),
		},
		{
			name:          "no_trueup_when_trueup_disabled",
			enableTrueUp:  false,
			utilized:      10,
			expectedLen:   1,
			expectedTotal: decimal.NewFromInt(10),
		},
		{
			name:          "no_trueup_when_commitment_fully_utilized",
			enableTrueUp:  true,
			utilized:      20,
			expectedLen:   1,
			expectedTotal: decimal.NewFromInt(10),
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			subCopy := *s.data.sub
			subCopy.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(20))
			subCopy.OverageFactor = lo.ToPtr(decimal.NewFromInt(2))
			subCopy.EnableTrueUp = tc.enableTrueUp
			subCopy.LineItems = []*subscription.SubscriptionLineItem{s.data.usageItem}

			usage := usageWithCharge(s.sumCharge(500))
			usage.HasOverage = tc.hasOverage
			usage.CommitmentUtilized = tc.utilized

			charges, total, err := s.billing.CalculateMeterUsageCharges(
				ctx, &subCopy, usage, ps, pe, types.UsageSourceInvoiceCreation)
			s.NoError(err)
			s.Len(charges, tc.expectedLen)
			s.True(total.Equal(tc.expectedTotal), "total: want %s got %s", tc.expectedTotal, total)
			if tc.validateTrueUp != nil {
				tc.validateTrueUp(charges[len(charges)-1])
			}
		})
	}
}

func (s *BillingMeterUsageSuite) TestCalculateMeterUsageCharges_BucketedMeter() {
	ctx := s.GetContext()
	ps, pe := s.data.periodStart, s.data.periodEnd

	s.Run("prefetched_bucketed_result_is_used_without_querying", func() {
		// No meter_usage rows seeded — result must come from the charge itself.
		charge := &dto.SubscriptionUsageByMetersResponse{
			SubscriptionLineItemID: s.data.bucketItem.ID,
			MeterID:                s.data.meterBucket.ID,
			Currency:               "usd",
			Price:                  s.data.priceBucket,
			BucketedUsageResult: &events.AggregationResult{
				Value: decimal.NewFromInt(30),
				Results: []events.UsageResult{
					{WindowSize: time.Date(2025, 6, 3, 0, 0, 0, 0, time.UTC), Value: decimal.NewFromInt(10)},
					{WindowSize: time.Date(2025, 6, 4, 0, 0, 0, 0, time.UTC), Value: decimal.NewFromInt(20)},
				},
			},
		}
		charges, total, err := s.billing.CalculateMeterUsageCharges(
			ctx, s.subWithItems(s.data.bucketItem), usageWithCharge(charge), ps, pe,
			types.UsageSourceInvoiceCreation)
		s.NoError(err)
		s.Len(charges, 1)
		// per-bucket slab pricing: 10*0.02 + 20*0.02 = 0.6
		s.True(charges[0].Amount.Equal(decimal.RequireFromString("0.6")), "amount should be 0.6, got %s", charges[0].Amount)
		s.True(charges[0].Quantity.Equal(decimal.NewFromInt(30)))
		s.True(total.Equal(decimal.RequireFromString("0.6")))
	})

	s.Run("falls_back_to_direct_meter_usage_query", func() {
		// Jun 3: max(10, 7) = 10; Jun 4: max = 20 → same $0.6 as above.
		s.insertMeterUsage(s.data.meterBucket.ID, "bmu_bucket_event", time.Date(2025, 6, 3, 8, 0, 0, 0, time.UTC), 10)
		s.insertMeterUsage(s.data.meterBucket.ID, "bmu_bucket_event", time.Date(2025, 6, 3, 9, 0, 0, 0, time.UTC), 7)
		s.insertMeterUsage(s.data.meterBucket.ID, "bmu_bucket_event", time.Date(2025, 6, 4, 8, 0, 0, 0, time.UTC), 20)

		charge := &dto.SubscriptionUsageByMetersResponse{
			SubscriptionLineItemID: s.data.bucketItem.ID,
			MeterID:                s.data.meterBucket.ID,
			Currency:               "usd",
			Price:                  s.data.priceBucket,
		}
		charges, total, err := s.billing.CalculateMeterUsageCharges(
			ctx, s.subWithItems(s.data.bucketItem), usageWithCharge(charge), ps, pe,
			types.UsageSourceInvoiceCreation)
		s.NoError(err)
		s.Len(charges, 1)
		s.True(charges[0].Amount.Equal(decimal.RequireFromString("0.6")), "amount should be 0.6, got %s", charges[0].Amount)
		s.True(charges[0].Quantity.Equal(decimal.NewFromInt(30)))
		s.True(total.Equal(decimal.RequireFromString("0.6")))
	})
}

func (s *BillingMeterUsageSuite) TestCalculateMeterUsageCharges_BucketedMeterEntitlement() {
	ctx := s.GetContext()

	// Feature + entitlement bound to the bucketed meter: allowance of 5 units.
	featBucket := &feature.Feature{
		ID:        "feat_bmu_bucket",
		Name:      "BMU Bucket Feature",
		Type:      types.FeatureTypeMetered,
		MeterID:   s.data.meterBucket.ID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, featBucket))
	ent := &entitlement.Entitlement{
		ID:               "ent_bmu_bucket",
		EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:         s.data.plan.ID,
		FeatureID:        featBucket.ID,
		FeatureType:      types.FeatureTypeMetered,
		IsEnabled:        true,
		UsageLimit:       lo.ToPtr(int64(5)),
		UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().EntitlementRepo.Create(ctx, ent)
	s.NoError(err)

	charge := &dto.SubscriptionUsageByMetersResponse{
		SubscriptionLineItemID: s.data.bucketItem.ID,
		MeterID:                s.data.meterBucket.ID,
		Currency:               "usd",
		Price:                  s.data.priceBucket,
		BucketedUsageResult: &events.AggregationResult{
			Value: decimal.NewFromInt(30),
			Results: []events.UsageResult{
				{WindowSize: time.Date(2025, 6, 3, 0, 0, 0, 0, time.UTC), Value: decimal.NewFromInt(10)},
				{WindowSize: time.Date(2025, 6, 4, 0, 0, 0, 0, time.UTC), Value: decimal.NewFromInt(20)},
			},
		},
	}
	charges, total, err := s.billing.CalculateMeterUsageCharges(
		ctx, s.subWithItems(s.data.bucketItem), usageWithCharge(charge),
		s.data.periodStart, s.data.periodEnd, types.UsageSourceInvoiceCreation)
	s.NoError(err)
	s.Len(charges, 1)
	// total 30 - allowance 5 = 25 → 25 * $0.02 = $0.5 (aggregate-level adjustment)
	s.True(charges[0].Quantity.Equal(decimal.NewFromInt(25)), "quantity should be 25, got %s", charges[0].Quantity)
	s.True(charges[0].Amount.Equal(decimal.RequireFromString("0.5")), "amount should be 0.5, got %s", charges[0].Amount)
	s.True(total.Equal(decimal.RequireFromString("0.5")))
	s.NotNil(charges[0].AdjustedEntitlementQuantity)
	s.True(charges[0].AdjustedEntitlementQuantity.Equal(decimal.NewFromInt(5)))
}

func (s *BillingMeterUsageSuite) TestCalculateMeterUsageCharges_CumulativeCommitment() {
	ctx := s.GetContext()
	invoiceRepo := s.GetStores().InvoiceRepo.(*testutil.InMemoryInvoiceStore)

	// Monthly subscription with an ANNUAL $60 commitment at 2x overage.
	annualStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mkSub := func(ps, pe time.Time, enableTrueUp bool) *subscription.Subscription {
		subCopy := *s.data.sub
		subCopy.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(60))
		subCopy.OverageFactor = lo.ToPtr(decimal.NewFromInt(2))
		subCopy.CommitmentDuration = lo.ToPtr(types.BILLING_PERIOD_ANNUAL)
		subCopy.EnableTrueUp = enableTrueUp
		subCopy.StartDate = annualStart
		subCopy.CurrentPeriodStart = ps
		subCopy.CurrentPeriodEnd = pe
		subCopy.BillingAnchor = pe
		subCopy.LineItems = []*subscription.SubscriptionLineItem{s.data.usageItem}
		return &subCopy
	}

	mkPriorInvoice := func(id string, amount int64, ps, pe time.Time) *invoice.Invoice {
		return &invoice.Invoice{
			ID:             id,
			CustomerID:     s.data.customer.ID,
			SubscriptionID: lo.ToPtr(s.data.sub.ID),
			InvoiceType:    types.InvoiceTypeSubscription,
			InvoiceStatus:  types.InvoiceStatusFinalized,
			PaymentStatus:  types.PaymentStatusPending,
			Currency:       "usd",
			AmountDue:      decimal.NewFromInt(amount),
			PeriodStart:    &ps,
			PeriodEnd:      &pe,
			BaseModel:      types.GetDefaultBaseModel(ctx),
			LineItems: []*invoice.InvoiceLineItem{
				{
					ID:             id + "_li",
					InvoiceID:      id,
					CustomerID:     s.data.customer.ID,
					SubscriptionID: lo.ToPtr(s.data.sub.ID),
					PriceID:        lo.ToPtr(s.data.priceSum.ID),
					PriceType:      lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
					Amount:         decimal.NewFromInt(amount),
					Currency:       "usd",
					PeriodStart:    &ps,
					PeriodEnd:      &pe,
					BaseModel:      types.GetDefaultBaseModel(ctx),
				},
			},
		}
	}

	s.Run("mid_period_overage_split_between_commitment_and_overage_line", func() {
		invoiceRepo.Clear()
		// Prior invoices: $30 (Jan) + $20 (Feb) → prior base $50, remaining commitment $10.
		s.NoError(invoiceRepo.CreateWithLineItems(ctx, mkPriorInvoice("inv_bmu_1", 30,
			time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC))))
		s.NoError(invoiceRepo.CreateWithLineItems(ctx, mkPriorInvoice("inv_bmu_2", 20,
			time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC))))

		ps := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
		pe := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
		sub := mkSub(ps, pe, false)

		// Current usage 600 units → $12 base; within commitment $10, overage base $2 → $4 charged.
		charges, total, err := s.billing.CalculateMeterUsageCharges(
			ctx, sub, usageWithCharge(s.sumCharge(600)), ps, pe, types.UsageSourceInvoiceCreation)
		s.NoError(err)
		s.Len(charges, 2)
		s.True(total.Equal(decimal.NewFromInt(14)), "total should be 10 + 4 = 14, got %s", total)

		// Allocated usage line: $10 at prorated quantity 600 * 10/12 = 500.
		s.True(charges[0].Amount.Equal(decimal.NewFromInt(10)), "allocated amount should be 10, got %s", charges[0].Amount)
		s.True(charges[0].Quantity.Equal(decimal.NewFromInt(500)), "allocated quantity should be 500, got %s", charges[0].Quantity)

		// Overage line: $4 with overage metadata.
		overageLine := charges[1]
		s.True(overageLine.Amount.Equal(decimal.NewFromInt(4)), "overage amount should be 4, got %s", overageLine.Amount)
		s.Equal("true", overageLine.Metadata["is_overage"])
		s.Equal("2", overageLine.Metadata["overage_factor"])
		s.Equal("BMU Plan Overage", lo.FromPtr(overageLine.DisplayName))
	})

	s.Run("last_period_trueup_bills_remaining_commitment", func() {
		invoiceRepo.Clear()
		s.NoError(invoiceRepo.CreateWithLineItems(ctx, mkPriorInvoice("inv_bmu_3", 30,
			time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC))))

		ps := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
		pe := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		sub := mkSub(ps, pe, true)

		// Current $12, cumulative 30+12=42 < 60 → true-up $18 on the last period.
		charges, total, err := s.billing.CalculateMeterUsageCharges(
			ctx, sub, usageWithCharge(s.sumCharge(600)), ps, pe, types.UsageSourceInvoiceCreation)
		s.NoError(err)
		s.Len(charges, 2)
		s.True(total.Equal(decimal.NewFromInt(30)), "total should be 12 + 18 = 30, got %s", total)

		s.True(charges[0].Amount.Equal(decimal.NewFromInt(12)))
		trueUpLine := charges[1]
		s.True(trueUpLine.Amount.Equal(decimal.NewFromInt(18)), "true-up should be 60-42=18, got %s", trueUpLine.Amount)
		s.Equal("true", trueUpLine.Metadata["is_commitment_trueup"])
		s.Equal("60", trueUpLine.Metadata["commitment_amount"])
		s.Equal("BMU Plan True Up", lo.FromPtr(trueUpLine.DisplayName))
	})
}

func (s *BillingMeterUsageSuite) TestCalculateMeterUsageChargesPipeline() {
	ctx := s.GetContext()
	ps, pe := s.data.periodStart, s.data.periodEnd

	// 500 units in-period on the sum meter → $10 usage; fixed fee $5.
	s.insertMeterUsage(s.data.meterSum.ID, "bmu_event", time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC), 500)

	lineItems := []*subscription.SubscriptionLineItem{s.data.usageItem, s.data.fixedItem}

	s.Run("include_usage_combines_fixed_and_usage_charges", func() {
		result, err := s.billing.calculateMeterUsageCharges(ctx, s.data.sub, lineItems, ps, pe, true)
		s.NoError(err)
		s.Len(result.FixedCharges, 1)
		s.True(result.FixedCharges[0].Amount.Equal(decimal.NewFromInt(5)), "fixed charge should be 5, got %s", result.FixedCharges[0].Amount)
		s.Len(result.UsageCharges, 1)
		s.True(result.UsageCharges[0].Amount.Equal(decimal.NewFromInt(10)), "usage charge should be 10, got %s", result.UsageCharges[0].Amount)
		s.True(result.TotalAmount.Equal(decimal.NewFromInt(15)), "total should be 15, got %s", result.TotalAmount)
		s.Equal("usd", result.Currency)
	})

	s.Run("recompute_is_idempotent_and_does_not_double_bill", func() {
		first, err := s.billing.calculateMeterUsageCharges(ctx, s.data.sub, lineItems, ps, pe, true)
		s.NoError(err)
		second, err := s.billing.calculateMeterUsageCharges(ctx, s.data.sub, lineItems, ps, pe, true)
		s.NoError(err)
		s.True(first.TotalAmount.Equal(second.TotalAmount),
			"recompute changed total: %s vs %s", first.TotalAmount, second.TotalAmount)
		s.Len(second.UsageCharges, len(first.UsageCharges))
		s.True(second.TotalAmount.Equal(decimal.NewFromInt(15)))
	})

	s.Run("without_usage_returns_fixed_charges_only", func() {
		result, err := s.billing.calculateMeterUsageCharges(ctx, s.data.sub, lineItems, ps, pe, false)
		s.NoError(err)
		s.Len(result.FixedCharges, 1)
		s.Empty(result.UsageCharges)
		s.True(result.TotalAmount.Equal(decimal.NewFromInt(5)), "total should be fixed-only 5, got %s", result.TotalAmount)
	})
}
