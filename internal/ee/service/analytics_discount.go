package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/shopspring/decimal"
)

// analyticsCoupon is the applicator's pure view of a coupon association: the coupon plus the
// active window from its association. It intentionally has no coupon_association dependency so
// the applicator stays trivially unit-testable. Callers are responsible for validity/cadence/type
// filtering: the applicator does NOT call IsValid and assumes only applicable percentage coupons
// are passed in.
type analyticsCoupon struct {
	Coupon    *coupon.Coupon
	StartDate time.Time
	EndDate   *time.Time
}

// activeAt reports whether the association window covers t. EndDate nil = open-ended; both
// bounds inclusive.
func (ac analyticsCoupon) activeAt(t time.Time) bool {
	if t.Before(ac.StartDate) {
		return false
	}
	if ac.EndDate != nil && t.After(*ac.EndDate) {
		return false
	}
	return true
}

// activeOverlaps reports whether the association window overlaps [start, end]. EndDate nil =
// open-ended; both bounds inclusive (a coupon with EndDate == start is still considered active).
// This matches the repo's CouponAssociation ActiveOnly filter (end_date >= period_start).
func (ac analyticsCoupon) activeOverlaps(start, end time.Time) bool {
	if ac.EndDate != nil && ac.EndDate.Before(start) {
		return false
	}
	return !ac.StartDate.After(end)
}

type pointCost struct {
	Timestamp time.Time
	Cost      decimal.Decimal
}

type discountInput struct {
	Currency    string
	SubTotal    decimal.Decimal
	Points      []events.UsageAnalyticPoint // empty => non-windowed
	LineCoupons []*analyticsCoupon          // applied first
	SubCoupons  []*analyticsCoupon          // applied after, compounding
	RangeStart  time.Time                   // used when Points is empty
	RangeEnd    time.Time                   // used when Points is empty
}

type discountOutput struct {
	TotalDiscount  decimal.Decimal
	SubTotal       decimal.Decimal
	PointDiscounts []events.UsageAnalyticPoint // aligned to input Points; nil when non-windowed
}

// compound applies the active line coupons then sub coupons onto base, using the canonical
// coupon.ApplyDiscount math (which rounds to currency precision and caps at >= 0), and returns
// the total discount (base - remaining).
func compound(base decimal.Decimal, currency string, line, sub []*analyticsCoupon, active func(*analyticsCoupon) bool) decimal.Decimal {
	remaining := base
	apply := func(cs []*analyticsCoupon) {
		for _, c := range cs {
			if remaining.LessThanOrEqual(decimal.Zero) || !active(c) {
				continue
			}
			remaining = c.Coupon.ApplyDiscount(remaining, currency).FinalPrice
		}
	}
	apply(line)
	apply(sub)
	return base.Sub(remaining)
}

// ApplyAnalyticsDiscounts computes per-window (or all-or-nothing when non-windowed) percentage
// discounts for one analytic item. Pure: no DB, no service deps.
func ApplyAnalyticsDiscounts(in *discountInput) *discountOutput {
	if len(in.LineCoupons) == 0 && len(in.SubCoupons) == 0 {
		return &discountOutput{
			TotalDiscount:  decimal.Zero,
			SubTotal:       in.SubTotal,
			PointDiscounts: in.Points,
		}
	}

	// Non-windowed input.
	// so in case points are empty, we apply the discounts to subscription level only internally we still apply coupons for line items and roll up to subscription level.
	if len(in.Points) == 0 {

		discount := compound(in.SubTotal, in.Currency, in.LineCoupons, in.SubCoupons,
			func(c *analyticsCoupon) bool { return c.activeOverlaps(in.RangeStart, in.RangeEnd) })
		return &discountOutput{TotalDiscount: discount, SubTotal: in.SubTotal.Sub(discount)}
	}

	totalDiscount := decimal.Zero
	for i := range in.Points {
		discount := compound(in.Points[i].Cost, in.Currency, in.LineCoupons, in.SubCoupons,
			func(c *analyticsCoupon) bool { return c.activeAt(in.Points[i].Timestamp) })
		totalDiscount = totalDiscount.Add(discount)
		in.Points[i].Discount = discount
	}
	return &discountOutput{
		TotalDiscount:  totalDiscount,
		SubTotal:       in.SubTotal.Sub(totalDiscount),
		PointDiscounts: in.Points,
	}
}
