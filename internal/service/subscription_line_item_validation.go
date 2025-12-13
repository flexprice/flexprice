package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// validateLineItemCommitment validates commitment configuration for a subscription line item
func (s *subscriptionService) validateLineItemCommitment(ctx context.Context, lineItem *subscription.SubscriptionLineItem, meter *meter.Meter) error {
	// If no commitment is configured, no validation needed
	if !lineItem.HasCommitment() {
		return nil
	}

	// Validate commitment type is valid
	if lineItem.CommitmentType != "" && !lineItem.CommitmentType.Validate() {
		return ierr.NewError("invalid commitment type").
			WithHint("Commitment type must be either 'amount' or 'quantity'").
			WithReportableDetails(map[string]interface{}{
				"commitment_type": lineItem.CommitmentType,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 1: Cannot set both commitment_amount and commitment_quantity
	hasAmountCommitment := lineItem.CommitmentAmount != nil && lineItem.CommitmentAmount.GreaterThan(decimal.Zero)
	hasQuantityCommitment := lineItem.CommitmentQuantity != nil && lineItem.CommitmentQuantity.GreaterThan(decimal.Zero)

	if hasAmountCommitment && hasQuantityCommitment {
		return ierr.NewError("cannot set both commitment_amount and commitment_quantity").
			WithHint("Specify either commitment_amount or commitment_quantity, not both").
			WithReportableDetails(map[string]interface{}{
				"commitment_amount":   lineItem.CommitmentAmount,
				"commitment_quantity": lineItem.CommitmentQuantity,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 2: Overage factor must be greater than 1.0 when commitment is set
	if lineItem.OverageFactor == nil {
		return ierr.NewError("overage_factor is required when commitment is set").
			WithHint("Specify an overage_factor greater than 1.0").
			Mark(ierr.ErrValidation)
	}

	if lineItem.OverageFactor.LessThanOrEqual(decimal.NewFromInt(1)) {
		return ierr.NewError("overage_factor must be greater than 1.0").
			WithHint("Overage factor determines the multiplier for usage beyond commitment").
			WithReportableDetails(map[string]interface{}{
				"overage_factor": lineItem.OverageFactor,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 3: Price must be PRICE_TYPE_USAGE
	if lineItem.PriceType != types.PRICE_TYPE_USAGE {
		return ierr.NewError("commitment is only allowed for usage-based pricing").
			WithHint("Line item must have price_type='usage' to use commitment pricing").
			WithReportableDetails(map[string]interface{}{
				"price_type": lineItem.PriceType,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 4: Window commitment requires meter with bucket_size
	if lineItem.IsWindowCommitment {
		if meter == nil {
			return ierr.NewError("meter is required for window-based commitment").
				WithHint("Window commitment requires a meter with bucket_size configured").
				Mark(ierr.ErrValidation)
		}

		if !meter.HasBucketSize() {
			return ierr.NewError("window commitment requires meter with bucket_size").
				WithHint("Configure bucket_size on the meter to use window-based commitment").
				WithReportableDetails(map[string]interface{}{
					"meter_id":         meter.ID,
					"aggregation_type": meter.Aggregation.Type,
					"bucket_size":      meter.Aggregation.BucketSize,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Rule 5: Validate commitment type matches what was set
	if hasAmountCommitment && lineItem.CommitmentType != types.COMMITMENT_TYPE_AMOUNT {
		return ierr.NewError("commitment_type mismatch").
			WithHint("When commitment_amount is set, commitment_type must be 'amount'").
			WithReportableDetails(map[string]interface{}{
				"commitment_type":   lineItem.CommitmentType,
				"commitment_amount": lineItem.CommitmentAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	if hasQuantityCommitment && lineItem.CommitmentType != types.COMMITMENT_TYPE_QUANTITY {
		return ierr.NewError("commitment_type mismatch").
			WithHint("When commitment_quantity is set, commitment_type must be 'quantity'").
			WithReportableDetails(map[string]interface{}{
				"commitment_type":     lineItem.CommitmentType,
				"commitment_quantity": lineItem.CommitmentQuantity,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// validateSubscriptionLevelCommitment validates that subscription and line items don't both have commitment
func (s *subscriptionService) validateSubscriptionLevelCommitment(sub *subscription.Subscription) error {
	// Check if subscription has commitment
	subscriptionHasCommitment := sub.CommitmentAmount != nil &&
		sub.CommitmentAmount.GreaterThan(decimal.Zero) &&
		sub.OverageFactor != nil &&
		sub.OverageFactor.GreaterThan(decimal.NewFromInt(1))

	if !subscriptionHasCommitment {
		return nil
	}

	// Check if any line item has commitment
	for _, lineItem := range sub.LineItems {
		if lineItem.HasCommitment() {
			return ierr.NewError("cannot set commitment on both subscription and line item").
				WithHint("Use either subscription-level commitment or line-item-level commitment, not both").
				WithReportableDetails(map[string]interface{}{
					"subscription_id":               sub.ID,
					"subscription_commitment":       sub.CommitmentAmount,
					"line_item_id":                  lineItem.ID,
					"line_item_commitment_amount":   lineItem.CommitmentAmount,
					"line_item_commitment_quantity": lineItem.CommitmentQuantity,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}
