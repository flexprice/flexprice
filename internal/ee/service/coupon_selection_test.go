package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/coupon"
	ca "github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

// dec(...) is defined in analytics_discount_test.go (same package).

func TestSplitAndOrderAssociations(t *testing.T) {
	pct := dec("10")
	perc := &coupon.Coupon{Type: types.CouponTypePercentage, PercentageOff: &pct, Cadence: types.CouponCadenceForever}
	fixed := &coupon.Coupon{Type: types.CouponTypeFixed}

	subs := []*subscription.Subscription{{
		ID:        "sub_1",
		LineItems: []*subscription.SubscriptionLineItem{{ID: "sli_1", PriceID: "price_1"}},
	}}

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	sli := "sli_1"
	assocs := []*ca.CouponAssociation{
		{ID: "ca_b", SubscriptionID: "sub_1", StartDate: t1, Coupon: perc},                                  // sub-level, later
		{ID: "ca_a", SubscriptionID: "sub_1", StartDate: t0, Coupon: perc},                                  // sub-level, earlier
		{ID: "ca_c", SubscriptionID: "sub_1", StartDate: t0, Coupon: perc},                                  // sub-level, same StartDate as ca_a (ID tiebreak)
		{ID: "ca_line", SubscriptionID: "sub_1", SubscriptionLineItemID: &sli, StartDate: t0, Coupon: perc}, // line-level
		{ID: "ca_fixed", SubscriptionID: "sub_1", StartDate: t0, Coupon: fixed},                             // filtered out
	}

	keep := func(c *coupon.Coupon, _ *ca.CouponAssociation) bool { return c.Type == types.CouponTypePercentage }
	sel := splitAndOrderAssociations(subs, assocs, keep)

	// StartDate-tie (ca_a, ca_c at t0) broken by ID; ca_b at t1 sorts last.
	if got := sel.SubLevel["sub_1"]; len(got) != 3 || got[0].ID != "ca_a" || got[1].ID != "ca_c" || got[2].ID != "ca_b" {
		t.Fatalf("sub-level split/order wrong: %+v", got)
	}
	if got := sel.LineLevel["sli_1"]; len(got) != 1 || got[0].ID != "ca_line" {
		t.Fatalf("line-level split wrong: %+v", got)
	}
	if sel.SubLineItemIDToPriceID["sli_1"] != "price_1" {
		t.Fatalf("price map wrong: %+v", sel.SubLineItemIDToPriceID)
	}
}

// TestSplitAndOrderAssociations_NilGuards covers the defensive skip for a nil
// association or one with a nil embedded Coupon — reachable if a caller ever
// passes a malformed slice, not just well-formed repo output.
func TestSplitAndOrderAssociations_NilGuards(t *testing.T) {
	pct := dec("10")
	perc := &coupon.Coupon{Type: types.CouponTypePercentage, PercentageOff: &pct}
	subs := []*subscription.Subscription{{ID: "sub_1"}}
	assocs := []*ca.CouponAssociation{
		nil,
		{ID: "ca_nil_coupon", SubscriptionID: "sub_1", StartDate: time.Now(), Coupon: nil},
		{ID: "ca_ok", SubscriptionID: "sub_1", StartDate: time.Now(), Coupon: perc},
	}

	sel := splitAndOrderAssociations(subs, assocs, nil)

	if got := sel.SubLevel["sub_1"]; len(got) != 1 || got[0].ID != "ca_ok" {
		t.Fatalf("nil/nil-coupon entries should be skipped, got %+v", got)
	}
}

// TestSelectSubscriptionCoupons_EmptySubs covers the empty-subs early return —
// no current caller hits this (both meter-usage and billing guard against it
// upstream), but it's a public shared helper's own defensive contract worth
// verifying directly.
func TestSelectSubscriptionCoupons_EmptySubs(t *testing.T) {
	sel, err := selectSubscriptionCoupons(context.Background(), ServiceParams{}, nil, time.Now(), time.Now(), nil)
	if err != nil {
		t.Fatalf("expected no error for empty subs, got %v", err)
	}
	if len(sel.SubLevel) != 0 || len(sel.LineLevel) != 0 {
		t.Fatalf("expected an empty selection, got %+v", sel)
	}
}

func TestProjectAnalyticsCoupons(t *testing.T) {
	pct := dec("10")
	perc := &coupon.Coupon{Type: types.CouponTypePercentage, PercentageOff: &pct}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sli := "sli_1"
	sel := &subscriptionCouponSelection{
		SubLevel:  map[string][]*ca.CouponAssociation{"sub_1": {{ID: "ca_a", SubscriptionID: "sub_1", StartDate: t0, Coupon: perc}}},
		LineLevel: map[string][]*ca.CouponAssociation{"sli_1": {{ID: "ca_line", SubscriptionID: "sub_1", SubscriptionLineItemID: &sli, StartDate: t0, Coupon: perc}}},
	}
	line, sub := projectAnalyticsCoupons(sel)
	if len(line["sli_1"]) != 1 || line["sli_1"][0].Coupon != perc || !line["sli_1"][0].StartDate.Equal(t0) {
		t.Fatalf("line projection wrong: %+v", line)
	}
	if len(sub["sub_1"]) != 1 || sub["sub_1"][0].Coupon != perc {
		t.Fatalf("sub projection wrong: %+v", sub)
	}
}
