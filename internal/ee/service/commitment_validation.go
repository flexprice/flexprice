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
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type commitmentContext struct {
	Sub    *subscription.Subscription
	Items  []*subscription.SubscriptionLineItem
	Prices map[string]*price.Price
	Meters map[string]*meter.Meter
}

// loadCommitmentContext loads a subscription with every line item it has had, and their prices and meters,
// so a term reaching back before the current period still resolves its scope.
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
	items, err := s.SubscriptionLineItemRepo.List(ctx, lf)
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
		f.AllowExpiredPrices = true
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

// scopeLinesInPeriod returns the line items in scope at any point of [from, to), ordered for fill; a nil to takes
// every line joining after from.
func scopeLinesInPeriod(
	scopeType types.CommitmentScopeType,
	scopeID string,
	cc *commitmentContext,
	from time.Time,
	to *time.Time,
) []*subscription.SubscriptionLineItem {
	if endReached(to, from) {
		return nil
	}

	at := []time.Time{from}
	for _, li := range cc.Items {
		if li.StartDate.After(from) && !endReached(to, li.StartDate) {
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
	accepted := []types.SubscriptionStatus{types.SubscriptionStatusActive, types.SubscriptionStatusTrialing}
	if opts.allowPendingSubscription {
		accepted = append(accepted, types.SubscriptionStatusIncomplete, types.SubscriptionStatusDraft)
	}

	if !lo.Contains(accepted, sub.SubscriptionStatus) {
		return ierr.Validationf("subscription %s must be active or trialing", sub.ID)
	}

	// Term edges are whole seconds (ApplyDefaults), so a subscription starting mid-second accepts a
	// commitment aligned to that second's start.
	if req.StartDate.Before(sub.StartDate.Truncate(time.Second)) {
		return ierr.Validationf("start_date cannot be before the subscription start %s", sub.StartDate.Format(time.RFC3339))
	}

	// A term starting once the subscription has ended has no period to bill, open-ended or not.
	if sub.EndDate != nil && !req.StartDate.Before(*sub.EndDate) {
		return ierr.Validationf("start_date must be before the subscription end %s", sub.EndDate.Format(time.RFC3339))
	}

	if sub.EndDate != nil && req.EndDate != nil && req.EndDate.After(*sub.EndDate) {
		return ierr.Validationf("end_date cannot be after the subscription end %s", sub.EndDate.Format(time.RFC3339))
	}

	if req.Scope.EntityType == types.CommitmentScopeSubscription && req.Scope.EntityID != sub.ID {
		return ierr.Validationf("subscription scope must reference subscription %s", sub.ID)
	}

	if req.Scope.EntityType == types.CommitmentScopeGroup {
		g, err := s.GroupRepo.Get(ctx, req.Scope.EntityID)
		if err != nil {
			return err
		}

		if g.EntityType != types.GroupEntityTypePrice {
			return ierr.Validationf("group %s must be a price group", g.ID)
		}
	}

	// The commitment cannot bill before the subscription exists, and its start may sit a fraction of a
	// second below the mid-second line starts (term edges are whole seconds), so coverage is checked
	// from whichever is later.
	lines := resolveScopeLines(req.Scope.EntityType, req.Scope.EntityID, cc, maxTime(req.StartDate, sub.StartDate))
	if len(lines) == 0 {
		return ierr.Validationf(
			"scope %s/%s covers no usage prices on the subscription",
			req.Scope.EntityType,
			req.Scope.EntityID,
		)
	}

	// Lines scheduled to join later in the term must satisfy the same rules as those present at start.
	term := &commitment.Commitment{StartDate: req.StartDate, EndDate: req.EndDate}
	termLines := scopeLinesInPeriod(req.Scope.EntityType, req.Scope.EntityID, cc, term.StartDate, term.EffectiveEnd())
	meters := lo.Uniq(lo.Map(termLines, func(li *subscription.SubscriptionLineItem, _ int) string {
		return li.MeterID
	}))
	if meterID, err := s.entitledMeter(ctx, sub, meters); err != nil {
		return err
	} else if meterID != "" {
		return ierr.Validationf("meter %s has an active entitlement; a commitment cannot cover entitled usage", meterID)
	}

	if (req.Type == types.COMMITMENT_TYPE_QUANTITY || req.OveragePrice != nil) && len(meters) != 1 {
		return ierr.Validationf(
			"quantity commitments and custom overage prices need a scope with exactly one meter, found %d",
			len(meters),
		)
	}

	if req.Measurement == types.CommitmentMeasurementWindow {
		sizes := lo.Uniq(lo.Map(termLines, func(li *subscription.SubscriptionLineItem, _ int) types.WindowSize {
			return price.ResolveBucketSize(cc.Prices[li.PriceID], cc.Meters[li.MeterID])
		}))
		if len(sizes) != 1 || sizes[0] == "" {
			return ierr.Validation("window measurement needs bucketed prices that all share one bucket size")
		}

		if err := rejectCalendarBuckets(sizes[0]); err != nil {
			return err
		}
	}

	if sub.CommitmentAmount != nil && sub.CommitmentAmount.IsPositive() {
		return ierr.Validation("subscription has a legacy subscription-level commitment; remove it before adding a commitment")
	}

	for _, li := range termLines {
		if li.HasAnyCommitment() {
			return ierr.Validationf("price %s already has a legacy line-item commitment", li.PriceID)
		}

		if err := rejectGroupedBucketedPrice(cc.Prices[li.PriceID], cc.Meters[li.MeterID]); err != nil {
			return err
		}

		if !billsEverySubscriptionPeriod(sub, li.BillingPeriod, li.BillingPeriodCount) {
			return ierr.NewErrorf(
				"commitments require scope prices billed every subscription period; price %s bills on another cadence",
				li.PriceID,
			).
				WithHint("commitments require scope prices billed every subscription period").
				Mark(ierr.ErrValidation)
		}
	}

	return s.validateOverlaps(ctx, req, term, cc, termLines)
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

// billsEverySubscriptionPeriod reports whether a price bills on the subscription's own cadence, so its invoice
// window is the subscription period.
func billsEverySubscriptionPeriod(sub *subscription.Subscription, period types.BillingPeriod, count int) bool {
	return period == sub.BillingPeriod && max(count, 1) == max(sub.BillingPeriodCount, 1)
}

// validateOverlaps requires commitments sharing a line item and still allocating to differ in priority, and
// all overlapping ones to agree on type, measurement, bucket size (for windows) and overage rate; for the latter
// a commitment counts through the end of the billing period holding its effective end.
func (s *commitmentService) validateOverlaps(
	ctx context.Context,
	req *dto.CreateCommitmentRequest,
	term *commitment.Commitment,
	cc *commitmentContext,
	lines []*subscription.SubscriptionLineItem,
) error {
	f := commitment.NewNoLimitFilter()
	f.SubscriptionIDs = []string{cc.Sub.ID}
	f.Statuses = []types.CommitmentStatus{types.CommitmentStatusActive, types.CommitmentStatusTerminated}
	existing, err := s.CommitmentRepo.List(ctx, f)
	if err != nil {
		return err
	}

	reqBlockedUntil, err := blocksUntil(cc.Sub, term)
	if err != nil {
		return err
	}

	lineIDs := lo.Map(lines, func(li *subscription.SubscriptionLineItem, _ int) string {
		return li.ID
	})
	for _, other := range existing {
		blockedUntil, err := blocksUntil(cc.Sub, other)
		if err != nil {
			return err
		}

		if endReached(blockedUntil, other.StartDate) ||
			endReached(reqBlockedUntil, other.StartDate) ||
			endReached(blockedUntil, req.StartDate) {
			continue
		}

		overlapEnd := blockedUntil
		if reqBlockedUntil != nil && (blockedUntil == nil || reqBlockedUntil.Before(*blockedUntil)) {
			overlapEnd = reqBlockedUntil
		}

		otherLines := scopeLinesInPeriod(
			other.ScopeEntityType,
			other.ScopeEntityID,
			cc,
			maxTime(req.StartDate, other.StartDate),
			overlapEnd,
		)
		otherLineIDs := lo.Map(otherLines, func(li *subscription.SubscriptionLineItem, _ int) string {
			return li.ID
		})
		if len(lo.Intersect(lineIDs, otherLineIDs)) == 0 {
			continue
		}

		allocatingTogether := !other.EndedBy(req.StartDate) && !term.EndedBy(other.StartDate)
		if allocatingTogether && other.Priority == req.Priority {
			return ierr.Validationf("priority %d is already used by overlapping commitment %s", req.Priority, other.ID)
		}

		if other.Type != req.Type || other.Measurement != req.Measurement {
			return ierr.Validationf("overlapping commitment %s has a different type or measurement", other.ID)
		}

		if req.Measurement == types.CommitmentMeasurementWindow {
			want := price.ResolveBucketSize(cc.Prices[lines[0].PriceID], cc.Meters[lines[0].MeterID])
			sizes := lo.Map(otherLines, func(li *subscription.SubscriptionLineItem, _ int) types.WindowSize {
				return price.ResolveBucketSize(cc.Prices[li.PriceID], cc.Meters[li.MeterID])
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

		same, err := s.sameOverage(ctx, other, req)
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

// blocksUntil returns when the commitment stops blocking conflicting commitments on its scope: the end of the
// billing period holding its term end (that invoice still bills it); nil = forever, its own end when it never ran.
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

// termEnd returns the commitment's effective end; an unending one stops with its subscription, so it has an end
// only when the subscription does.
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

// sameOverage reports whether both sides have no overage price, or flat ones with the same amount.
func (s *commitmentService) sameOverage(
	ctx context.Context,
	other *commitment.Commitment,
	req *dto.CreateCommitmentRequest,
) (bool, error) {
	if other.OveragePriceID == nil || req.OveragePrice == nil {
		return other.OveragePriceID == nil && req.OveragePrice == nil, nil
	}

	if req.OveragePrice.BillingModel != types.BILLING_MODEL_FLAT_FEE {
		return false, nil
	}

	p, err := s.PriceRepo.Get(ctx, *other.OveragePriceID)
	if err != nil {
		return false, err
	}

	return p.BillingModel == types.BILLING_MODEL_FLAT_FEE && p.Amount.Equal(lo.FromPtr(req.OveragePrice.Amount)), nil
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

// checkLineItemCommitmentAttach runs the commitment price-attach guard for a usage line item joining sub.
// Other lines cost nothing; with commitments disabled the cost is the cached settings read.
func checkLineItemCommitmentAttach(
	ctx context.Context,
	params ServiceParams,
	sub *subscription.Subscription,
	li *subscription.SubscriptionLineItem,
) error {
	if li == nil || li.PriceType != types.PRICE_TYPE_USAGE {
		return nil
	}

	svc := NewCommitmentService(params)
	enabled, err := svc.IsEnabled(ctx)
	if err != nil || !enabled {
		return err
	}

	p, err := params.PriceRepo.Get(ctx, li.PriceID)
	if err != nil {
		return err
	}

	return svc.CheckPriceAttach(ctx, sub, p, li.StartDate, lo.EmptyableToPtr(li.EndDate))
}

// rejectLegacyCommitmentInScope rejects legacy line-item commitment fields on a line that a commitment still
// covers, since the two would bill the same usage twice.
func rejectLegacyCommitmentInScope(
	ctx context.Context,
	params ServiceParams,
	sub *subscription.Subscription,
	li *subscription.SubscriptionLineItem,
) error {
	if li == nil || !li.HasAnyCommitment() {
		return nil
	}

	svc := NewCommitmentService(params)
	enabled, err := svc.IsEnabled(ctx)
	if err != nil || !enabled {
		return err
	}

	// Serialise with commitment creation, which validates against committed legacy fields.
	if err := params.DB.LockWithWait(ctx, postgres.LockRequest{Key: "commitments:" + sub.ID}); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to lock subscription commitments").
			Mark(ierr.ErrInternal)
	}

	f := commitment.NewNoLimitFilter()
	f.SubscriptionIDs = []string{sub.ID}
	f.Statuses = []types.CommitmentStatus{types.CommitmentStatusActive, types.CommitmentStatusTerminated}
	listed, err := params.CommitmentRepo.List(ctx, f)
	if err != nil || len(listed) == 0 {
		return err
	}

	p, err := params.PriceRepo.Get(ctx, li.PriceID)
	if err != nil {
		return err
	}

	for _, c := range listed {
		overlaps, err := overlapsLine(sub, c, li)
		if err != nil {
			return err
		}

		if overlaps && commitmentCoversLine(c, li, p) {
			return ierr.Validationf(
				"line item %s is covered by commitment %s; legacy line-item commitment fields are not allowed",
				li.ID,
				c.ID,
			)
		}
	}

	return nil
}

// overlapsLine reports whether the line's billing interval overlaps the commitment's term, extended to the end
// of the billing period holding its effective end.
func overlapsLine(sub *subscription.Subscription, c *commitment.Commitment, li *subscription.SubscriptionLineItem) (bool, error) {
	blockedUntil, err := blocksUntil(sub, c)
	if err != nil {
		return false, err
	}

	if endReached(blockedUntil, li.StartDate) {
		return false, nil
	}

	return li.EndDate.IsZero() || c.StartDate.Before(li.EndDate), nil
}

func commitmentCoversLine(c *commitment.Commitment, li *subscription.SubscriptionLineItem, p *price.Price) bool {
	switch c.ScopeEntityType {
	case types.CommitmentScopeSubscription:
		return true
	case types.CommitmentScopeGroup:
		return p.GroupID == c.ScopeEntityID
	case types.CommitmentScopeMeter:
		return li.MeterID == c.ScopeEntityID
	case types.CommitmentScopePrice:
		return li.PriceID == c.ScopeEntityID
	}

	return false
}

// createSyncLineItem inserts a price-sync usage line item in the same transaction as the commitment attach
// check, under the subscription's commitments lock. A violation is logged and the line skipped, so one
// subscription never fails the whole sync; it reports whether the line was created.
func createSyncLineItem(
	ctx context.Context,
	params ServiceParams,
	sub *subscription.Subscription,
	p *price.Price,
	li *subscription.SubscriptionLineItem,
) (bool, error) {
	svc := &commitmentService{ServiceParams: params}
	var commitmentID string
	var checkErr error
	err := params.DB.WithTx(ctx, func(txCtx context.Context) error {
		commitmentID, checkErr = svc.checkPriceAttach(txCtx, sub, p, li.StartDate, lo.EmptyableToPtr(li.EndDate))
		if checkErr != nil {
			return checkErr
		}

		return params.SubscriptionLineItemRepo.Create(txCtx, li)
	})
	if checkErr != nil && ierr.IsValidation(checkErr) {
		params.Logger.Info(ctx, "commitment.sync_price_skipped",
			"operation", "commitment.sync_price_skipped",
			"subscription_id", sub.ID, "price_id", p.ID, "commitment_id", commitmentID, "reason", checkErr.Error())
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

// checkLineItemCommitmentDetach rejects ending a usage line item at endDate when an active binding commitment
// scopes it and the other lines in its scope do not cover the rest of its term without a gap, which would
// silently stop billing its minimum. Call it inside the write's transaction, after any replacement line exists.
func checkLineItemCommitmentDetach(
	ctx context.Context,
	params ServiceParams,
	sub *subscription.Subscription,
	li *subscription.SubscriptionLineItem,
	endDate time.Time,
) error {
	if li == nil || li.PriceType != types.PRICE_TYPE_USAGE {
		return nil
	}

	svc := &commitmentService{ServiceParams: params}
	enabled, err := svc.IsEnabled(ctx)
	if err != nil || !enabled {
		return err
	}

	// Serialise with other scope changes, so two removals cannot each see the other's line as cover.
	if err := params.DB.LockWithWait(ctx, postgres.LockRequest{Key: "commitments:" + sub.ID}); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to lock subscription commitments").
			Mark(ierr.ErrInternal)
	}

	f := commitment.NewNoLimitFilter()
	f.SubscriptionIDs = []string{sub.ID}
	f.Statuses = []types.CommitmentStatus{types.CommitmentStatusActive}
	active, err := params.CommitmentRepo.List(ctx, f)
	if err != nil || len(active) == 0 {
		return err
	}

	cc, err := svc.loadCommitmentContext(ctx, sub.ID)
	if err != nil {
		return err
	}

	p := cc.Prices[li.PriceID]
	if p == nil {
		if p, err = params.PriceRepo.Get(ctx, li.PriceID); err != nil {
			return err
		}
	}

	for _, c := range active {
		end := termEnd(sub, c)
		from := maxTime(endDate, c.StartDate)
		if !c.Binding || endReached(end, from) || endReached(end, li.StartDate) || !commitmentCoversLine(c, li, p) {
			continue
		}

		remaining := lo.Filter(cc.Items, func(other *subscription.SubscriptionLineItem, _ int) bool {
			op := cc.Prices[other.PriceID]
			return other.ID != li.ID && other.PriceType == types.PRICE_TYPE_USAGE && op != nil && commitmentCoversLine(c, other, op)
		})
		if gap, uncovered := coverageGap(remaining, from, end); uncovered {
			return ierr.Validationf(
				"ending line item %s breaks binding commitment %s's scope coverage at %s, inside its term; keep a covering line through the term or terminate the commitment first",
				li.ID,
				c.ID,
				gap.Format(time.RFC3339),
			)
		}
	}

	return nil
}

// coverageGap returns the first instant of [from, to) the lines leave uncovered, and whether one exists; a nil to
// needs cover forever.
func coverageGap(lines []*subscription.SubscriptionLineItem, from time.Time, to *time.Time) (time.Time, bool) {
	sorted := append([]*subscription.SubscriptionLineItem(nil), lines...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].StartDate.Before(sorted[j].StartDate) })

	covered := from
	for _, li := range sorted {
		if endReached(to, covered) {
			return time.Time{}, false
		}

		if li.StartDate.After(covered) {
			return covered, true
		}

		if li.EndDate.IsZero() {
			return time.Time{}, false
		}

		covered = maxTime(covered, li.EndDate)
	}

	if !endReached(to, covered) {
		return covered, true
	}

	return time.Time{}, false
}
