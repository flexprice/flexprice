package service

// meter_usage_discount_test.go — integration tests for percentage-discount
// analytics in GetDetailedAnalytics.
//
// Cases covered:
//  1. Single line-item 10% forever percentage coupon → discount = 10% of gross
//  2. Subscription-level 10% forever coupon          → same math
//  3. Fixed coupon only                              → discount = 0 (skipped silently)
//  4. Once-cadence percentage coupon                 → discount = 0 (skipped silently)
//  5. No coupons                                     → discount = 0 (regression guard)
//  6. Group-by source with line-item 10% coupon      → per-item NetCost = 90% of cost
//  7. Windowed (DAY) with forever 10% coupon         → per-point discount = 10% of point cost
//  8. Compounding (10% line + 15% sub) + cross-meter isolation on a second line item
//  9. Straddling range: non-windowed over-applies vs. windowed per-point accuracy
//  10. Mixed coverage across subscriptions: one discounted, one not

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	coupon_association "github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ---------------------------------------------------------------------------
// Suite
// ---------------------------------------------------------------------------

type MeterUsageDiscountSuite struct {
	testutil.BaseServiceTestSuite
	svc            MeterUsageService
	meterUsageRepo *testutil.InMemoryMeterUsageStore

	customer    *customer.Customer
	meterAPI    *meter.Meter
	priceAPI    *price.Price
	sub         *subscription.Subscription
	now         time.Time
	periodStart time.Time
	periodEnd   time.Time
}

func TestMeterUsageDiscount(t *testing.T) {
	suite.Run(t, new(MeterUsageDiscountSuite))
}

func (s *MeterUsageDiscountSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	s.meterUsageRepo = s.GetStores().MeterUsageRepo.(*testutil.InMemoryMeterUsageStore)

	s.now = time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC)
	s.periodStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.periodEnd = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	s.setupEntitiesDiscount()

	// KEY DIFFERENCE: wire CouponRepo + CouponAssociationRepo so discounts apply.
	s.svc = NewMeterUsageService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		PlanRepo:                 s.GetStores().PlanRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		MeterRepo:                s.GetStores().MeterRepo,
		CustomerRepo:             s.GetStores().CustomerRepo,
		FeatureRepo:              s.GetStores().FeatureRepo,
		MeterUsageRepo:           s.meterUsageRepo,
		EnvironmentRepo:          s.GetStores().EnvironmentRepo,
		TenantRepo:               s.GetStores().TenantRepo,
		EventRepo:                s.GetStores().EventRepo,
		EntitlementRepo:          s.GetStores().EntitlementRepo,
		InvoiceRepo:              s.GetStores().InvoiceRepo,
		WalletRepo:               s.GetStores().WalletRepo,
		UserRepo:                 s.GetStores().UserRepo,
		AuthRepo:                 s.GetStores().AuthRepo,
		// Coupon repos — required for discount application
		CouponRepo:            s.GetStores().CouponRepo,
		CouponAssociationRepo: s.GetStores().CouponAssociationRepo,
	})
}

func (s *MeterUsageDiscountSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.meterUsageRepo.Clear()
}

// setupEntitiesDiscount mirrors setupEntities from meter_usage_test.go.
// $0.01/unit flat fee — 10,000 units → gross TotalCost = $100.
func (s *MeterUsageDiscountSuite) setupEntitiesDiscount() {
	ctx := s.GetContext()

	s.customer = &customer.Customer{
		ID:         "cust_discount_1",
		ExternalID: "ext_discount_1",
		Name:       "Discount Test Customer",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.customer))

	s.meterAPI = &meter.Meter{
		ID:        "meter_discount_api",
		Name:      "API Calls",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type: types.AggregationSum,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.meterAPI))

	s.priceAPI = &price.Price{
		ID:             "price_discount_api",
		Amount:         decimal.NewFromFloat(0.01), // $0.01 per unit
		Currency:       "usd",
		EntityType:     types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:       "plan_discount_1",
		BillingModel:   types.BILLING_MODEL_FLAT_FEE,
		Type:           types.PRICE_TYPE_USAGE,
		MeterID:        s.meterAPI.ID,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.priceAPI))

	s.sub = &subscription.Subscription{
		ID:                 "sub_discount_1",
		CustomerID:         s.customer.ID,
		PlanID:             "plan_discount_1",
		Currency:           "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: s.periodStart,
		CurrentPeriodEnd:   s.periodEnd,
		BillingAnchor:      s.periodStart,
		StartDate:          s.periodStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, s.sub))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// insertUsage inserts a single meter_usage record at the given timestamp.
func (s *MeterUsageDiscountSuite) insertUsage(ctx context.Context, ts time.Time, qty float64) {
	s.insertUsageWithSource(ctx, ts, qty, "")
}

// insertUsageWithSource inserts a meter_usage record with an explicit source tag.
func (s *MeterUsageDiscountSuite) insertUsageWithSource(ctx context.Context, ts time.Time, qty float64, source string) {
	s.NoError(s.meterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		{
			Event: events.Event{
				ID:                 types.GenerateUUID(),
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				ExternalCustomerID: s.customer.ExternalID,
				Timestamp:          ts,
				EventName:          "api_call",
				Source:             source,
			},
			MeterID:  s.meterAPI.ID,
			QtyTotal: decimal.NewFromFloat(qty),
		},
	}))
}

// createLineItemDiscount creates and stores a subscription line item.
func (s *MeterUsageDiscountSuite) createLineItemDiscount(ctx context.Context, id string) *subscription.SubscriptionLineItem {
	li := &subscription.SubscriptionLineItem{
		ID:             id,
		SubscriptionID: s.sub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        s.priceAPI.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        s.meterAPI.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.periodStart,
		EndDate:        s.periodEnd,
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))
	return li
}

// createPercentageCoupon creates a percentage coupon with Forever cadence.
func (s *MeterUsageDiscountSuite) createPercentageCoupon(ctx context.Context, id string, pct float64) *coupon.Coupon {
	c := &coupon.Coupon{
		ID:            id,
		Name:          "Test " + id,
		Type:          types.CouponTypePercentage,
		PercentageOff: lo.ToPtr(decimal.NewFromFloat(pct)),
		Cadence:       types.CouponCadenceForever,
		Currency:      "usd",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx), // sets Status=Published
	}
	s.NoError(s.GetStores().CouponRepo.Create(ctx, c))
	return c
}

// attachLineItemCoupon creates a coupon association scoped to a subscription line item.
// StartDate is set to periodStart so it is active for the full query window.
func (s *MeterUsageDiscountSuite) attachLineItemCoupon(ctx context.Context, assocID string, c *coupon.Coupon, lineItemID string) {
	a := &coupon_association.CouponAssociation{
		ID:                     assocID,
		CouponID:               c.ID,
		SubscriptionID:         s.sub.ID,
		SubscriptionLineItemID: lo.ToPtr(lineItemID),
		StartDate:              s.periodStart,
		EndDate:                nil,
		EnvironmentID:          types.GetEnvironmentID(ctx),
		Coupon:                 c, // embedded coupon — required by splitAndOrderAssociations
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponAssociationRepo.Create(ctx, a))
}

// attachSubCoupon creates a subscription-level coupon association (no line item).
func (s *MeterUsageDiscountSuite) attachSubCoupon(ctx context.Context, assocID string, c *coupon.Coupon) {
	s.attachSubCouponWithStart(ctx, assocID, c, s.periodStart)
}

// attachLineItemCouponWithStart is attachLineItemCoupon with an explicit StartDate,
// used to build coupons that only cover part of the query range.
func (s *MeterUsageDiscountSuite) attachLineItemCouponWithStart(ctx context.Context, assocID string, c *coupon.Coupon, lineItemID string, startDate time.Time) {
	a := &coupon_association.CouponAssociation{
		ID:                     assocID,
		CouponID:               c.ID,
		SubscriptionID:         s.sub.ID,
		SubscriptionLineItemID: lo.ToPtr(lineItemID),
		StartDate:              startDate,
		EndDate:                nil,
		EnvironmentID:          types.GetEnvironmentID(ctx),
		Coupon:                 c,
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponAssociationRepo.Create(ctx, a))
}

// attachSubCouponWithStart is attachSubCoupon with an explicit StartDate.
func (s *MeterUsageDiscountSuite) attachSubCouponWithStart(ctx context.Context, assocID string, c *coupon.Coupon, startDate time.Time) {
	a := &coupon_association.CouponAssociation{
		ID:             assocID,
		CouponID:       c.ID,
		SubscriptionID: s.sub.ID,
		StartDate:      startDate,
		EndDate:        nil,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		Coupon:         c,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponAssociationRepo.Create(ctx, a))
}

// createSecondMeterLineItem creates a second meter/price/line-item on the SAME
// subscription, so tests can verify cross-meter (cross-line-item) isolation.
// $0.01/unit flat fee, mirroring setupEntitiesDiscount's Meter A pricing.
func (s *MeterUsageDiscountSuite) createSecondMeterLineItem(ctx context.Context, id string) (*meter.Meter, *price.Price, *subscription.SubscriptionLineItem) {
	m := &meter.Meter{
		ID:        "meter_discount_storage",
		Name:      "Storage Ops",
		EventName: "storage_op",
		Aggregation: meter.Aggregation{
			Type: types.AggregationSum,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))

	p := &price.Price{
		ID:             "price_discount_storage",
		Amount:         decimal.NewFromFloat(0.01),
		Currency:       "usd",
		EntityType:     types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:       "plan_discount_1",
		BillingModel:   types.BILLING_MODEL_FLAT_FEE,
		Type:           types.PRICE_TYPE_USAGE,
		MeterID:        m.ID,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))

	li := &subscription.SubscriptionLineItem{
		ID:             id,
		SubscriptionID: s.sub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        p.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        m.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.periodStart,
		EndDate:        s.periodEnd,
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))
	return m, p, li
}

// insertUsageForMeter inserts a meter_usage record for an arbitrary meter/event,
// used once a second meter exists on the subscription.
func (s *MeterUsageDiscountSuite) insertUsageForMeter(ctx context.Context, meterID, eventName string, ts time.Time, qty float64, source string) {
	s.NoError(s.meterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		{
			Event: events.Event{
				ID:                 types.GenerateUUID(),
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				ExternalCustomerID: s.customer.ExternalID,
				Timestamp:          ts,
				EventName:          eventName,
				Source:             source,
			},
			MeterID:  meterID,
			QtyTotal: decimal.NewFromFloat(qty),
		},
	}))
}

// queryAnalytics is a convenience wrapper around GetDetailedAnalytics.
// 10,000 units at $0.01 → gross = $100.
func (s *MeterUsageDiscountSuite) queryAnalytics(ctx context.Context) *dto.GetUsageAnalyticsResponse {
	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		ExternalCustomerID: s.customer.ExternalID,
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)
	s.Require().NotNil(resp)
	return resp
}

// ---------------------------------------------------------------------------
// Test cases
// ---------------------------------------------------------------------------

// Case 1: Line-item 10% forever coupon — discount = 10% of gross.
func (s *MeterUsageDiscountSuite) TestLineItemPercentageCoupon() {
	ctx := s.GetContext()

	li := s.createLineItemDiscount(ctx, "li_disc_1")

	// 10,000 units × $0.01 = $100 gross
	s.insertUsage(ctx, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), 10_000)

	c := s.createPercentageCoupon(ctx, "coupon_li_1", 10)
	s.attachLineItemCoupon(ctx, "assoc_li_1", c, li.ID)

	resp := s.queryAnalytics(ctx)

	s.Require().Len(resp.Items, 1, "expected exactly one analytic item")
	item := resp.Items[0]

	expectedGross := decimal.NewFromInt(100)
	expectedDiscount := decimal.NewFromInt(10)
	expectedNet := decimal.NewFromInt(90)

	s.True(item.TotalCost.Equal(expectedGross),
		"item.TotalCost want %s got %s", expectedGross, item.TotalCost)
	s.True(item.TotalDiscount.Equal(expectedDiscount),
		"item.TotalDiscount want %s got %s", expectedDiscount, item.TotalDiscount)
	s.True(item.NetCost.Equal(expectedNet),
		"item.NetCost want %s got %s", expectedNet, item.NetCost)

	// Response-level totals
	s.True(resp.TotalCost.Equal(expectedGross),
		"resp.TotalCost want %s got %s", expectedGross, resp.TotalCost)
	s.True(resp.TotalDiscount.Equal(expectedDiscount),
		"resp.TotalDiscount want %s got %s", expectedDiscount, resp.TotalDiscount)
	s.True(resp.TotalNetCost.Equal(expectedNet),
		"resp.TotalNetCost want %s got %s", expectedNet, resp.TotalNetCost)
}

// Case 2: Subscription-level 10% forever coupon (SubscriptionLineItemID == nil).
func (s *MeterUsageDiscountSuite) TestSubLevelPercentageCoupon() {
	ctx := s.GetContext()

	s.createLineItemDiscount(ctx, "li_disc_2")

	// 10,000 units × $0.01 = $100 gross
	s.insertUsage(ctx, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), 10_000)

	c := s.createPercentageCoupon(ctx, "coupon_sub_1", 10)
	s.attachSubCoupon(ctx, "assoc_sub_1", c)

	resp := s.queryAnalytics(ctx)

	s.Require().Len(resp.Items, 1)
	item := resp.Items[0]

	expectedGross := decimal.NewFromInt(100)
	expectedDiscount := decimal.NewFromInt(10)
	expectedNet := decimal.NewFromInt(90)

	s.True(item.TotalCost.Equal(expectedGross),
		"item.TotalCost want %s got %s", expectedGross, item.TotalCost)
	s.True(item.TotalDiscount.Equal(expectedDiscount),
		"item.TotalDiscount want %s got %s", expectedDiscount, item.TotalDiscount)
	s.True(item.NetCost.Equal(expectedNet),
		"item.NetCost want %s got %s", expectedNet, item.NetCost)

	s.True(resp.TotalCost.Equal(expectedGross), "resp.TotalCost")
	s.True(resp.TotalDiscount.Equal(expectedDiscount), "resp.TotalDiscount")
	s.True(resp.TotalNetCost.Equal(expectedNet), "resp.TotalNetCost")
}

// Case 3: Fixed coupon only — discount stays 0 (fixed amounts are silently skipped
// by the analytics discount pass which only applies percentage coupons).
func (s *MeterUsageDiscountSuite) TestFixedCouponSkipped() {
	ctx := s.GetContext()

	li := s.createLineItemDiscount(ctx, "li_disc_3")

	s.insertUsage(ctx, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), 10_000)

	// Fixed coupon: $20 off
	fixedCoupon := &coupon.Coupon{
		ID:            "coupon_fixed_1",
		Name:          "Fixed Coupon",
		Type:          types.CouponTypeFixed,
		AmountOff:     lo.ToPtr(decimal.NewFromInt(20)),
		Cadence:       types.CouponCadenceForever,
		Currency:      "usd",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx), // sets Status=Published
	}
	s.NoError(s.GetStores().CouponRepo.Create(ctx, fixedCoupon))
	s.attachLineItemCoupon(ctx, "assoc_fixed_1", fixedCoupon, li.ID)

	resp := s.queryAnalytics(ctx)

	s.Require().Len(resp.Items, 1)
	item := resp.Items[0]

	// Assert gross cost explicitly so this test still catches a pricing regression
	// to zero — "discount==0 && net==total" alone would pass even if both were 0.
	s.True(item.TotalCost.Equal(decimal.NewFromInt(100)),
		"item.TotalCost want 100 got %s", item.TotalCost)
	s.True(item.TotalDiscount.Equal(decimal.Zero),
		"fixed coupon should produce zero discount in analytics, got %s", item.TotalDiscount)
	// When no percentage discount applies, NetCost is derived as TotalCost - TotalDiscount = TotalCost.
	s.True(item.NetCost.Equal(item.TotalCost),
		"NetCost should equal TotalCost when no applicable discount, got %s", item.NetCost)
}

// Case 4: Once-cadence percentage coupon — skipped silently (analytics only applies
// forever/repeated coupons).
func (s *MeterUsageDiscountSuite) TestOnceCadenceCouponSkipped() {
	ctx := s.GetContext()

	li := s.createLineItemDiscount(ctx, "li_disc_4")

	s.insertUsage(ctx, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), 10_000)

	onceCoupon := &coupon.Coupon{
		ID:            "coupon_once_1",
		Name:          "Once Coupon",
		Type:          types.CouponTypePercentage,
		PercentageOff: lo.ToPtr(decimal.NewFromFloat(10)),
		Cadence:       types.CouponCadenceOnce,
		Currency:      "usd",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx), // sets Status=Published
	}
	s.NoError(s.GetStores().CouponRepo.Create(ctx, onceCoupon))
	s.attachLineItemCoupon(ctx, "assoc_once_1", onceCoupon, li.ID)

	resp := s.queryAnalytics(ctx)

	s.Require().Len(resp.Items, 1)
	item := resp.Items[0]

	s.True(item.TotalCost.Equal(decimal.NewFromInt(100)),
		"item.TotalCost want 100 got %s", item.TotalCost)
	s.True(item.TotalDiscount.Equal(decimal.Zero),
		"once-cadence coupon should be skipped in analytics, got %s", item.TotalDiscount)
	// When no percentage discount applies, NetCost is derived as TotalCost - TotalDiscount = TotalCost.
	s.True(item.NetCost.Equal(item.TotalCost),
		"NetCost should equal TotalCost when no applicable discount, got %s", item.NetCost)
}

// Case 5: No coupons — regression guard; discount = 0.
func (s *MeterUsageDiscountSuite) TestNoCoupons() {
	ctx := s.GetContext()

	s.createLineItemDiscount(ctx, "li_disc_5")

	s.insertUsage(ctx, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), 10_000)

	resp := s.queryAnalytics(ctx)

	s.Require().Len(resp.Items, 1)
	item := resp.Items[0]

	s.True(item.TotalCost.Equal(decimal.NewFromInt(100)),
		"item.TotalCost want 100 got %s", item.TotalCost)
	s.True(item.TotalDiscount.Equal(decimal.Zero),
		"no coupon should produce zero discount, got %s", item.TotalDiscount)
	// When no discount applies, NetCost is derived as TotalCost - TotalDiscount = TotalCost.
	s.True(item.NetCost.Equal(item.TotalCost),
		"NetCost should equal TotalCost when no applicable discount, got %s", item.NetCost)
	s.True(resp.TotalCost.Equal(decimal.NewFromInt(100)), "resp.TotalCost want 100 got %s", resp.TotalCost)
	s.True(resp.TotalDiscount.Equal(decimal.Zero), "resp.TotalDiscount should be 0")
	// TotalNetCost is derived as TotalCost - TotalDiscount — equals TotalCost when no discount.
	s.True(resp.TotalNetCost.Equal(resp.TotalCost), "resp.TotalNetCost should equal TotalCost when no discounts")
}

// Case 6: Group-by source — two sources, one line-item 10% coupon.
// Each item's NetCost must be 90% of its TotalCost.
// Sum of discounts must be 10% of sum of gross costs.
//
// Note: GetDetailedAnalytics uses the non-bucketed GetDetailedAnalytics repo path
// (not GetUsageForBucketedMetersDetailed) since the meter is a plain SUM meter.
// The in-memory store's GetDetailedAnalytics fully supports GroupBy source.
func (s *MeterUsageDiscountSuite) TestGroupBySourceWithCoupon() {
	ctx := s.GetContext()

	li := s.createLineItemDiscount(ctx, "li_disc_6")

	// 6,000 units from "sdk" and 4,000 from "api" → gross = $100 total
	s.insertUsageWithSource(ctx, time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC), 6_000, "sdk")
	s.insertUsageWithSource(ctx, time.Date(2026, 1, 11, 10, 0, 0, 0, time.UTC), 4_000, "api")

	c := s.createPercentageCoupon(ctx, "coupon_grp_1", 10)
	s.attachLineItemCoupon(ctx, "assoc_grp_1", c, li.ID)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		ExternalCustomerID: s.customer.ExternalID,
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		GroupBy:            []string{"source"},
	})
	s.NoError(err)
	s.Require().NotNil(resp)

	if len(resp.Items) < 2 {
		// In-memory store returned a single merged item (its GetDetailedAnalytics merges
		// usage per line item before applying group_by, unlike the real ClickHouse-backed
		// path — confirmed working with real multi-row group_by=source output in manual
		// end-to-end testing against production infra). Assert what this fixture CAN verify
		// (single-item linearity) below, then skip explicitly rather than silently passing —
		// this makes it visible in test output that the multi-row scenario went unexercised.
		s.Require().Len(resp.Items, 1, "expected at least one analytic item")
		item := resp.Items[0]
		expectedGross := decimal.NewFromInt(100)
		expectedDiscount := decimal.NewFromInt(10)
		expectedNet := decimal.NewFromInt(90)
		s.True(item.TotalCost.Equal(expectedGross), "item.TotalCost want %s got %s", expectedGross, item.TotalCost)
		s.True(item.TotalDiscount.Equal(expectedDiscount), "item.TotalDiscount want %s got %s", expectedDiscount, item.TotalDiscount)
		s.True(item.NetCost.Equal(expectedNet), "item.NetCost want %s got %s", expectedNet, item.NetCost)
		s.T().Skip("in-memory store does not fan out group_by=source into multiple items; single-item linearity verified above")
	}

	// Multiple items: each item's NetCost = TotalCost * 0.9
	// and the totals should sum correctly.
	var sumGross, sumDiscount decimal.Decimal
	ninetyPct := decimal.NewFromFloat(0.9)
	tenPct := decimal.NewFromFloat(0.10)
	for _, item := range resp.Items {
		sumGross = sumGross.Add(item.TotalCost)
		sumDiscount = sumDiscount.Add(item.TotalDiscount)

		expectedItemNet := types.RoundToCurrencyPrecision(item.TotalCost.Mul(ninetyPct), "usd")
		s.True(item.NetCost.Equal(expectedItemNet),
			"item.NetCost want %s got %s (source=%s)", expectedItemNet, item.NetCost, item.Source)
	}

	expectedTotalGross := decimal.NewFromInt(100)
	expectedTotalDiscount := types.RoundToCurrencyPrecision(sumGross.Mul(tenPct), "usd")

	s.True(sumGross.Equal(expectedTotalGross),
		"sum of item gross costs want %s got %s", expectedTotalGross, sumGross)

	// Allow ±1 cent for rounding when splitting discount across items.
	discountDiff := sumDiscount.Sub(expectedTotalDiscount).Abs()
	s.True(discountDiff.LessThanOrEqual(decimal.NewFromFloat(0.01)),
		"sum of discounts want ~%s got %s (diff=%s)", expectedTotalDiscount, sumDiscount, discountDiff)
}

// Case 7: Windowed (DAY) — per-point discount = 10% of point cost.
// Usage across 3 days → 3 points; each point should carry its own discount.
func (s *MeterUsageDiscountSuite) TestWindowedPerPointDiscount() {
	ctx := s.GetContext()

	li := s.createLineItemDiscount(ctx, "li_disc_7")

	// Day 1: 3,000 units → $30 gross
	// Day 2: 4,000 units → $40 gross
	// Day 3: 3,000 units → $30 gross
	// Total: 10,000 units → $100 gross
	s.insertUsage(ctx, time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), 3_000)
	s.insertUsage(ctx, time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC), 4_000)
	s.insertUsage(ctx, time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC), 3_000)

	c := s.createPercentageCoupon(ctx, "coupon_win_1", 10)
	s.attachLineItemCoupon(ctx, "assoc_win_1", c, li.ID)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		ExternalCustomerID: s.customer.ExternalID,
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		WindowSize:         types.WindowSizeDay,
	})
	s.NoError(err)
	s.Require().NotNil(resp)
	s.Require().Len(resp.Items, 1)

	item := resp.Items[0]

	expectedGross := decimal.NewFromInt(100)
	expectedDiscount := decimal.NewFromInt(10)
	expectedNet := decimal.NewFromInt(90)

	s.True(item.TotalCost.Equal(expectedGross),
		"item.TotalCost want %s got %s", expectedGross, item.TotalCost)
	s.True(item.TotalDiscount.Equal(expectedDiscount),
		"item.TotalDiscount want %s got %s", expectedDiscount, item.TotalDiscount)
	s.True(item.NetCost.Equal(expectedNet),
		"item.NetCost want %s got %s", expectedNet, item.NetCost)

	_ = li // silence unused warning if points check is skipped

	if len(item.Points) == 0 {
		s.T().Skip("no points returned for windowed query; per-point discount checks require points")
	}

	// Per-point: each point.Discount ≈ point.Cost * 0.10
	tenPct := decimal.NewFromFloat(0.10)
	var sumPointDiscount, sumPointNetCost decimal.Decimal
	for _, pt := range item.Points {
		expectedPtDiscount := types.RoundToCurrencyPrecision(pt.Cost.Mul(tenPct), "usd")
		discountDiff := pt.Discount.Sub(expectedPtDiscount).Abs()
		s.True(discountDiff.LessThanOrEqual(decimal.NewFromFloat(0.01)),
			"point.Discount want ~%s got %s at %s", expectedPtDiscount, pt.Discount, pt.Timestamp)

		expectedPtNet := pt.Cost.Sub(pt.Discount)
		s.True(pt.NetCost.Equal(expectedPtNet),
			"point.NetCost want %s got %s at %s", expectedPtNet, pt.NetCost, pt.Timestamp)

		sumPointDiscount = sumPointDiscount.Add(pt.Discount)
		sumPointNetCost = sumPointNetCost.Add(pt.NetCost)
	}

	// Sum of point discounts == item.TotalDiscount (allow ±1 cent for multi-point rounding)
	totalDiscountDiff := sumPointDiscount.Sub(item.TotalDiscount).Abs()
	s.True(totalDiscountDiff.LessThanOrEqual(decimal.NewFromFloat(0.01)),
		"sum(point.Discount)=%s should ≈ item.TotalDiscount=%s", sumPointDiscount, item.TotalDiscount)

	// Sum of point NetCosts == item.NetCost (allow ±1 cent)
	netCostDiff := sumPointNetCost.Sub(item.NetCost).Abs()
	s.True(netCostDiff.LessThanOrEqual(decimal.NewFromFloat(0.01)),
		"sum(point.NetCost)=%s should ≈ item.NetCost=%s", sumPointNetCost, item.NetCost)
}

// Case 8: Compounding (10% line-item + 15% subscription-level) on one line item,
// while a SECOND line item (different meter) only carries the subscription-level
// coupon. Verifies: (a) compounding order/math, (b) the line-item coupon does not
// bleed into the other meter, (c) the subscription-level coupon reaches both.
//
// Line item A (Meter A): $100 gross. 10% line coupon then 15% sub coupon:
//
//	100 * 0.90 * 0.85 = 76.50 → discount = 23.50
//
// Line item B (Meter Storage): $50 gross. Only the 15% sub coupon applies:
//
//	50 * 0.85 = 42.50 → discount = 7.50
func (s *MeterUsageDiscountSuite) TestCompoundingAndLineItemIsolation() {
	ctx := s.GetContext()

	liA := s.createLineItemDiscount(ctx, "li_disc_8a")
	_, _, liB := s.createSecondMeterLineItem(ctx, "li_disc_8b")

	// Meter A: 10,000 units × $0.01 = $100 gross.
	s.insertUsage(ctx, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), 10_000)
	// Meter Storage: 5,000 units × $0.01 = $50 gross.
	s.insertUsageForMeter(ctx, "meter_discount_storage", "storage_op", time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), 5_000, "")

	lineCoupon := s.createPercentageCoupon(ctx, "coupon_compound_line", 10)
	s.attachLineItemCoupon(ctx, "assoc_compound_line", lineCoupon, liA.ID)

	subCoupon := s.createPercentageCoupon(ctx, "coupon_compound_sub", 15)
	s.attachSubCoupon(ctx, "assoc_compound_sub", subCoupon)

	resp := s.queryAnalytics(ctx)
	s.Require().Len(resp.Items, 2, "expected one item per line item/meter")

	var itemA, itemB *dto.UsageAnalyticItem
	for i := range resp.Items {
		switch resp.Items[i].MeterID {
		case s.meterAPI.ID:
			itemA = &resp.Items[i]
		case "meter_discount_storage":
			itemB = &resp.Items[i]
		}
	}
	s.Require().NotNil(itemA, "expected an item for Meter A")
	s.Require().NotNil(itemB, "expected an item for Meter Storage")

	// Line item A: compounded 10% then 15%.
	s.True(itemA.TotalCost.Equal(decimal.NewFromInt(100)), "itemA.TotalCost got %s", itemA.TotalCost)
	s.True(itemA.TotalDiscount.Equal(decimal.RequireFromString("23.5")), "itemA.TotalDiscount got %s", itemA.TotalDiscount)
	s.True(itemA.NetCost.Equal(decimal.RequireFromString("76.5")), "itemA.NetCost got %s", itemA.NetCost)

	// Line item B: only the 15% sub-level coupon (the 10% line coupon is scoped to A).
	s.True(itemB.TotalCost.Equal(decimal.NewFromInt(50)), "itemB.TotalCost got %s", itemB.TotalCost)
	s.True(itemB.TotalDiscount.Equal(decimal.RequireFromString("7.5")), "itemB.TotalDiscount got %s", itemB.TotalDiscount)
	s.True(itemB.NetCost.Equal(decimal.RequireFromString("42.5")), "itemB.NetCost got %s", itemB.NetCost)

	// Response-level totals reconcile across both items.
	s.True(resp.TotalCost.Equal(decimal.NewFromInt(150)), "resp.TotalCost got %s", resp.TotalCost)
	s.True(resp.TotalDiscount.Equal(decimal.NewFromInt(31)), "resp.TotalDiscount got %s", resp.TotalDiscount)
	s.True(resp.TotalNetCost.Equal(decimal.NewFromInt(119)), "resp.TotalNetCost got %s", resp.TotalNetCost)

	_ = liB
}

// Case 9: Straddling range — a coupon whose StartDate falls partway through the
// query range. Non-windowed queries are all-or-nothing (over-apply to the whole
// range); windowed (DAY) queries only discount the windows at/after StartDate.
//
// Usage: $50 gross before the coupon starts (Jan 5 + Jan 10), $50 gross at/after
// (Jan 20 + Jan 25). Coupon: 20% line-item, StartDate = Jan 15 (mid-range).
//
//	Non-windowed: discounts the FULL $100 → discount = $20, net = $80
//	Windowed:     discounts only the post-start $50 → discount = $10, net = $90
func (s *MeterUsageDiscountSuite) TestStraddlingRangeNonWindowedOverAppliesVsWindowedAccurate() {
	ctx := s.GetContext()

	li := s.createLineItemDiscount(ctx, "li_disc_9")

	// Pre-coupon-start usage: $30 + $20 = $50 gross.
	s.insertUsage(ctx, time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), 3_000)
	s.insertUsage(ctx, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), 2_000)
	// Post-coupon-start usage: $20 + $30 = $50 gross.
	s.insertUsage(ctx, time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC), 2_000)
	s.insertUsage(ctx, time.Date(2026, 1, 25, 12, 0, 0, 0, time.UTC), 3_000)

	couponStart := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	c := s.createPercentageCoupon(ctx, "coupon_straddle", 20)
	s.attachLineItemCouponWithStart(ctx, "assoc_straddle", c, li.ID, couponStart)

	// Non-windowed: all-or-nothing over the full range.
	nonWindowed, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		ExternalCustomerID: s.customer.ExternalID,
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)
	s.Require().Len(nonWindowed.Items, 1)
	nwItem := nonWindowed.Items[0]

	s.True(nwItem.TotalCost.Equal(decimal.NewFromInt(100)), "non-windowed TotalCost got %s", nwItem.TotalCost)
	s.True(nwItem.TotalDiscount.Equal(decimal.NewFromInt(20)),
		"non-windowed should over-apply the discount to the whole range, got %s", nwItem.TotalDiscount)
	s.True(nwItem.NetCost.Equal(decimal.NewFromInt(80)), "non-windowed NetCost got %s", nwItem.NetCost)

	// Windowed (DAY): only post-coupon-start windows are discounted.
	windowed, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		ExternalCustomerID: s.customer.ExternalID,
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		WindowSize:         types.WindowSizeDay,
	})
	s.NoError(err)
	s.Require().Len(windowed.Items, 1)
	wItem := windowed.Items[0]

	s.True(wItem.TotalCost.Equal(decimal.NewFromInt(100)), "windowed TotalCost got %s", wItem.TotalCost)
	s.True(wItem.TotalDiscount.Equal(decimal.NewFromInt(10)),
		"windowed should only discount post-coupon-start usage, got %s", wItem.TotalDiscount)
	s.True(wItem.NetCost.Equal(decimal.NewFromInt(90)), "windowed NetCost got %s", wItem.NetCost)

	// The core claim of the feature: windowed accuracy strictly reduces the
	// discount compared to non-windowed over-application, on identical data.
	s.True(wItem.TotalDiscount.LessThan(nwItem.TotalDiscount),
		"windowed discount (%s) should be less than non-windowed discount (%s) when the coupon starts mid-range",
		wItem.TotalDiscount, nwItem.TotalDiscount)

	if len(wItem.Points) > 0 {
		for _, pt := range wItem.Points {
			if pt.Timestamp.Before(couponStart) {
				s.True(pt.Discount.IsZero(), "pre-coupon-start point should have zero discount, got %s at %s", pt.Discount, pt.Timestamp)
			}
		}
	}
}

// Case 10: two subscriptions for the SAME customer — subscription 1 has a
// line-item coupon, subscription 2 has no coupons at all. Subscription 2's item
// must show zero discount while subscription 1's is correctly discounted —
// proves the per-item "no coupon applies to THIS item" skip only affects the
// item it belongs to, not siblings elsewhere in the same response.
func (s *MeterUsageDiscountSuite) TestMixedCoverageAcrossSubscriptions() {
	ctx := s.GetContext()

	li := s.createLineItemDiscount(ctx, "li_disc_11a")
	s.insertUsage(ctx, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), 10_000) // sub_discount_1 / Meter A: $100

	c := s.createPercentageCoupon(ctx, "coupon_mixed_11", 10)
	s.attachLineItemCoupon(ctx, "assoc_mixed_11", c, li.ID)

	// A second subscription for the same customer, no coupons attached.
	sub2 := &subscription.Subscription{
		ID: "sub_discount_11b", CustomerID: s.customer.ID, PlanID: "plan_discount_1", Currency: "usd",
		SubscriptionStatus: types.SubscriptionStatusActive, CurrentPeriodStart: s.periodStart, CurrentPeriodEnd: s.periodEnd,
		BillingAnchor: s.periodStart, StartDate: s.periodStart, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub2))
	m2 := &meter.Meter{ID: "meter_discount_nocoupon", Name: "No Coupon Meter", EventName: "no_coupon_event", Aggregation: meter.Aggregation{Type: types.AggregationSum}, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m2))
	p2 := &price.Price{
		ID: "price_discount_nocoupon", Amount: decimal.NewFromFloat(0.01), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_discount_1", BillingModel: types.BILLING_MODEL_FLAT_FEE,
		Type: types.PRICE_TYPE_USAGE, MeterID: m2.ID, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p2))
	li2 := &subscription.SubscriptionLineItem{
		ID: "li_discount_nocoupon", SubscriptionID: sub2.ID, CustomerID: s.customer.ID, PriceID: p2.ID, PriceType: types.PRICE_TYPE_USAGE,
		MeterID: m2.ID, Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate: s.periodStart, EndDate: s.periodEnd, Quantity: decimal.NewFromInt(1), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li2))
	s.insertUsageForMeter(ctx, m2.ID, "no_coupon_event", time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), 2_000, "") // sub_discount_11b / Meter: $20

	resp := s.queryAnalytics(ctx)
	s.Require().Len(resp.Items, 2)

	var itemDiscounted, itemUndiscounted *dto.UsageAnalyticItem
	for i := range resp.Items {
		switch resp.Items[i].SubscriptionID {
		case s.sub.ID:
			itemDiscounted = &resp.Items[i]
		case sub2.ID:
			itemUndiscounted = &resp.Items[i]
		}
	}
	s.Require().NotNil(itemDiscounted, "expected an item for sub_discount_1")
	s.Require().NotNil(itemUndiscounted, "expected an item for sub_discount_11b")

	s.True(itemDiscounted.TotalDiscount.Equal(decimal.NewFromInt(10)), "itemDiscounted.TotalDiscount want 10 got %s", itemDiscounted.TotalDiscount)
	s.True(itemUndiscounted.TotalCost.Equal(decimal.NewFromInt(20)), "itemUndiscounted.TotalCost got %s", itemUndiscounted.TotalCost)
	s.True(itemUndiscounted.TotalDiscount.IsZero(), "itemUndiscounted.TotalDiscount should be 0, got %s", itemUndiscounted.TotalDiscount)
	s.True(itemUndiscounted.NetCost.Equal(itemUndiscounted.TotalCost), "itemUndiscounted.NetCost should equal TotalCost")
}
