package service

// feature_usage_discount_test.go — white-box integration test for percentage
// discounts on the feature-usage analytics path (buildAnalyticsResponse).
//
// A full black-box test through the public GetDetailedUsageAnalytics entry
// point isn't possible yet: InMemoryFeatureUsageStore.GetDetailedUsageAnalytics
// is a hard-coded stub that always returns an empty slice, so fetchAnalyticsData
// can never populate data.Analytics via the in-memory double (see
// internal/testutil/inmemory_feature_usage_store.go). Building a faithful
// ClickHouse-equivalent fake for that repo is a separate, larger effort.
//
// Instead, this test calls the private buildAnalyticsResponse directly with a
// hand-built AnalyticsData, exercising the REAL production code path —
// calculateCosts -> applyAnalyticsDiscounts -> aggregateAnalyticsByGrouping ->
// ToGetUsageAnalyticsResponseDTO — without needing to fake ClickHouse SQL
// semantics. This is a legitimate substitute per the task's escalation
// instructions: it verifies the actual wiring, just skips the repo-fetch step.

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	coupon_association "github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type FeatureUsageDiscountSuite struct {
	testutil.BaseServiceTestSuite
	svc *featureUsageTrackingService
}

func TestFeatureUsageDiscount(t *testing.T) {
	suite.Run(t, new(FeatureUsageDiscountSuite))
}

func (s *FeatureUsageDiscountSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	// Constructed directly (bypassing NewFeatureUsageTrackingService's Kafka/pubsub
	// setup, which buildAnalyticsResponse never touches) with the coupon repos wired
	// so discounts apply — same wiring approach as MeterUsageDiscountSuite.
	s.svc = &featureUsageTrackingService{
		ServiceParams: ServiceParams{
			Logger:                s.GetLogger(),
			Config:                s.GetConfig(),
			DB:                    s.GetDB(),
			CouponRepo:            s.GetStores().CouponRepo,
			CouponAssociationRepo: s.GetStores().CouponAssociationRepo,
			// SettingsRepo is needed by ToGetUsageAnalyticsResponseDTO's (unrelated)
			// custom-analytics-config lookup, which no-ops gracefully when no such
			// setting exists but nil-panics if the repo itself is nil.
			SettingsRepo: s.GetStores().SettingsRepo,
		},
	}
}

// buildData constructs a minimal AnalyticsData for one feature/meter/price/line
// item with the given raw usage quantity. $0.01/unit flat fee, mirroring the
// sibling meter-usage discount suite's pricing so the math is easy to verify:
// 10,000 units -> $100 gross.
func (s *FeatureUsageDiscountSuite) buildData(ctx context.Context, subID, lineItemID, featureID, priceID, meterID string, qty float64, groupBy []string) *AnalyticsData {
	m := &meter.Meter{ID: meterID, Name: "Test Meter", EventName: "test_event", Aggregation: meter.Aggregation{Type: types.AggregationSum}}
	f := &feature.Feature{ID: featureID, Name: "Test Feature", MeterID: meterID, Type: types.FeatureTypeMetered}
	p := &price.Price{
		ID: priceID, Amount: decimal.NewFromFloat(0.01), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_fu_1",
		BillingModel: types.BILLING_MODEL_FLAT_FEE, Type: types.PRICE_TYPE_USAGE,
		MeterID: meterID, BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear,
	}
	li := &subscription.SubscriptionLineItem{
		ID: lineItemID, SubscriptionID: subID, PriceID: priceID, PriceType: types.PRICE_TYPE_USAGE, MeterID: meterID,
	}
	sub := &subscription.Subscription{ID: subID, LineItems: []*subscription.SubscriptionLineItem{li}}

	return &AnalyticsData{
		Subscriptions:         []*subscription.Subscription{sub},
		SubscriptionLineItems: map[string]*subscription.SubscriptionLineItem{lineItemID: li},
		Analytics: []*events.DetailedUsageAnalytic{
			{FeatureID: featureID, PriceID: priceID, MeterID: meterID, SubLineItemID: lineItemID, SubscriptionID: subID, TotalUsage: decimal.NewFromFloat(qty)},
		},
		Features: map[string]*feature.Feature{featureID: f},
		Meters:   map[string]*meter.Meter{meterID: m},
		Prices:   map[string]*price.Price{priceID: p},
		Currency: "usd",
		Params:   &events.UsageAnalyticsParams{GroupBy: groupBy},
	}
}

func (s *FeatureUsageDiscountSuite) createPercentageCoupon(ctx context.Context, id string, pct float64) *coupon.Coupon {
	c := &coupon.Coupon{
		ID: id, Name: "Test " + id, Type: types.CouponTypePercentage,
		PercentageOff: lo.ToPtr(decimal.NewFromFloat(pct)), Cadence: types.CouponCadenceForever,
		Currency: "usd", EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CouponRepo.Create(s.GetContext(), c))
	return c
}

func (s *FeatureUsageDiscountSuite) attachLineItemCoupon(assocID string, c *coupon.Coupon, subID, lineItemID string) {
	a := &coupon_association.CouponAssociation{
		ID: assocID, CouponID: c.ID, SubscriptionID: subID, SubscriptionLineItemID: lo.ToPtr(lineItemID),
		StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		Coupon: c, BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CouponAssociationRepo.Create(s.GetContext(), a))
}

// Case 1: 10% forever line-item coupon -> discount = 10% of gross.
func (s *FeatureUsageDiscountSuite) TestLineItemPercentageCoupon() {
	ctx := s.GetContext()
	data := s.buildData(ctx, "sub_fu_1", "li_fu_1", "feat_fu_1", "price_fu_1", "meter_fu_1", 10_000, nil)

	c := s.createPercentageCoupon(ctx, "coupon_fu_1", 10)
	s.attachLineItemCoupon("assoc_fu_1", c, "sub_fu_1", "li_fu_1")

	req := &dto.GetUsageAnalyticsRequest{StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EndTime: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
	resp, err := s.svc.buildAnalyticsResponse(ctx, data, req)
	s.NoError(err)
	s.Require().Len(resp.Items, 1)
	item := resp.Items[0]

	s.True(item.TotalCost.Equal(decimal.NewFromInt(100)), "TotalCost want 100 got %s", item.TotalCost)
	s.True(item.TotalDiscount.Equal(decimal.NewFromInt(10)), "TotalDiscount want 10 got %s", item.TotalDiscount)
	s.True(item.NetCost.Equal(decimal.NewFromInt(90)), "NetCost want 90 got %s", item.NetCost)

	s.True(resp.TotalCost.Equal(decimal.NewFromInt(100)), "resp.TotalCost want 100 got %s", resp.TotalCost)
	s.True(resp.TotalDiscount.Equal(decimal.NewFromInt(10)), "resp.TotalDiscount want 10 got %s", resp.TotalDiscount)
	s.True(resp.TotalNetCost.Equal(decimal.NewFromInt(90)), "resp.TotalNetCost want 90 got %s", resp.TotalNetCost)
}

// Case 2: No coupon -> discount stays 0, net equals gross (regression guard).
func (s *FeatureUsageDiscountSuite) TestNoCoupons() {
	ctx := s.GetContext()
	data := s.buildData(ctx, "sub_fu_2", "li_fu_2", "feat_fu_2", "price_fu_2", "meter_fu_2", 10_000, nil)

	req := &dto.GetUsageAnalyticsRequest{StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EndTime: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
	resp, err := s.svc.buildAnalyticsResponse(ctx, data, req)
	s.NoError(err)
	s.Require().Len(resp.Items, 1)
	item := resp.Items[0]

	s.True(item.TotalCost.Equal(decimal.NewFromInt(100)), "TotalCost want 100 got %s", item.TotalCost)
	s.True(item.TotalDiscount.Equal(decimal.Zero), "TotalDiscount should be 0, got %s", item.TotalDiscount)
	s.True(item.NetCost.Equal(item.TotalCost), "NetCost should equal TotalCost, got %s", item.NetCost)
	s.True(resp.TotalNetCost.Equal(resp.TotalCost), "resp.TotalNetCost should equal resp.TotalCost")
}
