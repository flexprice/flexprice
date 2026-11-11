package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// checkLineAttach runs the commitment guards for a usage line joining sub: it may not bring legacy
// commitment fields into a commitment's scope, and its price must suit the commitments it joins.
func checkLineAttach(
	ctx context.Context,
	params ServiceParams,
	sub *subscription.Subscription,
	li *subscription.SubscriptionLineItem,
) error {
	if li == nil || li.PriceType != types.PRICE_TYPE_USAGE {
		return nil
	}

	if err := checkLegacyLineFields(ctx, params, sub, li); err != nil {
		return err
	}

	svc := newCommitmentService(params)
	enabled, err := svc.IsEnabled(ctx)
	if err != nil || !enabled {
		return err
	}

	p, err := params.PriceRepo.Get(ctx, li.PriceID)
	if err != nil {
		return err
	}

	return svc.CheckPriceAttach(ctx, sub, p, li.StartDate, lineEnd(li))
}

// lineEnd returns the line's end date, or nil while it runs open-ended.
func lineEnd(li *subscription.SubscriptionLineItem) *time.Time {
	if li.EndDate.IsZero() {
		return nil
	}

	end := li.EndDate
	return &end
}

// checkLegacyLineFields rejects legacy line-item commitment fields on a line that a commitment still
// covers, since the two would bill the same usage twice.
func checkLegacyLineFields(
	ctx context.Context,
	params ServiceParams,
	sub *subscription.Subscription,
	li *subscription.SubscriptionLineItem,
) error {
	if li == nil || !li.HasAnyCommitment() {
		return nil
	}

	svc := newCommitmentService(params)
	enabled, err := svc.IsEnabled(ctx)
	if err != nil || !enabled {
		return err
	}

	// Serialise with commitment creation, which validates against committed legacy fields.
	if err := svc.lockSubscriptionCommitments(ctx, sub.ID); err != nil {
		return err
	}

	listed, err := params.CommitmentRepo.List(ctx, commitment.NewSubscriptionFilter(
		sub.ID,
		types.CommitmentStatusActive,
		types.CommitmentStatusTerminated,
	))
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

func (s *commitmentService) CheckPriceAttach(
	ctx context.Context,
	sub *subscription.Subscription,
	p *price.Price,
	at time.Time,
	until *time.Time,
) error {
	_, err := s.checkPriceAttach(ctx, sub, p, at, until)
	return err
}

// checkPriceAttach is CheckPriceAttach that also returns the ID of the commitment the price would break. It must run
// inside a transaction.
func (s *commitmentService) checkPriceAttach(
	ctx context.Context,
	sub *subscription.Subscription,
	p *price.Price,
	at time.Time,
	until *time.Time,
) (brokenID string, err error) {
	if p == nil || p.Type != types.PRICE_TYPE_USAGE {
		return "", nil
	}

	enabled, err := s.IsEnabled(ctx)
	if err != nil || !enabled {
		return "", err
	}

	// Serialise with create before reading, so a concurrent create is seen.
	if err := s.lockSubscriptionCommitments(ctx, sub.ID); err != nil {
		return "", err
	}

	covering, err := s.commitmentsCoveringPrice(ctx, sub, p, at, until)
	if err != nil || len(covering) == 0 {
		return "", err
	}

	meterID, err := s.entitledMeter(ctx, sub, []string{p.MeterID})
	if err != nil {
		return "", err
	}

	if meterID != "" {
		return covering[0].ID, ierr.Validationf(
			"price %s is on meter %s, which has an active entitlement; it cannot join a commitment's scope",
			p.ID,
			meterID,
		)
	}

	if err := rejectOffCadencePrice(sub, p.ID, p.BillingPeriod, p.BillingPeriodCount); err != nil {
		return covering[0].ID, err
	}

	m, err := s.MeterRepo.GetMeter(ctx, p.MeterID)
	if err != nil {
		return "", err
	}

	if err := rejectGroupedBucketedPrice(p, m); err != nil {
		return covering[0].ID, err
	}

	cc, err := s.loadCommitmentContext(ctx, sub.ID)
	if err != nil {
		return "", err
	}

	for _, c := range covering {
		if err := s.checkPriceFits(ctx, c, cc, p, at); err != nil {
			return c.ID, err
		}
	}

	return s.checkCoveringCommitmentsAgree(ctx, sub, covering)
}

// commitmentsCoveringPrice returns the commitments whose scope takes in the new usage price and that hold their scope
// after at, through the billing period holding their effective end, and start before until (nil when open-ended).
func (s *commitmentService) commitmentsCoveringPrice(
	ctx context.Context,
	sub *subscription.Subscription,
	p *price.Price,
	at time.Time,
	until *time.Time,
) ([]*commitment.Commitment, error) {
	listed, err := s.CommitmentRepo.List(ctx, commitment.NewSubscriptionFilter(
		sub.ID,
		types.CommitmentStatusActive,
		types.CommitmentStatusTerminated,
	))
	if err != nil {
		return nil, err
	}

	var covering []*commitment.Commitment
	for _, c := range listed {
		blockedUntil, err := blocksUntil(sub, c)
		if err != nil {
			return nil, err
		}

		if endReached(blockedUntil, at) || endReached(blockedUntil, c.StartDate) || endReached(until, c.StartDate) {
			continue
		}

		// A price-scoped commitment only covers its own price, which passed these checks at creation.
		switch c.ScopeEntityType {
		case types.CommitmentScopeSubscription:
		case types.CommitmentScopeGroup:
			if p.GroupID != c.ScopeEntityID {
				continue
			}
		case types.CommitmentScopeMeter:
			if p.MeterID != c.ScopeEntityID {
				continue
			}
		default:
			continue
		}

		covering = append(covering, c)
	}

	return covering, nil
}

// checkPriceFits checks the meter and bucket-size rules for one commitment the new price joins.
func (s *commitmentService) checkPriceFits(
	ctx context.Context,
	c *commitment.Commitment,
	cc *commitmentContext,
	p *price.Price,
	at time.Time,
) error {
	lines := scopeLinesAt(c, cc, maxTime(at, c.StartDate))
	if len(lines) == 0 {
		// Nothing in scope at this instant, e.g. the only line was just replaced: check against the term's lines.
		lines = scopeLinesInPeriod(c, cc, c.StartDate, c.EffectiveEnd())
	}

	if len(lines) == 0 {
		return nil
	}

	if (c.Type == types.COMMITMENT_TYPE_QUANTITY || c.OveragePriceID != nil) && p.MeterID != lines[0].MeterID {
		return ierr.Validationf("commitment %s covers a single meter; price %s is on a different meter", c.ID, p.ID)
	}

	if c.Measurement != types.CommitmentMeasurementWindow {
		return nil
	}

	m, err := s.MeterRepo.GetMeter(ctx, p.MeterID)
	if err != nil {
		return err
	}

	want := cc.bucketSize(lines[0])
	got := price.ResolveBucketSize(p, m)
	if got != want {
		return ierr.Validationf(
			"commitment %s measures by window with bucket size %q; price %s has bucket size %q",
			c.ID,
			want,
			p.ID,
			got,
		)
	}

	return rejectCalendarBuckets(got)
}

// checkCoveringCommitmentsAgree requires commitments that now share the new price and overlap in time to agree on
// type, measurement and overage price, and those still allocating together to differ in priority.
func (s *commitmentService) checkCoveringCommitmentsAgree(
	ctx context.Context,
	sub *subscription.Subscription,
	covering []*commitment.Commitment,
) (string, error) {
	for i, a := range covering {
		for _, b := range covering[i+1:] {
			aBlockedUntil, err := blocksUntil(sub, a)
			if err != nil {
				return "", err
			}

			bBlockedUntil, err := blocksUntil(sub, b)
			if err != nil {
				return "", err
			}

			if endReached(bBlockedUntil, a.StartDate) || endReached(aBlockedUntil, b.StartDate) {
				continue
			}

			allocatingTogether := !b.EndedBy(a.StartDate) && !a.EndedBy(b.StartDate)
			if allocatingTogether && a.Priority == b.Priority {
				return a.ID, ierr.Validationf(
					"commitments %s and %s overlap on the new price with the same priority %d",
					a.ID,
					b.ID,
					a.Priority,
				)
			}

			if a.Type != b.Type || a.Measurement != b.Measurement {
				return a.ID, ierr.Validationf(
					"commitments %s and %s overlap on the new price but differ in type or measurement",
					a.ID,
					b.ID,
				)
			}

			same, err := s.sameOverage(ctx, a, b)
			if err != nil {
				return "", err
			}

			if !same {
				return a.ID, ierr.Validationf(
					"commitments %s and %s overlap on the new price but have different overage prices; only equal flat overage amounts combine on the same usage",
					a.ID,
					b.ID,
				)
			}
		}
	}

	return "", nil
}

// sameOverage reports whether a and b both have no overage price, or flat ones with the same amount.
func (s *commitmentService) sameOverage(ctx context.Context, a, b *commitment.Commitment) (bool, error) {
	if a.OveragePriceID == nil || b.OveragePriceID == nil {
		return a.OveragePriceID == nil && b.OveragePriceID == nil, nil
	}

	pa, err := s.PriceRepo.Get(ctx, *a.OveragePriceID)
	if err != nil {
		return false, err
	}

	pb, err := s.PriceRepo.Get(ctx, *b.OveragePriceID)
	if err != nil {
		return false, err
	}

	return sameOveragePrice(pa, pb), nil
}

// createPriceSyncLine inserts a price-sync usage line item in the same transaction as the commitment attach
// check, under the subscription's commitments lock. A violation is logged and the line skipped, so one
// subscription never fails the whole sync; it reports whether the line was created.
func createPriceSyncLine(
	ctx context.Context,
	params ServiceParams,
	sub *subscription.Subscription,
	p *price.Price,
	li *subscription.SubscriptionLineItem,
) (bool, error) {
	svc := newCommitmentService(params)
	var commitmentID string
	var checkErr error
	err := params.DB.WithTx(ctx, func(txCtx context.Context) error {
		commitmentID, checkErr = svc.checkPriceAttach(txCtx, sub, p, li.StartDate, lineEnd(li))
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

// checkLineDetach rejects ending a usage line item at endDate when a binding commitment still running then, a pending
// termination included, scopes it and the other lines in its scope do not cover the rest of its term without a gap,
// which would silently stop billing its minimum. Call it inside the write's transaction, after any replacement line
// exists.
func checkLineDetach(
	ctx context.Context,
	params ServiceParams,
	sub *subscription.Subscription,
	li *subscription.SubscriptionLineItem,
	endDate time.Time,
) error {
	if li == nil || li.PriceType != types.PRICE_TYPE_USAGE {
		return nil
	}

	svc := newCommitmentService(params)
	enabled, err := svc.IsEnabled(ctx)
	if err != nil || !enabled {
		return err
	}

	// Serialise with other scope changes, so two removals cannot each see the other's line as cover.
	if err := svc.lockSubscriptionCommitments(ctx, sub.ID); err != nil {
		return err
	}

	listed, err := params.CommitmentRepo.List(ctx, commitment.NewSubscriptionFilter(
		sub.ID,
		types.CommitmentStatusActive,
		types.CommitmentStatusTerminated,
	))
	if err != nil || len(listed) == 0 {
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

	for _, c := range listed {
		end := termEnd(sub, c)
		from := maxTime(endDate, c.StartDate)
		if !c.Binding || endReached(end, from) || endReached(end, li.StartDate) || !commitmentCoversLine(c, li, p) {
			continue
		}

		remaining := lo.Filter(cc.Lines, func(other *subscription.SubscriptionLineItem, _ int) bool {
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
