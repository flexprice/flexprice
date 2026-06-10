package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// generateBucketStarts returns all bucket start timestamps in the range [start, end)
// for the given bucket size and optional billing anchor. Used by commitment fill logic
// so windowed true-up includes every period window.
//
// For MONTH: if billingAnchor is set, bucket boundaries use that day of month (e.g. 5th);
// if nil, boundaries use the range start's day (e.g. period start 5 Feb → 5 Feb, 5 Mar).
func generateBucketStarts(start, end time.Time, bucketSize types.WindowSize, billingAnchor *time.Time) []time.Time {
	if !end.After(start) {
		return nil
	}
	var out []time.Time
	if bucketSize == types.WindowSizeMonth && billingAnchor == nil {
		first := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
		for t := first; t.Before(end); t = t.AddDate(0, 1, 0) {
			out = append(out, t)
		}
		return out
	}
	t := truncateToBucketStart(start, bucketSize, billingAnchor)
	for t.Before(end) {
		out = append(out, t)
		t = nextBucketStart(t, bucketSize, billingAnchor)
	}
	return out
}

func truncateToBucketStart(t time.Time, bucketSize types.WindowSize, billingAnchor *time.Time) time.Time {
	loc := t.Location()
	if bucketSize == types.WindowSizeMonth && billingAnchor != nil {
		anchorDay := billingAnchor.Day()
		t = t.AddDate(0, 0, -(anchorDay - 1))
		t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
		t = t.AddDate(0, 0, anchorDay-1)
		return t
	}
	switch bucketSize {
	case types.WindowSizeMinute:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	case types.WindowSize15Min:
		m := t.Minute() / 15 * 15
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), m, 0, 0, loc)
	case types.WindowSize30Min:
		m := t.Minute() / 30 * 30
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), m, 0, 0, loc)
	case types.WindowSizeHour:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc)
	case types.WindowSize3Hour:
		h := t.Hour() / 3 * 3
		return time.Date(t.Year(), t.Month(), t.Day(), h, 0, 0, 0, loc)
	case types.WindowSize6Hour:
		h := t.Hour() / 6 * 6
		return time.Date(t.Year(), t.Month(), t.Day(), h, 0, 0, 0, loc)
	case types.WindowSize12Hour:
		h := t.Hour() / 12 * 12
		return time.Date(t.Year(), t.Month(), t.Day(), h, 0, 0, 0, loc)
	case types.WindowSizeDay:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	case types.WindowSizeWeek:
		weekday := int(t.Weekday()) - 1
		if weekday < 0 {
			weekday = 6
		}
		t = t.AddDate(0, 0, -weekday)
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	case types.WindowSizeMonth:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
	default:
		return t
	}
}

func nextBucketStart(t time.Time, bucketSize types.WindowSize, billingAnchor *time.Time) time.Time {
	if bucketSize == types.WindowSizeMonth {
		return t.AddDate(0, 1, 0)
	}
	switch bucketSize {
	case types.WindowSizeMinute:
		return t.Add(1 * time.Minute)
	case types.WindowSize15Min:
		return t.Add(15 * time.Minute)
	case types.WindowSize30Min:
		return t.Add(30 * time.Minute)
	case types.WindowSizeHour:
		return t.Add(1 * time.Hour)
	case types.WindowSize3Hour:
		return t.Add(3 * time.Hour)
	case types.WindowSize6Hour:
		return t.Add(6 * time.Hour)
	case types.WindowSize12Hour:
		return t.Add(12 * time.Hour)
	case types.WindowSizeDay:
		return t.AddDate(0, 0, 1)
	case types.WindowSizeWeek:
		return t.AddDate(0, 0, 7)
	default:
		return t.Add(1 * time.Hour)
	}
}

// commitmentCalculator handles commitment-based pricing calculations for line items
type commitmentCalculator struct {
	logger       *logger.Logger
	priceService PriceService
}

// newCommitmentCalculator creates a new commitment calculator
func newCommitmentCalculator(logger *logger.Logger, priceService PriceService) *commitmentCalculator {
	return &commitmentCalculator{
		logger:       logger,
		priceService: priceService,
	}
}

// normalizeCommitmentToAmount converts quantity-based commitment to amount
// This is the core normalization function that ensures we always compare amounts
func (c *commitmentCalculator) normalizeCommitmentToAmount(
	ctx context.Context,
	lineItem *subscription.SubscriptionLineItem,
	priceObj *price.Price,
) (decimal.Decimal, error) {
	if lineItem.CommitmentType == types.COMMITMENT_TYPE_AMOUNT {
		return lo.FromPtr(lineItem.CommitmentAmount), nil
	}

	if lineItem.CommitmentType == types.COMMITMENT_TYPE_QUANTITY {
		commitmentQuantity := lo.FromPtr(lineItem.CommitmentQuantity)

		// Use existing CalculateCost method to convert quantity to amount
		// This handles all pricing models: flat_fee, tiered, package
		commitmentAmount := c.priceService.CalculateCost(ctx, priceObj, commitmentQuantity)

		return commitmentAmount, nil
	}

	return decimal.Zero, nil
}

// applyCommitmentToLineItem applies commitment logic to a single line item's
// aggregate charge (non-windowed). Returns the adjusted amount and commitment info.
func (c *commitmentCalculator) applyCommitmentToLineItem(
	ctx context.Context,
	lineItem *subscription.SubscriptionLineItem,
	usageCost decimal.Decimal,
	priceObj *price.Price,
) (decimal.Decimal, *types.CommitmentInfo, error) {
	// Normalize commitment to amount for comparison.
	commitmentAmount, err := c.normalizeCommitmentToAmount(ctx, lineItem, priceObj)
	if err != nil {
		return usageCost, nil, err
	}

	overageFactor := lo.FromPtr(lineItem.CommitmentOverageFactor)
	charge, utilized, overage, trueUp := computeCommitmentMath(usageCost, commitmentAmount, overageFactor, lineItem.CommitmentTrueUpEnabled)

	return charge, &types.CommitmentInfo{
		Type:                             lineItem.CommitmentType,
		Amount:                           commitmentAmount,
		Quantity:                         lo.FromPtr(lineItem.CommitmentQuantity),
		OverageFactor:                    lineItem.CommitmentOverageFactor,
		TrueUpEnabled:                    lineItem.CommitmentTrueUpEnabled,
		IsWindowed:                       false,
		ComputedCommitmentUtilizedAmount: utilized,
		ComputedOverageAmount:            overage,
		ComputedTrueUpAmount:             trueUp,
	}, nil
}

// computeCommitmentMath applies the overage / true-up rule for a single charge
// against a commitment, both already expressed as money. Returns
// (finalCharge, utilized, overage, trueUp). It is the single source of truth for
// commitment math — amount- and quantity-typed commitments only differ in how
// the commitment money is derived before calling this.
func computeCommitmentMath(usageCharge, commitmentCharge, overageFactor decimal.Decimal, trueUp bool) (decimal.Decimal, decimal.Decimal, decimal.Decimal, decimal.Decimal) {
	if usageCharge.GreaterThanOrEqual(commitmentCharge) {
		overage := usageCharge.Sub(commitmentCharge).Mul(overageFactor)
		return commitmentCharge.Add(overage), commitmentCharge, overage, decimal.Zero
	}
	if trueUp {
		return commitmentCharge, usageCharge, decimal.Zero, commitmentCharge.Sub(usageCharge)
	}
	return usageCharge, usageCharge, decimal.Zero, decimal.Zero
}

// commitmentParts holds the four outputs of a single commitment calculation:
// final charge plus its utilized / overage / true-up breakdown.
type commitmentParts struct {
	charge   decimal.Decimal
	utilized decimal.Decimal
	overage  decimal.Decimal
	trueUp   decimal.Decimal
}

func (p *commitmentParts) add(o commitmentParts) {
	p.charge = p.charge.Add(o.charge)
	p.utilized = p.utilized.Add(o.utilized)
	p.overage = p.overage.Add(o.overage)
	p.trueUp = p.trueUp.Add(o.trueUp)
}

// applyWindowCommitmentToLineItem applies commitment to windowed usage one window
// at a time. For each window, if its start falls inside a configured commitment
// time bucket, that bucket's own price + commitment apply; otherwise the line
// item's own price + commitment apply (or base rate when the line item carries no
// commitment). windowValues and windowStarts are 1:1; windowStarts may be nil
// (single aggregate window), in which case every window uses the line item.
func (c *commitmentCalculator) applyWindowCommitmentToLineItem(
	ctx context.Context,
	lineItem *subscription.SubscriptionLineItem,
	windowValues []decimal.Decimal,
	windowStarts []time.Time,
	lineItemPrice *price.Price,
) (decimal.Decimal, *types.CommitmentInfo, error) {
	if windowStarts != nil && len(windowStarts) != len(windowValues) {
		return decimal.Zero, nil, ierr.NewError("windowStarts/windowValues length mismatch").
			WithHint("When windowStarts is non-nil, it must have the same length as windowValues").
			WithReportableDetails(map[string]interface{}{
				"window_starts": len(windowStarts),
				"window_values": len(windowValues),
			}).
			Mark(ierr.ErrSystem)
	}

	buckets := lineItem.CommitmentTimeBuckets
	// Cache bucket prices so a bucket spanning many windows is fetched once.
	priceCache := make(map[string]*price.Price)

	var total commitmentParts
	for i, v := range windowValues {
		var (
			parts commitmentParts
			err   error
		)
		if idx, ok := bucketIndexAt(buckets, windowStarts, i); ok {
			parts, err = c.chargeWindowAtBucket(ctx, buckets[idx], v, priceCache)
		} else {
			parts, err = c.chargeWindowAtLineItem(ctx, lineItem, lineItemPrice, v)
		}
		if err != nil {
			return decimal.Zero, nil, err
		}
		total.add(parts)
	}

	return total.charge, &types.CommitmentInfo{
		Type:                             lineItem.CommitmentType,
		Amount:                           lo.FromPtr(lineItem.CommitmentAmount),
		Quantity:                         lo.FromPtr(lineItem.CommitmentQuantity),
		OverageFactor:                    lineItem.CommitmentOverageFactor,
		TrueUpEnabled:                    lineItem.CommitmentTrueUpEnabled,
		IsWindowed:                       true,
		ComputedCommitmentUtilizedAmount: total.utilized,
		ComputedOverageAmount:            total.overage,
		ComputedTrueUpAmount:             total.trueUp,
	}, nil
}

// chargeWindowAtBucket bills a single window using the bucket's own price + commitment.
func (c *commitmentCalculator) chargeWindowAtBucket(
	ctx context.Context,
	b types.TimeOfDayBucket,
	value decimal.Decimal,
	priceCache map[string]*price.Price,
) (commitmentParts, error) {
	if b.PriceID == "" {
		return commitmentParts{}, ierr.NewError("bucket is missing its price").
			WithHint("Every commitment time bucket must be materialized with a price").
			Mark(ierr.ErrSystem)
	}
	bucketPrice, ok := priceCache[b.PriceID]
	if !ok {
		resp, err := c.priceService.GetPrice(ctx, b.PriceID)
		if err != nil {
			return commitmentParts{}, err
		}
		bucketPrice = resp.Price
		priceCache[b.PriceID] = bucketPrice
	}

	baseCharge := c.priceService.CalculateCost(ctx, bucketPrice, value)
	of := lo.FromPtr(b.OverageFactor)

	// Derive the commitment as money: AMOUNT is already money; QUANTITY is the
	// cost of the committed quantity at the bucket price.
	commitmentCharge := b.CommitmentValue
	if b.CommitmentType == types.COMMITMENT_TYPE_QUANTITY {
		commitmentCharge = c.priceService.CalculateCost(ctx, bucketPrice, b.CommitmentValue)
	}
	charge, util, ov, tu := computeCommitmentMath(baseCharge, commitmentCharge, of, b.TrueUpEnabled)
	return commitmentParts{charge: charge, utilized: util, overage: ov, trueUp: tu}, nil
}

// applyToCost applies the line item's commitment — windowed (per window, with
// per-bucket pricing) or aggregate — and returns the adjusted cost plus the
// commitment info. On calculation failure it logs and falls back to the
// uncommitted cost (defaultCost, or the bucketed cost of windowValues when
// defaultCost is zero) with nil info. This is the single dispatch shared by the
// analytics services; billing paths call the underlying methods directly so
// errors propagate instead of falling back.
func (c *commitmentCalculator) applyToCost(
	ctx context.Context,
	lineItem *subscription.SubscriptionLineItem,
	windowValues []decimal.Decimal,
	windowStarts []time.Time,
	priceObj *price.Price,
	defaultCost decimal.Decimal,
) (decimal.Decimal, *types.CommitmentInfo) {
	fallback := func() decimal.Decimal {
		if defaultCost.IsZero() && len(windowValues) > 0 {
			return c.priceService.CalculateBucketedCost(ctx, priceObj, windowValues)
		}
		return defaultCost
	}

	if lineItem.CommitmentWindowed {
		cost, info, err := c.applyWindowCommitmentToLineItem(ctx, lineItem, windowValues, windowStarts, priceObj)
		if err != nil {
			c.logger.Info(ctx, "failed to apply window commitment", "error", err, "line_item_id", lineItem.ID)
			return fallback(), nil
		}
		return cost, info
	}

	cost, info, err := c.applyCommitmentToLineItem(ctx, lineItem, fallback(), priceObj)
	if err != nil {
		c.logger.Info(ctx, "failed to apply commitment", "error", err, "line_item_id", lineItem.ID)
		return fallback(), nil
	}
	return cost, info
}

// chargeWindowAtLineItem bills a single out-of-bucket window using the line item's
// own price + commitment. With no line-item commitment it charges actual usage only.
func (c *commitmentCalculator) chargeWindowAtLineItem(
	ctx context.Context,
	lineItem *subscription.SubscriptionLineItem,
	lineItemPrice *price.Price,
	value decimal.Decimal,
) (commitmentParts, error) {
	baseCharge := c.priceService.CalculateCost(ctx, lineItemPrice, value)
	liCommit, err := c.normalizeCommitmentToAmount(ctx, lineItem, lineItemPrice)
	if err != nil {
		return commitmentParts{}, err
	}
	if liCommit.IsZero() {
		return commitmentParts{charge: baseCharge, utilized: baseCharge}, nil
	}
	of := lo.FromPtr(lineItem.CommitmentOverageFactor)
	charge, util, ov, tu := computeCommitmentMath(baseCharge, liCommit, of, lineItem.CommitmentTrueUpEnabled)
	return commitmentParts{charge: charge, utilized: util, overage: ov, trueUp: tu}, nil
}

// CumulativeSubscriptionCommitmentResult holds the result of applying cumulative subscription commitment
type CumulativeSubscriptionCommitmentResult struct {
	TotalCharge         decimal.Decimal
	CommitmentUtilized  decimal.Decimal
	OverageAmount       decimal.Decimal
	TrueUpAmount        decimal.Decimal
	WithinCommitment    decimal.Decimal
	OverageBase         decimal.Decimal
	CommitmentRemaining decimal.Decimal
}

// applyCumulativeSubscriptionCommitment applies cumulative commitment logic at subscription level.
// Used when commitment duration (e.g. ANNUAL) differs from billing period (e.g. MONTHLY).
// totalPriorBase = sum of base usage from prior invoices (commitment_start to period_start).
func applyCumulativeSubscriptionCommitment(
	commitmentAmount, overageFactor, totalCurrentBase, totalPriorBase decimal.Decimal,
	enableTrueUp, isLastPeriodOfCommitment bool,
	logger *logger.Logger,
) CumulativeSubscriptionCommitmentResult {
	commitmentRemaining := commitmentAmount.Sub(totalPriorBase)
	if commitmentRemaining.LessThan(decimal.Zero) {
		commitmentRemaining = decimal.Zero
	}

	withinCommitment := decimal.Min(totalCurrentBase, commitmentRemaining)
	overageBase := totalCurrentBase.Sub(withinCommitment)

	overageCharge := overageBase.Mul(overageFactor)
	totalCharge := withinCommitment.Add(overageCharge)
	commitmentUtilized := withinCommitment
	trueUpAmount := decimal.Zero

	// True-up: only on last invoice of commitment period, when total usage < commitment
	if isLastPeriodOfCommitment && enableTrueUp {
		totalCumulative := totalPriorBase.Add(totalCurrentBase)
		if totalCumulative.LessThan(commitmentAmount) {
			trueUpAmount = commitmentAmount.Sub(totalCumulative)
			totalCharge = totalCharge.Add(trueUpAmount)
		}
	}

	logger.Debug(context.Background(), "applied cumulative subscription commitment",
		"commitment_amount", commitmentAmount,
		"total_prior_base", totalPriorBase,
		"total_current_base", totalCurrentBase,
		"commitment_remaining", commitmentRemaining,
		"within_commitment", withinCommitment,
		"overage_base", overageBase,
		"overage_charge", overageCharge,
		"true_up", trueUpAmount,
		"total_charge", totalCharge)

	return CumulativeSubscriptionCommitmentResult{
		TotalCharge:         totalCharge,
		CommitmentUtilized:  commitmentUtilized,
		OverageAmount:       overageCharge,
		TrueUpAmount:        trueUpAmount,
		WithinCommitment:    withinCommitment,
		OverageBase:         overageBase,
		CommitmentRemaining: commitmentRemaining,
	}
}

// getSubscriptionCommitmentPeriodBounds returns (commitmentStart, commitmentEnd) for the subscription's commitment period.
// Returns (time.Time{}, time.Time{}, false) if CommitmentDuration is nil or same as billing period.
func getSubscriptionCommitmentPeriodBounds(
	sub *subscription.Subscription,
	periodStart time.Time,
) (commitmentStart, commitmentEnd time.Time, ok bool) {
	if sub.CommitmentDuration == nil {
		return time.Time{}, time.Time{}, false
	}
	cd := types.BillingPeriod(*sub.CommitmentDuration)
	bp := sub.BillingPeriod
	if bp != "" && cd == bp {
		return time.Time{}, time.Time{}, false
	}

	// Commitment starts at subscription start (first billing period)
	commitmentStart = sub.StartDate
	if commitmentStart.IsZero() {
		commitmentStart = sub.CurrentPeriodStart
	}

	// Add duration to get commitment end
	switch cd {
	case types.BILLING_PERIOD_ANNUAL:
		commitmentEnd = commitmentStart.AddDate(1, 0, 0)
	case types.BILLING_PERIOD_QUARTER:
		commitmentEnd = commitmentStart.AddDate(0, 3, 0)
	case types.BILLING_PERIOD_HALF_YEAR:
		commitmentEnd = commitmentStart.AddDate(0, 6, 0)
	case types.BILLING_PERIOD_MONTHLY:
		commitmentEnd = commitmentStart.AddDate(0, 1, 0)
	case types.BILLING_PERIOD_WEEKLY:
		commitmentEnd = commitmentStart.AddDate(0, 0, 7)
	case types.BILLING_PERIOD_DAILY:
		commitmentEnd = commitmentStart.AddDate(0, 0, 1)
	default:
		return time.Time{}, time.Time{}, false
	}

	return commitmentStart, commitmentEnd, true
}

// isLastPeriodOfCommitmentPeriod returns true when the current invoice period closes or extends past the commitment period end.
func isLastPeriodOfCommitmentPeriod(periodEnd, commitmentEnd time.Time) bool {
	return !periodEnd.Before(commitmentEnd)
}

// bucketIndexAt returns the index of the bucket containing window i's start
// time-of-day, and whether one was found. When starts is nil (no per-window
// timestamps) no bucket can match.
func bucketIndexAt(buckets types.TimeOfDayBuckets, starts []time.Time, i int) (int, bool) {
	if starts == nil {
		return 0, false
	}
	for idx, b := range buckets {
		if b.ContainsTime(starts[i]) {
			return idx, true
		}
	}
	return 0, false
}

// bucketIDForPointWindow returns (bucketID, bucketPriceID, ok) for the commitment
// bucket that FULLY contains the analytics window [windowStart, windowStart+window).
// A window that straddles a bucket boundary (possible when the requested window
// size is coarser than the buckets) is left unattributed so per-point breakdown
// and bucket summaries don't misreport. Used by analytics breakdown only.
//
// Containment is checked on the minute-of-day axis: the window's span from the
// bucket start must fit inside the bucket's length. This correctly rejects
// windows of a day or more (which cover every time-of-day) unless the bucket
// spans the whole day, and rejects week/month windows entirely.
func bucketIDForPointWindow(buckets types.TimeOfDayBuckets, windowStart time.Time, window types.WindowSize) (string, string, bool) {
	windowMin := window.ToMinutes()
	if windowMin <= 0 || windowMin > 1440 {
		return "", "", false
	}
	idx, ok := bucketIndexAt(buckets, []time.Time{windowStart}, 0)
	if !ok {
		return "", "", false
	}
	b := buckets[idx]

	bucketLen := b.End.MinuteOfDay() - b.Start.MinuteOfDay()
	if bucketLen <= 0 {
		bucketLen += 1440 // midnight-wrapping bucket
	}
	utc := windowStart.UTC()
	offset := utc.Hour()*60 + utc.Minute() - b.Start.MinuteOfDay()
	if offset < 0 {
		offset += 1440
	}
	if offset+windowMin > bucketLen {
		return "", "", false
	}
	return b.ID, b.PriceID, true
}
