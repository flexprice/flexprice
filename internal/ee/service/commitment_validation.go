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

// commitmentContext is a subscription with every published line it has had, and their prices and meters by ID.
type commitmentContext struct {
	Sub    *subscription.Subscription
	Lines  []*subscription.SubscriptionLineItem
	Prices map[string]*price.Price
	Meters map[string]*meter.Meter
}

// bucketSize is the bucket size the line's price bills usage in.
func (cc *commitmentContext) bucketSize(li *subscription.SubscriptionLineItem) types.WindowSize {
	return price.ResolveBucketSize(cc.Prices[li.PriceID], cc.Meters[li.MeterID])
}

// loadCommitmentContext loads a subscription with every line it has had, and their prices and meters, so a term
// reaching back before the current period still resolves its scope.
func (s *commitmentService) loadCommitmentContext(
	ctx context.Context,
	subscriptionID string,
) (*commitmentContext, error) {
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	lf := types.NewNoLimitSubscriptionLineItemFilter()
	lf.SubscriptionIDs = []string{subscriptionID}
	lf.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	lines, err := s.SubscriptionLineItemRepo.List(ctx, lf)
	if err != nil {
		return nil, err
	}

	cc := &commitmentContext{
		Sub:    sub,
		Lines:  lines,
		Prices: map[string]*price.Price{},
		Meters: map[string]*meter.Meter{},
	}

	priceIDs := lo.Uniq(lo.Map(lines, func(li *subscription.SubscriptionLineItem, _ int) string {
		return li.PriceID
	}))
	if len(priceIDs) > 0 {
		f := types.NewNoLimitPriceFilter()
		f.PriceIDs = priceIDs
		f.AllowExpiredPrices = true
		prices, err := s.PriceRepo.List(ctx, f)
		if err != nil {
			return nil, err
		}

		for _, p := range prices {
			cc.Prices[p.ID] = p
		}
	}

	meterIDs := lo.Uniq(lo.FilterMap(lines, func(li *subscription.SubscriptionLineItem, _ int) (string, bool) {
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

// scopeLinesAt returns the usage lines c's scope covers at the instant at, ordered for fill.
func scopeLinesAt(c *commitment.Commitment, cc *commitmentContext, at time.Time) []*subscription.SubscriptionLineItem {
	lines := lo.Filter(cc.Lines, func(li *subscription.SubscriptionLineItem, _ int) bool {
		return li.PriceType == types.PRICE_TYPE_USAGE && lineActiveAt(li, at) && commitmentCoversLine(c, li, cc.Prices[li.PriceID])
	})
	sortLinesForFill(lines)
	return lines
}

// scopeLinesAtStart returns the lines c's scope covers when it starts billing: at the later of its start and the
// subscription's, since a whole-second term edge can sit just below a mid-second line start.
func scopeLinesAtStart(c *commitment.Commitment, cc *commitmentContext) []*subscription.SubscriptionLineItem {
	return scopeLinesAt(c, cc, maxTime(c.StartDate, cc.Sub.StartDate))
}

func lineActiveAt(li *subscription.SubscriptionLineItem, at time.Time) bool {
	if at.Before(li.StartDate) {
		return false
	}

	return li.EndDate.IsZero() || at.Before(li.EndDate)
}

// scopeLinesInPeriod returns the lines c's scope covers at any point of [from, to), ordered for fill; a nil to takes
// every line joining after from.
func scopeLinesInPeriod(
	c *commitment.Commitment,
	cc *commitmentContext,
	from time.Time,
	to *time.Time,
) []*subscription.SubscriptionLineItem {
	if endReached(to, from) {
		return nil
	}

	at := []time.Time{from}
	for _, li := range cc.Lines {
		if li.StartDate.After(from) && !endReached(to, li.StartDate) {
			at = append(at, li.StartDate)
		}
	}

	seen := map[string]bool{}
	var out []*subscription.SubscriptionLineItem
	for _, t := range at {
		for _, li := range scopeLinesAt(c, cc, t) {
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

// validateAgainstSubscription checks c against its subscription: the term fits it, the scope covers usage lines
// that suit c over the whole term, and c agrees with the commitments it overlaps.
func (s *commitmentService) validateAgainstSubscription(
	ctx context.Context,
	c *commitment.Commitment,
	overage *dto.CommitmentPriceInput,
	cc *commitmentContext,
	opts commitmentCreateOptions,
) error {
	if err := validateTerm(c, cc.Sub, opts); err != nil {
		return err
	}

	if err := s.validateScopeEntity(ctx, c, cc.Sub); err != nil {
		return err
	}

	if len(scopeLinesAtStart(c, cc)) == 0 {
		return ierr.Validationf("scope %s/%s covers no usage prices on the subscription", c.ScopeEntityType, c.ScopeEntityID)
	}

	// Lines scheduled to join later in the term must satisfy the same rules as those present at start.
	termLines := scopeLinesInPeriod(c, cc, c.StartDate, c.EffectiveEnd())
	if err := s.validateTermLines(ctx, c, overage, cc, termLines); err != nil {
		return err
	}

	return s.validateOverlaps(ctx, c, overage, cc, termLines)
}

// validateTerm checks that the subscription can take a commitment and that c's term fits inside it.
func validateTerm(c *commitment.Commitment, sub *subscription.Subscription, opts commitmentCreateOptions) error {
	accepted := []types.SubscriptionStatus{types.SubscriptionStatusActive, types.SubscriptionStatusTrialing}
	if opts.allowPendingSubscription {
		accepted = append(accepted, types.SubscriptionStatusIncomplete, types.SubscriptionStatusDraft)
	}

	if !lo.Contains(accepted, sub.SubscriptionStatus) {
		return ierr.Validationf("subscription %s must be active or trialing", sub.ID)
	}

	if sub.CommitmentAmount != nil && sub.CommitmentAmount.IsPositive() {
		return ierr.Validation("subscription has a legacy subscription-level commitment; remove it before adding a commitment")
	}

	// A subscription created before starts were whole seconds may start mid-second; a term may start at that second.
	if c.StartDate.Before(sub.StartDate.Truncate(time.Second)) {
		return ierr.Validationf("start_date cannot be before the subscription start %s", sub.StartDate.Format(time.RFC3339))
	}

	// A term starting once the subscription has ended has no period to bill.
	if sub.EndDate != nil && !c.StartDate.Before(*sub.EndDate) {
		return ierr.Validationf("start_date must be before the subscription end %s", sub.EndDate.Format(time.RFC3339))
	}

	if sub.EndDate != nil && c.EndDate != nil && c.EndDate.After(*sub.EndDate) {
		return ierr.Validationf("end_date cannot be after the subscription end %s", sub.EndDate.Format(time.RFC3339))
	}

	return nil
}

// validateScopeEntity checks that a subscription scope names c's subscription and a group scope a price group.
func (s *commitmentService) validateScopeEntity(ctx context.Context, c *commitment.Commitment, sub *subscription.Subscription) error {
	switch c.ScopeEntityType {
	case types.CommitmentScopeSubscription:
		if c.ScopeEntityID != sub.ID {
			return ierr.Validationf("subscription scope must reference subscription %s", sub.ID)
		}
	case types.CommitmentScopeGroup:
		g, err := s.GroupRepo.Get(ctx, c.ScopeEntityID)
		if err != nil {
			return err
		}

		if g.EntityType != types.GroupEntityTypePrice {
			return ierr.Validationf("group %s must be a price group", g.ID)
		}
	}

	return nil
}

// validateTermLines checks the lines c's scope covers over its term: no entitled meter, a single meter where c needs
// one, one bucket size for window measurement, no legacy commitments, and prices billed every subscription period.
func (s *commitmentService) validateTermLines(
	ctx context.Context,
	c *commitment.Commitment,
	overage *dto.CommitmentPriceInput,
	cc *commitmentContext,
	lines []*subscription.SubscriptionLineItem,
) error {
	meters := lo.Uniq(lo.Map(lines, func(li *subscription.SubscriptionLineItem, _ int) string {
		return li.MeterID
	}))
	meterID, err := s.entitledMeter(ctx, cc.Sub, meters)
	if err != nil {
		return err
	}

	if meterID != "" {
		return ierr.Validationf("meter %s has an active entitlement; a commitment cannot cover entitled usage", meterID)
	}

	if (c.Type == types.COMMITMENT_TYPE_QUANTITY || overage != nil) && len(meters) != 1 {
		return ierr.Validationf(
			"quantity commitments and custom overage prices need a scope with exactly one meter, found %d",
			len(meters),
		)
	}

	if c.Measurement == types.CommitmentMeasurementWindow {
		sizes := lo.Uniq(lo.Map(lines, func(li *subscription.SubscriptionLineItem, _ int) types.WindowSize {
			return cc.bucketSize(li)
		}))
		if len(sizes) != 1 || sizes[0] == "" {
			return ierr.Validation("window measurement needs bucketed prices that all share one bucket size")
		}

		if err := rejectCalendarBuckets(sizes[0]); err != nil {
			return err
		}
	}

	for _, li := range lines {
		if li.HasAnyCommitment() {
			return ierr.Validationf("price %s already has a legacy line-item commitment", li.PriceID)
		}

		if err := rejectGroupedBucketedPrice(cc.Prices[li.PriceID], cc.Meters[li.MeterID]); err != nil {
			return err
		}

		if err := rejectOffCadencePrice(cc.Sub, li.PriceID, li.BillingPeriod, li.BillingPeriodCount); err != nil {
			return err
		}
	}

	return nil
}

// rejectGroupedBucketedPrice rejects a bucketed MAX price grouped by a property: normal billing prices each group's
// buckets separately, which commitment allocation cannot reproduce yet.
func rejectGroupedBucketedPrice(p *price.Price, m *meter.Meter) error {
	groupBy := price.BucketedGroupBy(p, m)
	if groupBy == "" {
		return nil
	}

	return ierr.NewErrorf("price %s is bucketed per %q group; commitments cannot cover grouped bucketed prices", p.ID, groupBy).
		WithHint("Commitments cannot cover bucketed prices whose meter groups usage by a property").
		Mark(ierr.ErrValidation)
}

// rejectCalendarBuckets rejects weekly and monthly buckets for window measurement: their usage buckets do not
// start where the commitment's windows do, so usage would be allocated to a shifted window.
func rejectCalendarBuckets(size types.WindowSize) error {
	var reason string
	switch size {
	case types.WindowSizeWeek:
		reason = "window measurement does not support weekly buckets: usage weeks start on Sunday, commitment windows on Monday"
	case types.WindowSizeMonth:
		reason = "window measurement does not support monthly buckets: usage months start on a UTC calendar date, commitment windows at local midnight"
	default:
		return nil
	}

	return ierr.NewError(reason).
		WithHint("Use a daily or shorter bucket size for window commitments").
		Mark(ierr.ErrValidation)
}

// rejectOffCadencePrice rejects a scope price that does not bill every subscription period, since a commitment's
// windows are the subscription's periods.
func rejectOffCadencePrice(sub *subscription.Subscription, priceID string, period types.BillingPeriod, count int) error {
	if period == sub.BillingPeriod && max(count, 1) == max(sub.BillingPeriodCount, 1) {
		return nil
	}

	return ierr.NewErrorf(
		"commitments require scope prices billed every subscription period; price %s bills on another cadence",
		priceID,
	).
		WithHint("Use a price billed every subscription period").
		Mark(ierr.ErrValidation)
}

// validateOverlaps requires the commitments sharing a line with c and still allocating with it to differ in priority,
// and all that overlap it to agree on type, measurement, bucket size and overage price.
func (s *commitmentService) validateOverlaps(
	ctx context.Context,
	c *commitment.Commitment,
	overage *dto.CommitmentPriceInput,
	cc *commitmentContext,
	lines []*subscription.SubscriptionLineItem,
) error {
	existing, err := s.CommitmentRepo.List(ctx, commitment.NewSubscriptionFilter(
		cc.Sub.ID,
		types.CommitmentStatusActive,
		types.CommitmentStatusTerminated,
	))
	if err != nil {
		return err
	}

	cBlockedUntil, err := blocksUntil(cc.Sub, c)
	if err != nil {
		return err
	}

	lineIDs := lo.Map(lines, func(li *subscription.SubscriptionLineItem, _ int) string {
		return li.ID
	})
	for _, other := range existing {
		otherBlockedUntil, err := blocksUntil(cc.Sub, other)
		if err != nil {
			return err
		}

		if endReached(otherBlockedUntil, other.StartDate) ||
			endReached(cBlockedUntil, other.StartDate) ||
			endReached(otherBlockedUntil, c.StartDate) {
			continue
		}

		overlapEnd := otherBlockedUntil
		if cBlockedUntil != nil && (otherBlockedUntil == nil || cBlockedUntil.Before(*otherBlockedUntil)) {
			overlapEnd = cBlockedUntil
		}

		otherLines := scopeLinesInPeriod(other, cc, maxTime(c.StartDate, other.StartDate), overlapEnd)
		otherLineIDs := lo.Map(otherLines, func(li *subscription.SubscriptionLineItem, _ int) string {
			return li.ID
		})
		if len(lo.Intersect(lineIDs, otherLineIDs)) == 0 {
			continue
		}

		allocatingTogether := !other.EndedBy(c.StartDate) && !c.EndedBy(other.StartDate)
		if allocatingTogether && other.Priority == c.Priority {
			return ierr.Validationf("priority %d is already used by overlapping commitment %s", c.Priority, other.ID)
		}

		if other.Type != c.Type || other.Measurement != c.Measurement {
			return ierr.Validationf("overlapping commitment %s has a different type or measurement", other.ID)
		}

		if c.Measurement == types.CommitmentMeasurementWindow {
			want := cc.bucketSize(lines[0])
			sizes := lo.Map(otherLines, func(li *subscription.SubscriptionLineItem, _ int) types.WindowSize {
				return cc.bucketSize(li)
			})
			for _, got := range append(sizes, bucketFor(other, cc)) {
				if got != "" && got != want {
					return ierr.Validationf(
						"overlapping commitment %s measures by window with bucket size %q; this commitment has bucket size %q",
						other.ID,
						got,
						want,
					)
				}
			}
		}

		same, err := s.sameOverageAsInput(ctx, other, overage)
		if err != nil {
			return err
		}

		if !same {
			return ierr.Validationf(
				"overlapping commitment %s has a different overage price; only equal flat overage amounts combine on the same usage",
				other.ID,
			)
		}
	}

	return nil
}

// blocksUntil returns when c stops blocking other commitments on its scope: the end of the billing period holding its
// term end, or that end itself when it never ran. Nil means never.
func blocksUntil(sub *subscription.Subscription, c *commitment.Commitment) (*time.Time, error) {
	endedAt := termEnd(sub, c)
	if endedAt == nil || !endedAt.After(c.StartDate) {
		return endedAt, nil
	}

	end, err := periodEndAt(sub, *endedAt)
	if err != nil {
		return nil, err
	}

	return &end, nil
}

// termEnd returns c's effective end, or the subscription end when c is open-ended.
func termEnd(sub *subscription.Subscription, c *commitment.Commitment) *time.Time {
	if c.OpenEnded() {
		return sub.EndDate
	}

	return c.EffectiveEnd()
}

// endReached reports whether t is at or after end, where a nil end never comes.
func endReached(end *time.Time, t time.Time) bool {
	return end != nil && !end.After(t)
}

// sameOverageAsInput reports whether other and the requested overage price both have none, or flat ones with the
// same amount.
func (s *commitmentService) sameOverageAsInput(
	ctx context.Context,
	other *commitment.Commitment,
	in *dto.CommitmentPriceInput,
) (bool, error) {
	if other.OveragePriceID == nil || in == nil {
		return other.OveragePriceID == nil && in == nil, nil
	}

	if in.BillingModel != types.BILLING_MODEL_FLAT_FEE {
		return false, nil
	}

	p, err := s.PriceRepo.Get(ctx, *other.OveragePriceID)
	if err != nil {
		return false, err
	}

	return p.BillingModel == types.BILLING_MODEL_FLAT_FEE && p.Amount.Equal(lo.FromPtr(in.Amount)), nil
}

// entitledMeter returns the first of the meters carrying an enabled metered entitlement on the
// subscription. The engine bills raw usage, so entitled usage in a commitment's scope would be
// charged instead of staying free; such scopes are rejected.
func (s *commitmentService) entitledMeter(
	ctx context.Context,
	sub *subscription.Subscription,
	meterIDs []string,
) (string, error) {
	agg, err := NewSubscriptionService(s.ServiceParams).GetAggregatedSubscriptionEntitlements(ctx, sub.ID, nil)
	if err != nil {
		return "", err
	}

	entitled := make(map[string]bool, len(agg.Features))
	for _, f := range agg.Features {
		if f.Feature != nil && types.FeatureType(f.Feature.Type) == types.FeatureTypeMetered &&
			f.Feature.MeterID != "" && f.Entitlement != nil && f.Entitlement.IsEnabled {
			entitled[f.Feature.MeterID] = true
		}
	}

	for _, id := range meterIDs {
		if entitled[id] {
			return id, nil
		}
	}

	return "", nil
}

// commitmentCoversLine reports whether c's scope takes in the line, priced at p.
func commitmentCoversLine(c *commitment.Commitment, li *subscription.SubscriptionLineItem, p *price.Price) bool {
	switch c.ScopeEntityType {
	case types.CommitmentScopeSubscription:
		return true
	case types.CommitmentScopeGroup:
		return p != nil && p.GroupID == c.ScopeEntityID
	case types.CommitmentScopeMeter:
		return li.MeterID == c.ScopeEntityID
	case types.CommitmentScopePrice:
		return li.PriceID == c.ScopeEntityID
	}

	return false
}
