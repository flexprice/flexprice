package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func pctCoupon(pct string, start time.Time, end *time.Time) analyticsCoupon {
	p := dec(pct)
	return analyticsCoupon{
		Coupon: &coupon.Coupon{
			Type:          types.CouponTypePercentage,
			PercentageOff: &p,
		},
		StartDate: start,
		EndDate:   end,
	}
}

func TestApplyAnalyticsDiscounts_NonWindowed(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	before := t0.Add(-48 * time.Hour)

	tests := []struct {
		name         string
		gross        string
		line         []analyticsCoupon
		sub          []analyticsCoupon
		wantDiscount string
		wantNet      string
	}{
		{"no coupons", "100.00", nil, nil, "0", "100.00"},
		{"single line 10%", "100.00", []analyticsCoupon{pctCoupon("10", t0, nil)}, nil, "10", "90"},
		{"single sub 10%", "100.00", nil, []analyticsCoupon{pctCoupon("10", t0, nil)}, "10", "90"},
		{"line 10% then sub 20%", "100.00", []analyticsCoupon{pctCoupon("10", t0, nil)}, []analyticsCoupon{pctCoupon("20", t0, nil)}, "28", "72"},
		{"100% caps at zero", "100.00", []analyticsCoupon{pctCoupon("100", t0, nil)}, nil, "100", "0"},
		{"33.33% rounds", "100.00", nil, []analyticsCoupon{pctCoupon("33.33", t0, nil)}, "33.33", "66.67"},
		{"window before range -> none", "100.00", []analyticsCoupon{pctCoupon("10", before, ptrTime(t0.Add(-time.Hour)))}, nil, "0", "100.00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := ApplyAnalyticsDiscounts(discountInput{
				Currency:       "USD",
				GrossTotalCost: dec(tc.gross),
				LineCoupons:    tc.line,
				SubCoupons:     tc.sub,
				RangeStart:     t0,
				RangeEnd:       t1,
			})
			if !out.TotalDiscount.Equal(dec(tc.wantDiscount)) {
				t.Fatalf("discount: want %s got %s", tc.wantDiscount, out.TotalDiscount)
			}
			if !out.NetCost.Equal(dec(tc.wantNet)) {
				t.Fatalf("net: want %s got %s", tc.wantNet, out.NetCost)
			}
			if out.PointDiscounts != nil {
				t.Errorf("non-windowed PointDiscounts should be nil, got %v", out.PointDiscounts)
			}
		})
	}
}

func TestApplyAnalyticsDiscounts_OverlapBoundaryInclusive(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	// Coupon EndDate == RangeStart must still overlap (both bounds inclusive),
	// so a 10% discount applies.
	out := ApplyAnalyticsDiscounts(discountInput{
		Currency:       "USD",
		GrossTotalCost: dec("100.00"),
		SubCoupons:     []analyticsCoupon{pctCoupon("10", t0.Add(-48*time.Hour), ptrTime(t0))},
		RangeStart:     t0,
		RangeEnd:       t1,
	})
	if !out.TotalDiscount.Equal(dec("10")) || !out.NetCost.Equal(dec("90")) {
		t.Fatalf("boundary-inclusive: discount=%s net=%s", out.TotalDiscount, out.NetCost)
	}
}

func TestApplyAnalyticsDiscounts_Windowed(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	rangeEnd := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	points := []pointCost{{Timestamp: t0, Cost: dec("50")}, {Timestamp: t1, Cost: dec("50")}}

	out := ApplyAnalyticsDiscounts(discountInput{
		Currency: "USD", GrossTotalCost: dec("100"), Points: points,
		SubCoupons: []analyticsCoupon{pctCoupon("10", t0, nil)}, RangeStart: t0, RangeEnd: rangeEnd,
	})
	if !out.TotalDiscount.Equal(dec("10")) || !out.NetCost.Equal(dec("90")) {
		t.Fatalf("all-active: discount=%s net=%s", out.TotalDiscount, out.NetCost)
	}
	if len(out.PointDiscounts) != 2 || !out.PointDiscounts[0].Equal(dec("5")) || !out.PointDiscounts[1].Equal(dec("5")) {
		t.Fatalf("all-active point discounts: %v", out.PointDiscounts)
	}

	out = ApplyAnalyticsDiscounts(discountInput{
		Currency: "USD", GrossTotalCost: dec("100"), Points: points,
		SubCoupons: []analyticsCoupon{pctCoupon("10", t1, nil)}, RangeStart: t0, RangeEnd: rangeEnd,
	})
	if !out.PointDiscounts[0].Equal(decimal.Zero) || !out.PointDiscounts[1].Equal(dec("5")) {
		t.Fatalf("partial point discounts: %v", out.PointDiscounts)
	}
	if !out.TotalDiscount.Equal(dec("5")) || !out.NetCost.Equal(dec("95")) {
		t.Fatalf("partial: discount=%s net=%s", out.TotalDiscount, out.NetCost)
	}
}

// TestApplyAnalyticsDiscounts_WindowedEndDateExpiry covers the activeAt branch where a
// coupon has a non-nil EndDate and a point falls after it — the only branch of activeAt
// not otherwise exercised by open-ended (EndDate == nil) coupons used elsewhere.
func TestApplyAnalyticsDiscounts_WindowedEndDateExpiry(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	rangeEnd := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	couponEnd := t0.Add(12 * time.Hour) // expires between t0 and t1
	points := []pointCost{{Timestamp: t0, Cost: dec("50")}, {Timestamp: t1, Cost: dec("50")}}

	out := ApplyAnalyticsDiscounts(discountInput{
		Currency: "USD", GrossTotalCost: dec("100"), Points: points,
		SubCoupons: []analyticsCoupon{pctCoupon("10", t0, &couponEnd)}, RangeStart: t0, RangeEnd: rangeEnd,
	})
	if !out.PointDiscounts[0].Equal(dec("5")) {
		t.Fatalf("point at/before EndDate should be discounted, got %s", out.PointDiscounts[0])
	}
	if !out.PointDiscounts[1].Equal(decimal.Zero) {
		t.Fatalf("point after EndDate should NOT be discounted, got %s", out.PointDiscounts[1])
	}
	if !out.TotalDiscount.Equal(dec("5")) || !out.NetCost.Equal(dec("95")) {
		t.Fatalf("expired coupon: discount=%s net=%s", out.TotalDiscount, out.NetCost)
	}
}

func TestApplyAnalyticsDiscounts_WindowedNoCoupons(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	rangeEnd := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	points := []pointCost{{Timestamp: t0, Cost: dec("50")}, {Timestamp: t1, Cost: dec("50")}}

	out := ApplyAnalyticsDiscounts(discountInput{
		Currency: "USD", GrossTotalCost: dec("100"), Points: points,
		RangeStart: t0, RangeEnd: rangeEnd,
	})
	if out.PointDiscounts == nil {
		t.Fatal("windowed input must return a non-nil PointDiscounts slice")
	}
	if len(out.PointDiscounts) != 2 {
		t.Fatalf("PointDiscounts length: want 2 got %d", len(out.PointDiscounts))
	}
	for i, d := range out.PointDiscounts {
		if !d.Equal(decimal.Zero) {
			t.Fatalf("point %d discount: want 0 got %s", i, d)
		}
	}
	if !out.TotalDiscount.Equal(decimal.Zero) || !out.NetCost.Equal(dec("100")) {
		t.Fatalf("totals: discount=%s net=%s", out.TotalDiscount, out.NetCost)
	}
}

func TestApplyAnalyticsDiscounts_StackingAndCurrency(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name, currency, gross string
		sub                   []analyticsCoupon
		wantDiscount, wantNet string
	}{
		{"two sub 10% stack", "USD", "100.00", []analyticsCoupon{pctCoupon("10", t0, nil), pctCoupon("10", t0, nil)}, "19", "81"},
		// 999999.99 * 1% = 9999.9999, rounds half-up to 10000.00
		{"1% of large", "USD", "999999.99", []analyticsCoupon{pctCoupon("1", t0, nil)}, "10000.00", "989999.99"},
		{"JPY zero-decimal", "JPY", "100", []analyticsCoupon{pctCoupon("33.33", t0, nil)}, "33", "67"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := ApplyAnalyticsDiscounts(discountInput{
				Currency: tc.currency, GrossTotalCost: dec(tc.gross),
				SubCoupons: tc.sub, RangeStart: t0, RangeEnd: t1,
			})
			if !out.TotalDiscount.Equal(dec(tc.wantDiscount)) {
				t.Fatalf("discount: want %s got %s", tc.wantDiscount, out.TotalDiscount)
			}
			if !out.NetCost.Equal(dec(tc.wantNet)) {
				t.Fatalf("net: want %s got %s", tc.wantNet, out.NetCost)
			}
		})
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// TestApplyAnalyticsDiscounts_EmptyInputsAreNoOp covers loadAnalyticsCoupons's own
// early-return guards directly — GetDetailedAnalytics never calls it with empty
// Analytics/Subscriptions in practice, but the function's defensive guard should hold
// on its own regardless of caller behavior.
func TestApplyAnalyticsDiscounts_EmptyInputsAreNoOp(t *testing.T) {
	ctx := context.Background()

	lineItemCoupons, subscriptionCoupons := loadAnalyticsCoupons(ctx, ServiceParams{}, &AnalyticsData{}, time.Now(), time.Now())
	if lineItemCoupons != nil {
		t.Fatalf("lineItemCoupons: want nil got %v", lineItemCoupons)
	}
	if subscriptionCoupons != nil {
		t.Fatalf("subscriptionCoupons: want nil got %v", subscriptionCoupons)
	}

	lineItemCoupons, subscriptionCoupons = loadAnalyticsCoupons(ctx, ServiceParams{}, &AnalyticsData{
		Analytics: []*events.DetailedUsageAnalytic{{TotalCost: decimal.NewFromInt(100)}},
	}, time.Now(), time.Now())
	if lineItemCoupons != nil {
		t.Fatalf("lineItemCoupons: want nil got %v", lineItemCoupons)
	}
	if subscriptionCoupons != nil {
		t.Fatalf("subscriptionCoupons: want nil got %v", subscriptionCoupons)
	}
	// No assertions beyond "did not panic" and "empty maps come back nil" — this guards
	// the function's own early returns (no analytics/subscriptions, no CouponAssociationRepo).
}
