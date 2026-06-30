package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/domain/coupon"
	ca "github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

// subscriptionCouponSelection is the split, deterministically-ordered set of active coupon
// associations for a group of subscriptions. Shared by analytics and billing.
type subscriptionCouponSelection struct {
	// SubLevel: associations with SubscriptionLineItemID == nil, keyed by SubscriptionID.
	SubLevel map[string][]*ca.CouponAssociation
	// LineLevel: associations with SubscriptionLineItemID != nil, keyed by SubscriptionLineItemID.
	LineLevel map[string][]*ca.CouponAssociation
	// SubLineItemIDToPriceID maps each subscription line item ID to its price ID (for callers
	// that key line-item coupons by price, e.g. invoice line items).
	SubLineItemIDToPriceID map[string]string
}

// splitAndOrderAssociations is the pure core: given subscriptions and their associations, keep
// those matching the predicate, split into sub-level vs line-level, and order each slice
// deterministically (StartDate, then ID) so compounding is stable.
//
// If keep is nil, all associations are accepted.
//
// subs[*].LineItems must be populated for SubLineItemIDToPriceID to be useful; callers needing
// price-keyed lookups must fetch subscriptions with their line items eager-loaded.
func splitAndOrderAssociations(
	subs []*subscription.Subscription,
	associations []*ca.CouponAssociation,
	keep func(*coupon.Coupon, *ca.CouponAssociation) bool,
) *subscriptionCouponSelection {
	sel := &subscriptionCouponSelection{
		SubLevel:               map[string][]*ca.CouponAssociation{},
		LineLevel:              map[string][]*ca.CouponAssociation{},
		SubLineItemIDToPriceID: map[string]string{},
	}
	for _, s := range subs {
		for _, li := range s.LineItems {
			if li.PriceID != "" {
				sel.SubLineItemIDToPriceID[li.ID] = li.PriceID
			}
		}
	}
	for _, a := range associations {
		if a == nil || a.Coupon == nil {
			continue
		}
		if keep != nil && !keep(a.Coupon, a) {
			continue
		}
		if a.SubscriptionLineItemID != nil {
			k := *a.SubscriptionLineItemID
			sel.LineLevel[k] = append(sel.LineLevel[k], a)
		} else {
			sel.SubLevel[a.SubscriptionID] = append(sel.SubLevel[a.SubscriptionID], a)
		}
	}
	order := func(m map[string][]*ca.CouponAssociation) {
		for _, v := range m {
			sort.SliceStable(v, func(i, j int) bool {
				if !v[i].StartDate.Equal(v[j].StartDate) {
					return v[i].StartDate.Before(v[j].StartDate)
				}
				return v[i].ID < v[j].ID
			})
		}
	}
	order(sel.SubLevel)
	order(sel.LineLevel)
	return sel
}

// selectSubscriptionCoupons fetches active associations for the given subscriptions over
// [start, end] (coupon eager-loaded) and returns the split selection filtered by keep.
//
// subs[*].LineItems must be populated for the returned SubLineItemIDToPriceID to be useful;
// callers needing price-keyed lookups must fetch subscriptions with line items eager-loaded.
func selectSubscriptionCoupons(
	ctx context.Context, sp ServiceParams,
	subs []*subscription.Subscription, start, end time.Time,
	keep func(*coupon.Coupon, *ca.CouponAssociation) bool,
) (*subscriptionCouponSelection, error) {
	if len(subs) == 0 {
		return splitAndOrderAssociations(subs, nil, keep), nil
	}
	subIDs := make([]string, 0, len(subs))
	for _, s := range subs {
		subIDs = append(subIDs, s.ID)
	}
	filter := types.NewNoLimitCouponAssociationFilter()
	filter.SubscriptionIDs = subIDs
	filter.ActiveOnly = true
	filter.PeriodStart = &start
	filter.PeriodEnd = &end

	associations, err := sp.CouponAssociationRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	return splitAndOrderAssociations(subs, associations, keep), nil
}

// projectAnalyticsCoupons converts the selection into the applicator's pure analyticsCoupon maps:
// line coupons keyed by SubscriptionLineItemID, sub coupons keyed by SubscriptionID.
//
// The returned analyticsCoupon.Coupon is a SHARED pointer to the association's coupon and must
// not be mutated by callers.
//
// SubLineItemIDToPriceID is NOT included in the returned pair; callers needing price-keyed
// lookups should read sel.SubLineItemIDToPriceID directly.
func projectAnalyticsCoupons(sel *subscriptionCouponSelection) (line, sub map[string][]analyticsCoupon) {
	line = map[string][]analyticsCoupon{}
	sub = map[string][]analyticsCoupon{}
	conv := func(a *ca.CouponAssociation) analyticsCoupon {
		return analyticsCoupon{Coupon: a.Coupon, StartDate: a.StartDate, EndDate: a.EndDate}
	}
	for k, as := range sel.LineLevel {
		for _, a := range as {
			line[k] = append(line[k], conv(a))
		}
	}
	for k, as := range sel.SubLevel {
		for _, a := range as {
			sub[k] = append(sub[k], conv(a))
		}
	}
	return line, sub
}
