package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
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

		c.logger.Debugw("normalized quantity commitment to amount",
			"line_item_id", lineItem.ID,
			"commitment_quantity", commitmentQuantity,
			"commitment_amount", commitmentAmount,
			"price_id", priceObj.ID)

		return commitmentAmount, nil
	}

	return decimal.Zero, nil
}

// applyCommitmentToLineItem applies commitment logic to a single line item's charges
// Returns the adjusted amount and commitment info about the commitment application
func (c *commitmentCalculator) applyCommitmentToLineItem(
	ctx context.Context,
	lineItem *subscription.SubscriptionLineItem,
	usageCost decimal.Decimal,
	priceObj *price.Price,
) (decimal.Decimal, *types.CommitmentInfo, error) {
	// Normalize commitment to amount for comparison
	commitmentAmount, err := c.normalizeCommitmentToAmount(ctx, lineItem, priceObj)
	if err != nil {
		return usageCost, nil, err
	}

	overageFactor := lo.FromPtr(lineItem.CommitmentOverageFactor)
	info := &types.CommitmentInfo{
		Type:          lineItem.CommitmentType,
		Amount:        commitmentAmount,
		Quantity:      lo.FromPtr(lineItem.CommitmentQuantity),
		OverageFactor: lineItem.CommitmentOverageFactor,
		TrueUpEnabled: lineItem.CommitmentTrueUpEnabled,
		IsWindowed:    false,
	}

	// Calculate final charge based on commitment logic
	var finalCharge decimal.Decimal

	if usageCost.GreaterThanOrEqual(commitmentAmount) {
		// Usage meets or exceeds commitment
		// Charge: commitment + (usage - commitment) * overage_factor
		overage := usageCost.Sub(commitmentAmount)
		overageCharge := overage.Mul(overageFactor)
		finalCharge = commitmentAmount.Add(overageCharge)

		info.ComputedCommitmentUtilizedAmount = commitmentAmount
		info.ComputedOverageAmount = overageCharge
		info.ComputedTrueUpAmount = decimal.Zero

		c.logger.Debugw("usage exceeds commitment, applying overage",
			"line_item_id", lineItem.ID,
			"usage_cost", usageCost,
			"commitment_amount", commitmentAmount,
			"overage", overage,
			"overage_factor", overageFactor,
			"final_charge", finalCharge)
	} else {
		// Usage is less than commitment
		if lineItem.CommitmentTrueUpEnabled {
			// Charge full commitment (true-up)
			finalCharge = commitmentAmount
			info.ComputedCommitmentUtilizedAmount = usageCost
			info.ComputedOverageAmount = decimal.Zero
			info.ComputedTrueUpAmount = commitmentAmount.Sub(usageCost)

			c.logger.Debugw("usage below commitment, applying true-up",
				"line_item_id", lineItem.ID,
				"usage_cost", usageCost,
				"commitment_amount", commitmentAmount,
				"true_up", info.ComputedTrueUpAmount,
				"final_charge", finalCharge)
		} else {
			// Charge only actual usage (no true-up)
			finalCharge = usageCost
			info.ComputedCommitmentUtilizedAmount = usageCost
			info.ComputedOverageAmount = decimal.Zero
			info.ComputedTrueUpAmount = decimal.Zero

			c.logger.Debugw("usage below commitment, no true-up",
				"line_item_id", lineItem.ID,
				"usage_cost", usageCost,
				"commitment_amount", commitmentAmount,
				"final_charge", finalCharge)
		}
	}

	return finalCharge, info, nil
}

// applyWindowCommitmentToLineItem applies window-based commitment logic
// Processes each bucket individually and applies commitment per window
func (c *commitmentCalculator) applyWindowCommitmentToLineItem(
	ctx context.Context,
	lineItem *subscription.SubscriptionLineItem,
	bucketedValues []decimal.Decimal,
	priceObj *price.Price,
) (decimal.Decimal, *types.CommitmentInfo, error) {
	// Normalize commitment to amount (this is the per-window commitment)
	commitmentAmountPerWindow, err := c.normalizeCommitmentToAmount(ctx, lineItem, priceObj)
	if err != nil {
		return decimal.Zero, nil, err
	}

	overageFactor := lo.FromPtr(lineItem.CommitmentOverageFactor)
	info := &types.CommitmentInfo{
		Type:          lineItem.CommitmentType,
		Amount:        commitmentAmountPerWindow, // This is per window
		Quantity:      lo.FromPtr(lineItem.CommitmentQuantity),
		OverageFactor: lineItem.CommitmentOverageFactor,
		TrueUpEnabled: lineItem.CommitmentTrueUpEnabled,
		IsWindowed:    true,
	}

	totalCharge := decimal.Zero
	totalCommitmentUtilized := decimal.Zero
	totalOverage := decimal.Zero
	totalTrueUp := decimal.Zero
	windowsWithOverage := 0
	windowsWithTrueUp := 0

	// Process each window independently
	for _, bucketValue := range bucketedValues {
		// Calculate cost for this window
		windowCost := c.priceService.CalculateCost(ctx, priceObj, bucketValue)

		var windowCharge decimal.Decimal

		if windowCost.GreaterThanOrEqual(commitmentAmountPerWindow) {
			// Window usage meets or exceeds commitment
			overage := windowCost.Sub(commitmentAmountPerWindow)
			overageCharge := overage.Mul(overageFactor)
			windowCharge = commitmentAmountPerWindow.Add(overageCharge)

			totalCommitmentUtilized = totalCommitmentUtilized.Add(commitmentAmountPerWindow)
			totalOverage = totalOverage.Add(overageCharge) // Storing charge, not amount
			windowsWithOverage++
		} else {
			// Window usage is less than commitment
			if lineItem.CommitmentTrueUpEnabled {
				// Apply true-up for this window
				windowCharge = commitmentAmountPerWindow
				trueUp := commitmentAmountPerWindow.Sub(windowCost)
				totalTrueUp = totalTrueUp.Add(trueUp)
				windowsWithTrueUp++

			} else {
				// Charge only actual usage for this window
				windowCharge = windowCost
			}

			totalCommitmentUtilized = totalCommitmentUtilized.Add(windowCost)
		}

		totalCharge = totalCharge.Add(windowCharge)
	}

	info.ComputedCommitmentUtilizedAmount = totalCommitmentUtilized
	info.ComputedOverageAmount = totalOverage
	info.ComputedTrueUpAmount = totalTrueUp

	c.logger.Infow("window commitment applied to line item",
		"line_item_id", lineItem.ID,
		"num_windows", len(bucketedValues),
		"commitment_per_window", commitmentAmountPerWindow,
		"total_charge", totalCharge,
		"windows_with_overage", windowsWithOverage,
		"windows_with_true_up", windowsWithTrueUp)

	return totalCharge, info, nil
}
