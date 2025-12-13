package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// lineItemCommitmentCalculator handles commitment-based pricing calculations for line items
type lineItemCommitmentCalculator struct {
	service      *billingService
	priceService PriceService
}

// newLineItemCommitmentCalculator creates a new commitment calculator
func newLineItemCommitmentCalculator(service *billingService) *lineItemCommitmentCalculator {
	return &lineItemCommitmentCalculator{
		service:      service,
		priceService: NewPriceService(service.ServiceParams),
	}
}

// normalizeCommitmentToAmount converts quantity-based commitment to amount
// This is the core normalization function that ensures we always compare amounts
func (c *lineItemCommitmentCalculator) normalizeCommitmentToAmount(
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

		c.service.Logger.Debugw("normalized quantity commitment to amount",
			"line_item_id", lineItem.ID,
			"commitment_quantity", commitmentQuantity,
			"commitment_amount", commitmentAmount,
			"price_id", priceObj.ID)

		return commitmentAmount, nil
	}

	return decimal.Zero, nil
}

// applyCommitmentToLineItem applies commitment logic to a single line item's charges
// Returns the adjusted amount and metadata about the commitment application
func (c *lineItemCommitmentCalculator) applyCommitmentToLineItem(
	ctx context.Context,
	lineItem *subscription.SubscriptionLineItem,
	usageCost decimal.Decimal,
	priceObj *price.Price,
) (decimal.Decimal, types.Metadata, error) {
	metadata := make(types.Metadata)

	// Normalize commitment to amount for comparison
	commitmentAmount, err := c.normalizeCommitmentToAmount(ctx, lineItem, priceObj)
	if err != nil {
		return usageCost, metadata, err
	}

	// Add commitment metadata
	metadata["line_item_commitment"] = "true"
	metadata["commitment_type"] = string(lineItem.CommitmentType)
	metadata["commitment_amount"] = commitmentAmount.String()

	if lineItem.CommitmentType == types.COMMITMENT_TYPE_QUANTITY {
		metadata["commitment_quantity"] = lineItem.CommitmentQuantity.String()
	}

	overageFactor := lo.FromPtr(lineItem.OverageFactor)
	metadata["overage_factor"] = overageFactor.String()
	metadata["enable_true_up"] = fmt.Sprintf("%v", lineItem.EnableTrueUp)
	metadata["is_window_commitment"] = fmt.Sprintf("%v", lineItem.IsWindowCommitment)

	// Calculate final charge based on commitment logic
	var finalCharge decimal.Decimal

	if usageCost.GreaterThanOrEqual(commitmentAmount) {
		// Usage meets or exceeds commitment
		// Charge: commitment + (usage - commitment) * overage_factor
		overage := usageCost.Sub(commitmentAmount)
		overageCharge := overage.Mul(overageFactor)
		finalCharge = commitmentAmount.Add(overageCharge)

		metadata["commitment_utilized"] = commitmentAmount.String()
		metadata["overage_amount"] = overage.String()
		metadata["overage_charge"] = overageCharge.String()

		c.service.Logger.Debugw("usage exceeds commitment, applying overage",
			"line_item_id", lineItem.ID,
			"usage_cost", usageCost,
			"commitment_amount", commitmentAmount,
			"overage", overage,
			"overage_factor", overageFactor,
			"final_charge", finalCharge)
	} else {
		// Usage is less than commitment
		if lineItem.EnableTrueUp {
			// Charge full commitment (true-up)
			finalCharge = commitmentAmount
			metadata["commitment_utilized"] = usageCost.String()
			metadata["true_up_amount"] = commitmentAmount.Sub(usageCost).String()

			c.service.Logger.Debugw("usage below commitment, applying true-up",
				"line_item_id", lineItem.ID,
				"usage_cost", usageCost,
				"commitment_amount", commitmentAmount,
				"true_up", commitmentAmount.Sub(usageCost),
				"final_charge", finalCharge)
		} else {
			// Charge only actual usage (no true-up)
			finalCharge = usageCost
			metadata["commitment_utilized"] = usageCost.String()
			metadata["no_true_up"] = "true"

			c.service.Logger.Debugw("usage below commitment, no true-up",
				"line_item_id", lineItem.ID,
				"usage_cost", usageCost,
				"commitment_amount", commitmentAmount,
				"final_charge", finalCharge)
		}
	}

	return finalCharge, metadata, nil
}

// applyWindowCommitmentToLineItem applies window-based commitment logic
// Processes each bucket individually and applies commitment per window
func (c *lineItemCommitmentCalculator) applyWindowCommitmentToLineItem(
	ctx context.Context,
	lineItem *subscription.SubscriptionLineItem,
	bucketedValues []decimal.Decimal,
	priceObj *price.Price,
) (decimal.Decimal, types.Metadata, error) {
	metadata := make(types.Metadata)

	// Normalize commitment to amount (this is the per-window commitment)
	commitmentAmountPerWindow, err := c.normalizeCommitmentToAmount(ctx, lineItem, priceObj)
	if err != nil {
		return decimal.Zero, metadata, err
	}

	// Add commitment metadata
	metadata["line_item_commitment"] = "true"
	metadata["commitment_type"] = string(lineItem.CommitmentType)
	metadata["commitment_amount_per_window"] = commitmentAmountPerWindow.String()
	metadata["is_window_commitment"] = "true"
	metadata["num_windows"] = fmt.Sprintf("%d", len(bucketedValues))

	if lineItem.CommitmentType == types.COMMITMENT_TYPE_QUANTITY {
		metadata["commitment_quantity_per_window"] = lineItem.CommitmentQuantity.String()
	}

	overageFactor := lo.FromPtr(lineItem.OverageFactor)
	metadata["overage_factor"] = overageFactor.String()

	totalCharge := decimal.Zero
	totalCommitmentUtilized := decimal.Zero
	totalOverage := decimal.Zero
	totalTrueUp := decimal.Zero
	windowsWithOverage := 0
	windowsWithTrueUp := 0

	// Process each window independently
	for i, bucketValue := range bucketedValues {
		// Calculate cost for this window
		windowCost := c.priceService.CalculateCost(ctx, priceObj, bucketValue)

		var windowCharge decimal.Decimal

		if windowCost.GreaterThanOrEqual(commitmentAmountPerWindow) {
			// Window usage meets or exceeds commitment
			overage := windowCost.Sub(commitmentAmountPerWindow)
			overageCharge := overage.Mul(overageFactor)
			windowCharge = commitmentAmountPerWindow.Add(overageCharge)

			totalCommitmentUtilized = totalCommitmentUtilized.Add(commitmentAmountPerWindow)
			totalOverage = totalOverage.Add(overage)
			windowsWithOverage++

			c.service.Logger.Debugw("window usage exceeds commitment",
				"line_item_id", lineItem.ID,
				"window_index", i,
				"bucket_value", bucketValue,
				"window_cost", windowCost,
				"commitment", commitmentAmountPerWindow,
				"overage", overage,
				"window_charge", windowCharge)
		} else {
			// Window usage is less than commitment
			if lineItem.EnableTrueUp {
				// Apply true-up for this window
				windowCharge = commitmentAmountPerWindow
				trueUp := commitmentAmountPerWindow.Sub(windowCost)
				totalTrueUp = totalTrueUp.Add(trueUp)
				windowsWithTrueUp++

				c.service.Logger.Debugw("window usage below commitment, applying true-up",
					"line_item_id", lineItem.ID,
					"window_index", i,
					"bucket_value", bucketValue,
					"window_cost", windowCost,
					"commitment", commitmentAmountPerWindow,
					"true_up", trueUp,
					"window_charge", windowCharge)
			} else {
				// Charge only actual usage for this window
				windowCharge = windowCost

				c.service.Logger.Debugw("window usage below commitment, no true-up",
					"line_item_id", lineItem.ID,
					"window_index", i,
					"bucket_value", bucketValue,
					"window_cost", windowCost,
					"window_charge", windowCharge)
			}

			totalCommitmentUtilized = totalCommitmentUtilized.Add(windowCost)
		}

		totalCharge = totalCharge.Add(windowCharge)
	}

	// Add summary metadata
	metadata["total_commitment_utilized"] = totalCommitmentUtilized.String()
	metadata["total_overage"] = totalOverage.String()
	metadata["windows_with_overage"] = fmt.Sprintf("%d", windowsWithOverage)

	if lineItem.EnableTrueUp {
		metadata["total_true_up"] = totalTrueUp.String()
		metadata["windows_with_true_up"] = fmt.Sprintf("%d", windowsWithTrueUp)
	}

	c.service.Logger.Infow("window commitment applied to line item",
		"line_item_id", lineItem.ID,
		"num_windows", len(bucketedValues),
		"commitment_per_window", commitmentAmountPerWindow,
		"total_charge", totalCharge,
		"windows_with_overage", windowsWithOverage,
		"windows_with_true_up", windowsWithTrueUp)

	return totalCharge, metadata, nil
}
