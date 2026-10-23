package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type commitmentContext struct {
	Sub    *subscription.Subscription
	Items  []*subscription.SubscriptionLineItem
	Prices map[string]*price.Price
	Meters map[string]*meter.Meter
}

func invalidf(format string, args ...any) error {
	return ierr.NewErrorf(format, args...).
		WithHintf(format, args...).
		Mark(ierr.ErrValidation)
}

// loadCommitmentContext loads a subscription with its line items, prices and meters.
func (s *commitmentService) loadCommitmentContext(
	ctx context.Context,
	subscriptionID string,
) (*commitmentContext, error) {
	sub, items, err := s.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	cc := &commitmentContext{
		Sub:    sub,
		Items:  items,
		Prices: map[string]*price.Price{},
		Meters: map[string]*meter.Meter{},
	}

	priceIDs := lo.Uniq(lo.Map(items, func(li *subscription.SubscriptionLineItem, _ int) string {
		return li.PriceID
	}))
	if len(priceIDs) > 0 {
		f := types.NewNoLimitPriceFilter()
		f.PriceIDs = priceIDs
		prices, err := s.PriceRepo.List(ctx, f)
		if err != nil {
			return nil, err
		}

		for _, p := range prices {
			cc.Prices[p.ID] = p
		}
	}

	meterIDs := lo.Uniq(lo.FilterMap(items, func(li *subscription.SubscriptionLineItem, _ int) (string, bool) {
		return li.MeterID, li.MeterID != ""
	}))
	if len(meterIDs) > 0 {
		f := types.NewNoLimitMeterFilter()
		f.MeterIDs = meterIDs
		meters, err := s.MeterRepo.List(ctx, f)
		if err != nil {
			return nil, err
		}

		for _, m := range meters {
			cc.Meters[m.ID] = m
		}
	}

	return cc, nil
}

// resolveScopeLines returns usage line items covered by a scope at time t, ordered by price ID then line ID.
func resolveScopeLines(
	scopeType types.CommitmentScopeType,
	scopeID string,
	cc *commitmentContext,
	at time.Time,
) []*subscription.SubscriptionLineItem {
	lines := lo.Filter(cc.Items, func(li *subscription.SubscriptionLineItem, _ int) bool {
		if li.PriceType != types.PRICE_TYPE_USAGE || !lineActiveAt(li, at) {
			return false
		}

		p := cc.Prices[li.PriceID]
		switch scopeType {
		case types.CommitmentScopeSubscription:
			return true
		case types.CommitmentScopeGroup:
			return p != nil && p.GroupID == scopeID
		case types.CommitmentScopeMeter:
			return li.MeterID == scopeID
		case types.CommitmentScopePrice:
			return li.PriceID == scopeID
		}

		return false
	})
	sortLinesForFill(lines)
	return lines
}

func lineActiveAt(li *subscription.SubscriptionLineItem, at time.Time) bool {
	if at.Before(li.StartDate) {
		return false
	}

	return li.EndDate.IsZero() || at.Before(li.EndDate)
}

// scopeLinesInPeriod returns the line items in scope at any point of [from, to), ordered for fill.
func scopeLinesInPeriod(
	scopeType types.CommitmentScopeType,
	scopeID string,
	cc *commitmentContext,
	from time.Time,
	to time.Time,
) []*subscription.SubscriptionLineItem {
	if !from.Before(to) {
		return nil
	}

	at := []time.Time{from}
	for _, li := range cc.Items {
		if li.StartDate.After(from) && li.StartDate.Before(to) {
			at = append(at, li.StartDate)
		}
	}

	seen := map[string]bool{}
	var out []*subscription.SubscriptionLineItem
	for _, t := range at {
		for _, li := range resolveScopeLines(scopeType, scopeID, cc, t) {
			if !seen[li.ID] {
				seen[li.ID] = true
				out = append(out, li)
			}
		}
	}

	sortLinesForFill(out)
	return out
}

func sortLinesForFill(lines []*subscription.SubscriptionLineItem) {
	sort.SliceStable(lines, func(i, j int) bool {
		if lines[i].PriceID != lines[j].PriceID {
			return lines[i].PriceID < lines[j].PriceID
		}

		return lines[i].ID < lines[j].ID
	})
}

func (s *commitmentService) validateAgainstSubscription(
	ctx context.Context,
	req *dto.CreateCommitmentRequest,
	cc *commitmentContext,
	opts commitmentCreateOptions,
) error {
	sub := cc.Sub
	if !commitmentAcceptsSubscriptionStatus(sub.SubscriptionStatus, opts) {
		return invalidf("subscription %s must be active or trialing", sub.ID)
	}

	if req.StartDate.Before(sub.StartDate) {
		return invalidf("start_date cannot be before the subscription start %s", sub.StartDate.Format(time.RFC3339))
	}

	if err := validateBoundary(sub, req.EndDate); err != nil {
		return err
	}

	if req.Scope.EntityType == types.CommitmentScopeSubscription && req.Scope.EntityID != sub.ID {
		return invalidf("subscription scope must reference subscription %s", sub.ID)
	}

	if req.Scope.EntityType == types.CommitmentScopeGroup {
		g, err := s.GroupRepo.Get(ctx, req.Scope.EntityID)
		if err != nil {
			return err
		}

		if g.EntityType != types.GroupEntityTypePrice {
			return invalidf("group %s must be a price group", g.ID)
		}
	}

	lines := resolveScopeLines(req.Scope.EntityType, req.Scope.EntityID, cc, req.StartDate)
	if len(lines) == 0 {
		return invalidf(
			"scope %s/%s covers no usage prices on the subscription",
			req.Scope.EntityType,
			req.Scope.EntityID,
		)
	}

	meters := lo.Uniq(lo.Map(lines, func(li *subscription.SubscriptionLineItem, _ int) string {
		return li.MeterID
	}))
	if (req.Type == types.COMMITMENT_TYPE_QUANTITY || req.OverageRate != nil) && len(meters) != 1 {
		return invalidf(
			"quantity commitments and custom overage rates need a scope with exactly one meter, found %d",
			len(meters),
		)
	}

	if req.Measurement == types.CommitmentMeasurementWindow {
		sizes := lo.Uniq(lo.Map(lines, func(li *subscription.SubscriptionLineItem, _ int) types.WindowSize {
			return price.ResolveBucketSize(cc.Prices[li.PriceID], cc.Meters[li.MeterID])
		}))
		if len(sizes) != 1 || sizes[0] == "" {
			return invalid("window measurement needs bucketed prices that all share one bucket size")
		}
	}

	if sub.CommitmentAmount != nil && sub.CommitmentAmount.IsPositive() {
		return invalid("subscription has a legacy subscription-level commitment; remove it before adding a commitment")
	}

	for _, li := range lines {
		if li.HasAnyCommitment() {
			return invalidf("price %s already has a legacy line-item commitment", li.PriceID)
		}
	}

	for _, li := range scopeLinesInPeriod(req.Scope.EntityType, req.Scope.EntityID, cc, req.StartDate, req.EndDate) {
		if !billsEverySubscriptionPeriod(sub, li.BillingPeriod, li.BillingPeriodCount) {
			return errScopeCadence(li.PriceID)
		}
	}

	return s.validateOverlaps(ctx, req, cc, lines)
}

// billsEverySubscriptionPeriod reports whether a price bills on the subscription's own cadence, so its invoice
// window is the subscription period.
func billsEverySubscriptionPeriod(sub *subscription.Subscription, period types.BillingPeriod, count int) bool {
	return period == sub.BillingPeriod && max(count, 1) == max(sub.BillingPeriodCount, 1)
}

func errScopeCadence(priceID string) error {
	return ierr.NewErrorf("commitments require scope prices billed every subscription period; price %s bills on another cadence", priceID).
		WithHint("commitments require scope prices billed every subscription period").
		Mark(ierr.ErrValidation)
}

func commitmentAcceptsSubscriptionStatus(status types.SubscriptionStatus, opts commitmentCreateOptions) bool {
	switch status {
	case types.SubscriptionStatusActive, types.SubscriptionStatusTrialing:
		return true
	case types.SubscriptionStatusIncomplete, types.SubscriptionStatusDraft:
		return opts.allowPendingSubscription
	}

	return false
}

func invalid(msg string) error {
	return ierr.NewError(msg).
		WithHint(msg).
		Mark(ierr.ErrValidation)
}

// validateOverlaps requires active commitments sharing a line item to differ in priority and agree on
// type, measurement and overage rate.
func (s *commitmentService) validateOverlaps(
	ctx context.Context,
	req *dto.CreateCommitmentRequest,
	cc *commitmentContext,
	lines []*subscription.SubscriptionLineItem,
) error {
	f := commitment.NewNoLimitFilter()
	f.SubscriptionIDs = []string{cc.Sub.ID}
	f.Statuses = []types.CommitmentStatus{types.CommitmentStatusActive}
	f.ActiveFrom, f.ActiveTo = &req.StartDate, &req.EndDate
	existing, err := s.CommitmentRepo.List(ctx, f)
	if err != nil {
		return err
	}

	lineIDs := lo.Map(lines, func(li *subscription.SubscriptionLineItem, _ int) string {
		return li.ID
	})
	for _, other := range existing {
		otherLines := resolveScopeLines(
			other.ScopeEntityType,
			other.ScopeEntityID,
			cc,
			maxTime(req.StartDate, other.StartDate),
		)
		otherLineIDs := lo.Map(otherLines, func(li *subscription.SubscriptionLineItem, _ int) string {
			return li.ID
		})
		if len(lo.Intersect(lineIDs, otherLineIDs)) == 0 {
			continue
		}

		if other.Priority == req.Priority {
			return invalidf("priority %d is already used by overlapping commitment %s", req.Priority, other.ID)
		}

		if other.Type != req.Type || other.Measurement != req.Measurement {
			return invalidf("overlapping commitment %s has a different type or measurement", other.ID)
		}

		same, err := s.sameOverage(ctx, other, req)
		if err != nil {
			return err
		}

		if !same {
			return invalidf("overlapping commitment %s has a different overage rate", other.ID)
		}
	}

	return nil
}

// sameOverage reports whether both sides have no custom overage rate, or the same one.
func (s *commitmentService) sameOverage(
	ctx context.Context,
	other *commitment.Commitment,
	req *dto.CreateCommitmentRequest,
) (bool, error) {
	if other.OveragePriceID == nil || req.OverageRate == nil {
		return other.OveragePriceID == nil && req.OverageRate == nil, nil
	}

	p, err := s.PriceRepo.Get(ctx, *other.OveragePriceID)
	if err != nil {
		return false, err
	}

	return p.Amount.Equal(*req.OverageRate), nil
}

// validateBoundary requires end to fall on a subscription billing-period end or the subscription end,
// and never after the subscription end.
func validateBoundary(sub *subscription.Subscription, end time.Time) error {
	if sub.EndDate != nil {
		if end.Equal(*sub.EndDate) {
			return nil
		}

		if end.After(*sub.EndDate) {
			return invalidf("end_date cannot be after the subscription end %s", sub.EndDate.Format(time.RFC3339))
		}
	}

	// Extend one period past end so the last period is not clipped at end.
	horizon, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: end,
		BillingAnchor:      sub.BillingAnchor,
		Unit:               sub.BillingPeriodCount,
		Period:             sub.BillingPeriod,
		Timezone:           sub.Timezone,
	})
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to compute billing periods for commitment").
			Mark(ierr.ErrInternal)
	}

	periods, err := types.CalculateBillingPeriods(&types.CalculateBillingPeriodsParams{
		InitialPeriodStart: sub.StartDate,
		EndDate:            &horizon,
		Anchor:             sub.BillingAnchor,
		PeriodCount:        sub.BillingPeriodCount,
		BillingPeriod:      sub.BillingPeriod,
		Timezone:           sub.Timezone,
	})
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to compute billing periods for commitment").
			Mark(ierr.ErrInternal)
	}

	var before, after *time.Time
	for _, p := range periods {
		switch {
		case p.End.Equal(end):
			return nil
		case p.End.Before(end):
			before = lo.ToPtr(p.End)
		case after == nil:
			after = lo.ToPtr(p.End)
		}
	}

	const layout = "2 Jan 2006"
	switch {
	case before != nil && after != nil:
		return invalidf(
			"end_date must fall on a billing date; nearest are %s and %s",
			before.Format(layout),
			after.Format(layout),
		)
	case after != nil:
		return invalidf("end_date must fall on a billing date; nearest is %s", after.Format(layout))
	case before != nil:
		return invalidf("end_date must fall on a billing date; nearest is %s", before.Format(layout))
	}

	return invalid("end_date must fall on a billing date")
}
