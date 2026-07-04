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
// If filter is nil, all associations are accepted.
//
// subs[*].LineItems must be populated for SubLineItemIDToPriceID to be useful; callers needing
// price-keyed lookups must fetch subscriptions with their line items eager-loaded.
func splitAndOrderAssociations(
	subs []*subscription.Subscription,
	associations []*ca.CouponAssociation,
	filter func(*coupon.Coupon, *ca.CouponAssociation) bool,
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
		if filter != nil && !filter(a.Coupon, a) {
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
// [start, end] (coupon eager-loaded) and returns the split selection filtered by filter.
//
// subs[*].LineItems must be populated for the returned SubLineItemIDToPriceID to be useful;
// callers needing price-keyed lookups must fetch subscriptions with line items eager-loaded.
func selectSubscriptionCoupons(
	ctx context.Context, sp ServiceParams,
	subs []*subscription.Subscription, start, end time.Time,
	filter func(*coupon.Coupon, *ca.CouponAssociation) bool,
) (*subscriptionCouponSelection, error) {
	if len(subs) == 0 {
		return splitAndOrderAssociations(subs, nil, filter), nil
	}
	subIDs := make([]string, 0, len(subs))
	for _, s := range subs {
		subIDs = append(subIDs, s.ID)
	}
	caFilter := types.NewNoLimitCouponAssociationFilter()
	caFilter.SubscriptionIDs = subIDs
	caFilter.ActiveOnly = true
	caFilter.PeriodStart = &start
	caFilter.PeriodEnd = &end

	associations, err := sp.CouponAssociationRepo.List(ctx, caFilter)
	if err != nil {
		return nil, err
	}
	return splitAndOrderAssociations(subs, associations, filter), nil
}

// projectAnalyticsCoupons converts the selection into the applicator's pure analyticsCoupon maps:
// line coupons keyed by SubscriptionLineItemID, sub coupons keyed by SubscriptionID.
//
// The returned analyticsCoupon.Coupon is a SHARED pointer to the association's coupon and must
// not be mutated by callers.
//
// SubLineItemIDToPriceID is NOT included in the returned pair; callers needing price-keyed
// lookups should read sel.SubLineItemIDToPriceID directly.
func projectAnalyticsCoupons(sel *subscriptionCouponSelection) (line, sub map[string][]*analyticsCoupon) {
	line = map[string][]*analyticsCoupon{}
	sub = map[string][]*analyticsCoupon{}

	for k, as := range sel.LineLevel {
		for _, a := range as {
			line[k] = append(line[k], &analyticsCoupon{
				Coupon:    a.Coupon,
				StartDate: a.StartDate,
				EndDate:   a.EndDate,
			})
		}
	}
	for k, as := range sel.SubLevel {
		for _, a := range as {
			sub[k] = append(sub[k], &analyticsCoupon{
				Coupon:    a.Coupon,
				StartDate: a.StartDate,
				EndDate:   a.EndDate,
			})
		}
	}
	return line, sub
}
