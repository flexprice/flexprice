package service

// feature_usage_grouping_discount_test.go — regression test for a bug found by
// code review: aggregateAnalyticsByGrouping (and its point-level helper
// mergeTimeSeriesPoints) merge TotalCost/Cost across rows sharing a group key,
// but originally dropped TotalDiscount/Discount entirely. Since
// applyAnalyticsDiscounts runs BEFORE grouping in buildAnalyticsResponse, any
// grouped feature-usage analytics query with an applicable coupon would report
// zero discount despite it having been correctly computed pre-merge.
//
// These are pure unit tests against aggregateAnalyticsByGrouping directly — no
// DB or fixture is needed since the function only operates on the
// []*events.DetailedUsageAnalytic slice it's given.

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestAggregateAnalyticsByGrouping_PreservesDiscount(t *testing.T) {
	s := &featureUsageTrackingService{}
	ts := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

	// Two rows for the SAME feature/price/meter/line-item (e.g. split across two
	// underlying usage chunks), each already discounted by applyAnalyticsDiscounts.
	items := []*events.DetailedUsageAnalytic{
		{
			FeatureID:     "feat_1",
			PriceID:       "price_1",
			MeterID:       "meter_1",
			SubLineItemID: "sli_1",
			TotalCost:     decimal.NewFromInt(60),
			TotalDiscount: decimal.NewFromInt(6),
			NetCost:       decimal.NewFromInt(54),
			Points: []events.UsageAnalyticPoint{
				{Timestamp: ts, Cost: decimal.NewFromInt(60), Discount: decimal.NewFromInt(6), NetCost: decimal.NewFromInt(54)},
			},
		},
		{
			FeatureID:     "feat_1",
			PriceID:       "price_1",
			MeterID:       "meter_1",
			SubLineItemID: "sli_1",
			TotalCost:     decimal.NewFromInt(40),
			TotalDiscount: decimal.NewFromInt(4),
			NetCost:       decimal.NewFromInt(36),
			Points: []events.UsageAnalyticPoint{
				{Timestamp: ts, Cost: decimal.NewFromInt(40), Discount: decimal.NewFromInt(4), NetCost: decimal.NewFromInt(36)},
			},
		},
	}

	result := s.aggregateAnalyticsByGrouping(items, []string{"source"})

	require.Len(t, result, 1, "both rows share the same feature_id/price_id/meter_id/sub_line_item_id key and should merge into one")
	merged := result[0]

	require.True(t, merged.TotalCost.Equal(decimal.NewFromInt(100)), "TotalCost want 100 got %s", merged.TotalCost)
	require.True(t, merged.TotalDiscount.Equal(decimal.NewFromInt(10)), "TotalDiscount want 10 got %s", merged.TotalDiscount)
	require.True(t, merged.NetCost.Equal(decimal.NewFromInt(90)), "NetCost want 90 got %s", merged.NetCost)

	require.Len(t, merged.Points, 1, "both points share the same timestamp and should merge into one")
	pt := merged.Points[0]
	require.True(t, pt.Cost.Equal(decimal.NewFromInt(100)), "point.Cost want 100 got %s", pt.Cost)
	require.True(t, pt.Discount.Equal(decimal.NewFromInt(10)), "point.Discount want 10 got %s", pt.Discount)
	require.True(t, pt.NetCost.Equal(decimal.NewFromInt(90)), "point.NetCost want 90 got %s", pt.NetCost)
}

func TestAggregateAnalyticsByGrouping_NoDiscountRegression(t *testing.T) {
	// Regression guard: grouping with zero discounts must not introduce any.
	s := &featureUsageTrackingService{}
	ts := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

	items := []*events.DetailedUsageAnalytic{
		{
			FeatureID: "feat_2", PriceID: "price_2", MeterID: "meter_2", SubLineItemID: "sli_2",
			TotalCost: decimal.NewFromInt(30), NetCost: decimal.NewFromInt(30),
			Points: []events.UsageAnalyticPoint{{Timestamp: ts, Cost: decimal.NewFromInt(30), NetCost: decimal.NewFromInt(30)}},
		},
		{
			FeatureID: "feat_2", PriceID: "price_2", MeterID: "meter_2", SubLineItemID: "sli_2",
			TotalCost: decimal.NewFromInt(20), NetCost: decimal.NewFromInt(20),
			Points: []events.UsageAnalyticPoint{{Timestamp: ts, Cost: decimal.NewFromInt(20), NetCost: decimal.NewFromInt(20)}},
		},
	}

	result := s.aggregateAnalyticsByGrouping(items, []string{"source"})
	require.Len(t, result, 1)
	merged := result[0]

	require.True(t, merged.TotalCost.Equal(decimal.NewFromInt(50)), "TotalCost want 50 got %s", merged.TotalCost)
	require.True(t, merged.TotalDiscount.IsZero(), "TotalDiscount should stay 0, got %s", merged.TotalDiscount)
	require.True(t, merged.NetCost.Equal(decimal.NewFromInt(50)), "NetCost want 50 got %s", merged.NetCost)
}
