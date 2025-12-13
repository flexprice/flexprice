package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CreateSubscriptionLineItemRequest represents the request to create a subscription line item
type CreateSubscriptionLineItemRequest struct {
	PriceID             string            `json:"price_id" validate:"required"`
	Quantity            decimal.Decimal   `json:"quantity,omitempty" swaggertype:"string"`
	StartDate           *time.Time        `json:"start_date,omitempty"`
	EndDate             *time.Time        `json:"end_date,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	DisplayName         string            `json:"display_name,omitempty"`
	SubscriptionPhaseID *string           `json:"subscription_phase_id,omitempty"`

	// Commitment fields
	CommitmentAmount   *decimal.Decimal     `json:"commitment_amount,omitempty"`
	CommitmentQuantity *decimal.Decimal     `json:"commitment_quantity,omitempty"`
	CommitmentType     types.CommitmentType `json:"commitment_type,omitempty"`
	OverageFactor      *decimal.Decimal     `json:"overage_factor,omitempty"`
	EnableTrueUp       bool                 `json:"enable_true_up,omitempty"`
	IsWindowCommitment bool                 `json:"is_window_commitment,omitempty"`
}

// DeleteSubscriptionLineItemRequest represents the request to delete a subscription line item
type DeleteSubscriptionLineItemRequest struct {
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`
}

type UpdateSubscriptionLineItemRequest struct {
	// EffectiveFrom for the existing line item (if not provided, defaults to now)
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`

	BillingModel types.BillingModel `json:"billing_model,omitempty"`

	// Amount is the new price amount that overrides the original price
	Amount *decimal.Decimal `json:"amount,omitempty" swaggertype:"string"`

	// TierMode determines how to calculate the price for a given quantity
	TierMode types.BillingTier `json:"tier_mode,omitempty"`

	// Tiers determines the pricing tiers for this line item
	Tiers []CreatePriceTier `json:"tiers,omitempty"`

	// TransformQuantity determines how to transform the quantity for this line item
	TransformQuantity *price.TransformQuantity `json:"transform_quantity,omitempty"`

	// Metadata for the new line item
	Metadata map[string]string `json:"metadata,omitempty"`

	// Commitment fields
	CommitmentAmount   *decimal.Decimal     `json:"commitment_amount,omitempty"`
	CommitmentQuantity *decimal.Decimal     `json:"commitment_quantity,omitempty"`
	CommitmentType     types.CommitmentType `json:"commitment_type,omitempty"`
	OverageFactor      *decimal.Decimal     `json:"overage_factor,omitempty"`
	EnableTrueUp       *bool                `json:"enable_true_up,omitempty"`
	IsWindowCommitment *bool                `json:"is_window_commitment,omitempty"`
}

// LineItemParams contains all necessary parameters for creating a line item
type LineItemParams struct {
	Subscription *subscription.Subscription
	Price        *PriceResponse
	Plan         *PlanResponse  // Optional, for plan-based line items
	Addon        *AddonResponse // Optional, for addon-based line items
	EntityType   types.SubscriptionLineItemEntityType
}

// Validate validates the create subscription line item request
func (r *CreateSubscriptionLineItemRequest) Validate() error {
	if r.PriceID == "" {
		return ierr.NewError("price_id is required").
			WithHint("Price ID is required").
			Mark(ierr.ErrValidation)
	}

	// Validate start date is not after end date if both are provided
	if r.StartDate != nil && r.EndDate != nil {
		if r.StartDate.After(*r.EndDate) {
			return ierr.NewError("start_date cannot be after end_date").
				WithHint("Start date cannot be after end date").
				Mark(ierr.ErrValidation)
		}
	}

	// Validate quantity is positive if provided
	if !r.Quantity.IsZero() && r.Quantity.IsNegative() {
		return ierr.NewError("quantity must be positive").
			WithHint("Quantity must be positive").
			Mark(ierr.ErrValidation)
	}

	// Validate commitment fields if provided
	if err := r.validateCommitmentFields(); err != nil {
		return err
	}

	return nil
}

// validateCommitmentFields validates commitment-related fields
func (r *CreateSubscriptionLineItemRequest) validateCommitmentFields() error {
	hasAmountCommitment := r.CommitmentAmount != nil && r.CommitmentAmount.GreaterThan(decimal.Zero)
	hasQuantityCommitment := r.CommitmentQuantity != nil && r.CommitmentQuantity.GreaterThan(decimal.Zero)
	hasCommitment := hasAmountCommitment || hasQuantityCommitment

	if !hasCommitment {
		// No commitment configured, nothing to validate
		return nil
	}

	// Rule 1: Cannot set both commitment_amount and commitment_quantity
	if hasAmountCommitment && hasQuantityCommitment {
		return ierr.NewError("cannot set both commitment_amount and commitment_quantity").
			WithHint("Specify either commitment_amount or commitment_quantity, not both").
			WithReportableDetails(map[string]interface{}{
				"commitment_amount":   r.CommitmentAmount,
				"commitment_quantity": r.CommitmentQuantity,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 2: Commitment type must be valid and match the provided field
	if r.CommitmentType != "" && !r.CommitmentType.Validate() {
		return ierr.NewError("invalid commitment_type").
			WithHint("Commitment type must be either 'amount' or 'quantity'").
			WithReportableDetails(map[string]interface{}{
				"commitment_type": r.CommitmentType,
			}).
			Mark(ierr.ErrValidation)
	}

	// Auto-set commitment type if not provided
	if r.CommitmentType == "" {
		if hasAmountCommitment {
			r.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
		} else if hasQuantityCommitment {
			r.CommitmentType = types.COMMITMENT_TYPE_QUANTITY
		}
	} else {
		// Validate commitment type matches the provided field
		if hasAmountCommitment && r.CommitmentType != types.COMMITMENT_TYPE_AMOUNT {
			return ierr.NewError("commitment_type mismatch").
				WithHint("When commitment_amount is set, commitment_type must be 'amount'").
				WithReportableDetails(map[string]interface{}{
					"commitment_type":   r.CommitmentType,
					"commitment_amount": r.CommitmentAmount,
				}).
				Mark(ierr.ErrValidation)
		}

		if hasQuantityCommitment && r.CommitmentType != types.COMMITMENT_TYPE_QUANTITY {
			return ierr.NewError("commitment_type mismatch").
				WithHint("When commitment_quantity is set, commitment_type must be 'quantity'").
				WithReportableDetails(map[string]interface{}{
					"commitment_type":     r.CommitmentType,
					"commitment_quantity": r.CommitmentQuantity,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Rule 3: Overage factor is required and must be greater than 1.0
	if r.OverageFactor == nil {
		return ierr.NewError("overage_factor is required when commitment is set").
			WithHint("Specify an overage_factor greater than 1.0").
			Mark(ierr.ErrValidation)
	}

	if r.OverageFactor.LessThanOrEqual(decimal.NewFromInt(1)) {
		return ierr.NewError("overage_factor must be greater than 1.0").
			WithHint("Overage factor determines the multiplier for usage beyond commitment").
			WithReportableDetails(map[string]interface{}{
				"overage_factor": r.OverageFactor,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 4: Validate commitment values are positive
	if hasAmountCommitment && r.CommitmentAmount.IsNegative() {
		return ierr.NewError("commitment_amount must be non-negative").
			WithHint("Commitment amount cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"commitment_amount": r.CommitmentAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	if hasQuantityCommitment && r.CommitmentQuantity.IsNegative() {
		return ierr.NewError("commitment_quantity must be non-negative").
			WithHint("Commitment quantity cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"commitment_quantity": r.CommitmentQuantity,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ToSubscriptionLineItem converts the request to a domain subscription line item
func (r *CreateSubscriptionLineItemRequest) ToSubscriptionLineItem(ctx context.Context, params LineItemParams) *subscription.SubscriptionLineItem {
	lineItem := &subscription.SubscriptionLineItem{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:      params.Subscription.ID,
		CustomerID:          params.Subscription.CustomerID,
		PriceID:             r.PriceID,
		PriceType:           params.Price.Type,
		Currency:            params.Subscription.Currency,
		BillingPeriod:       params.Subscription.BillingPeriod,
		InvoiceCadence:      params.Price.InvoiceCadence,
		TrialPeriod:         params.Price.TrialPeriod,
		PriceUnitID:         params.Price.PriceUnitID,
		PriceUnit:           params.Price.PriceUnit,
		EntityType:          params.EntityType,
		DisplayName:         r.DisplayName,
		Metadata:            r.Metadata,
		SubscriptionPhaseID: r.SubscriptionPhaseID,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}

	if params.Price != nil && params.Price.Type == types.PRICE_TYPE_USAGE {
		// Usage-based pricing
		lineItem.MeterID = params.Price.MeterID
		if params.Price.Meter != nil {
			lineItem.MeterDisplayName = params.Price.Meter.Name
			// Meter name takes priority for display name
			lineItem.DisplayName = params.Price.Meter.Name
		}
		lineItem.Quantity = decimal.Zero // Start with zero for usage-based pricing
	} else {
		// Fixed pricing - set default quantity first
		if params.Price != nil {
			lineItem.Quantity = params.Price.GetDefaultQuantity()
		} else {
			lineItem.Quantity = decimal.NewFromInt(1)
		}
	}

	// Set entity-specific fields (only if display name not already set by meter)
	switch params.EntityType {
	case types.SubscriptionLineItemEntityTypePlan:
		if params.Plan != nil {
			lineItem.EntityID = params.Plan.ID
			lineItem.PlanDisplayName = params.Plan.Name
			// Only use plan name if display name not set by meter
			if lineItem.DisplayName == "" {
				lineItem.DisplayName = params.Plan.Name
			}
		}
	case types.SubscriptionLineItemEntityTypeAddon:
		if params.Addon != nil {
			lineItem.EntityID = params.Addon.ID
			// Only use addon name if display name not set by meter
			if lineItem.DisplayName == "" {
				lineItem.DisplayName = params.Addon.Name
			}
			// Add addon-specific metadata
			if lineItem.Metadata == nil {
				lineItem.Metadata = make(map[string]string)
			}
			lineItem.Metadata["addon_id"] = params.Addon.ID
			lineItem.Metadata["subscription_id"] = params.Subscription.ID
			lineItem.Metadata["addon_quantity"] = "1"
			lineItem.Metadata["addon_status"] = string(types.AddonStatusActive)
		}
	}

	// Override quantity if provided in request
	if !r.Quantity.IsZero() {
		lineItem.Quantity = r.Quantity
	}

	// Set dates if provided
	if r.StartDate != nil {
		lineItem.StartDate = r.StartDate.UTC()
	} else {
		lineItem.StartDate = time.Now().UTC()
	}

	if r.EndDate != nil {
		lineItem.EndDate = r.EndDate.UTC()
	}

	// Set commitment fields if provided
	if r.CommitmentAmount != nil {
		lineItem.CommitmentAmount = r.CommitmentAmount
	}
	if r.CommitmentQuantity != nil {
		lineItem.CommitmentQuantity = r.CommitmentQuantity
	}
	if r.CommitmentType != "" {
		lineItem.CommitmentType = r.CommitmentType
	}
	if r.OverageFactor != nil {
		lineItem.OverageFactor = r.OverageFactor
	}
	lineItem.EnableTrueUp = r.EnableTrueUp
	lineItem.IsWindowCommitment = r.IsWindowCommitment

	return lineItem
}

// Validate validates the delete subscription line item request
func (r *DeleteSubscriptionLineItemRequest) Validate() error {
	// Validate effective from date is not in the past if provided
	// Use a small buffer (5 seconds) to account for microsecond-level timing differences
	// when effectiveFrom defaults to time.Now() and StartDate is very recent
	// This allows effectiveFrom to be slightly before StartDate (within buffer) due to timing
	buffer := 5 * time.Second
	if r.EffectiveFrom != nil && r.EffectiveFrom.Before(time.Now().UTC().Add(-buffer)) {
		return ierr.NewError("effective_from must be in the future or present").
			WithHint("Effective from date must be in the future or present").
			WithReportableDetails(map[string]interface{}{
				"effective_from": r.EffectiveFrom,
				"current_time":   time.Now().UTC(),
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Validate validates the update subscription line item request
func (r *UpdateSubscriptionLineItemRequest) Validate() error {
	if r.EffectiveFrom != nil && r.EffectiveFrom.Before(time.Now().UTC()) {
		return ierr.NewError("effective_from must be in the future").
			WithHint("Effective from date must be in the future").
			WithReportableDetails(map[string]interface{}{
				"effective_from": r.EffectiveFrom,
				"current_time":   time.Now().UTC(),
			}).
			Mark(ierr.ErrValidation)
	}

	// If EffectiveFrom is provided, at least one critical field must be present
	if r.EffectiveFrom != nil && !r.ShouldCreateNewLineItem() {
		return ierr.NewError("effective_from requires at least one critical field").
			WithHint("When providing effective_from, you must also provide one of: amount, billing_model, tier_mode, tiers, transform_quantity, or commitment fields").
			Mark(ierr.ErrValidation)
	}

	// Validate commitment fields if provided
	if err := r.validateCommitmentFields(); err != nil {
		return err
	}

	return nil
}

// validateCommitmentFields validates commitment-related fields for update request
func (r *UpdateSubscriptionLineItemRequest) validateCommitmentFields() error {
	hasAmountCommitment := r.CommitmentAmount != nil && r.CommitmentAmount.GreaterThan(decimal.Zero)
	hasQuantityCommitment := r.CommitmentQuantity != nil && r.CommitmentQuantity.GreaterThan(decimal.Zero)
	hasCommitment := hasAmountCommitment || hasQuantityCommitment

	if !hasCommitment {
		// No commitment configured, nothing to validate
		return nil
	}

	// Rule 1: Cannot set both commitment_amount and commitment_quantity
	if hasAmountCommitment && hasQuantityCommitment {
		return ierr.NewError("cannot set both commitment_amount and commitment_quantity").
			WithHint("Specify either commitment_amount or commitment_quantity, not both").
			WithReportableDetails(map[string]interface{}{
				"commitment_amount":   r.CommitmentAmount,
				"commitment_quantity": r.CommitmentQuantity,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 2: Commitment type must be valid and match the provided field
	if r.CommitmentType != "" && !r.CommitmentType.Validate() {
		return ierr.NewError("invalid commitment_type").
			WithHint("Commitment type must be either 'amount' or 'quantity'").
			WithReportableDetails(map[string]interface{}{
				"commitment_type": r.CommitmentType,
			}).
			Mark(ierr.ErrValidation)
	}

	// Auto-set commitment type if not provided
	if r.CommitmentType == "" {
		if hasAmountCommitment {
			r.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
		} else if hasQuantityCommitment {
			r.CommitmentType = types.COMMITMENT_TYPE_QUANTITY
		}
	} else {
		// Validate commitment type matches the provided field
		if hasAmountCommitment && r.CommitmentType != types.COMMITMENT_TYPE_AMOUNT {
			return ierr.NewError("commitment_type mismatch").
				WithHint("When commitment_amount is set, commitment_type must be 'amount'").
				WithReportableDetails(map[string]interface{}{
					"commitment_type":   r.CommitmentType,
					"commitment_amount": r.CommitmentAmount,
				}).
				Mark(ierr.ErrValidation)
		}

		if hasQuantityCommitment && r.CommitmentType != types.COMMITMENT_TYPE_QUANTITY {
			return ierr.NewError("commitment_type mismatch").
				WithHint("When commitment_quantity is set, commitment_type must be 'quantity'").
				WithReportableDetails(map[string]interface{}{
					"commitment_type":     r.CommitmentType,
					"commitment_quantity": r.CommitmentQuantity,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Rule 3: Overage factor is required and must be greater than 1.0
	if r.OverageFactor == nil {
		return ierr.NewError("overage_factor is required when commitment is set").
			WithHint("Specify an overage_factor greater than 1.0").
			Mark(ierr.ErrValidation)
	}

	if r.OverageFactor.LessThanOrEqual(decimal.NewFromInt(1)) {
		return ierr.NewError("overage_factor must be greater than 1.0").
			WithHint("Overage factor determines the multiplier for usage beyond commitment").
			WithReportableDetails(map[string]interface{}{
				"overage_factor": r.OverageFactor,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 4: Validate commitment values are positive
	if hasAmountCommitment && r.CommitmentAmount.IsNegative() {
		return ierr.NewError("commitment_amount must be non-negative").
			WithHint("Commitment amount cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"commitment_amount": r.CommitmentAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	if hasQuantityCommitment && r.CommitmentQuantity.IsNegative() {
		return ierr.NewError("commitment_quantity must be non-negative").
			WithHint("Commitment quantity cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"commitment_quantity": r.CommitmentQuantity,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ShouldCreateNewLineItem checks if the request contains any critical fields that require creating a new line item
func (r *UpdateSubscriptionLineItemRequest) ShouldCreateNewLineItem() bool {
	hasCommitment := (r.CommitmentAmount != nil && r.CommitmentAmount.GreaterThan(decimal.Zero)) ||
		(r.CommitmentQuantity != nil && r.CommitmentQuantity.GreaterThan(decimal.Zero))

	return (r.Amount != nil && !r.Amount.IsZero()) ||
		r.BillingModel != "" ||
		r.TierMode != "" ||
		len(r.Tiers) > 0 ||
		r.TransformQuantity != nil ||
		hasCommitment ||
		r.OverageFactor != nil ||
		r.EnableTrueUp != nil ||
		r.IsWindowCommitment != nil
}

// ToSubscriptionLineItem converts the update request to a domain subscription line item
// This method creates a new line item based on the existing one with updated parameters
func (r *UpdateSubscriptionLineItemRequest) ToSubscriptionLineItem(ctx context.Context, existingLineItem *subscription.SubscriptionLineItem, newPriceID string) *subscription.SubscriptionLineItem {
	// Start with the existing line item as base
	newLineItem := &subscription.SubscriptionLineItem{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:   existingLineItem.SubscriptionID,
		CustomerID:       existingLineItem.CustomerID,
		PriceID:          newPriceID,
		PriceType:        existingLineItem.PriceType,
		Currency:         existingLineItem.Currency,
		BillingPeriod:    existingLineItem.BillingPeriod,
		InvoiceCadence:   existingLineItem.InvoiceCadence,
		TrialPeriod:      existingLineItem.TrialPeriod,
		PriceUnitID:      existingLineItem.PriceUnitID,
		PriceUnit:        existingLineItem.PriceUnit,
		EntityType:       existingLineItem.EntityType,
		EntityID:         existingLineItem.EntityID,
		PlanDisplayName:  existingLineItem.PlanDisplayName,
		MeterID:          existingLineItem.MeterID,
		MeterDisplayName: existingLineItem.MeterDisplayName,
		DisplayName:      existingLineItem.DisplayName,
		Quantity:         existingLineItem.Quantity,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}

	// Set metadata - use provided metadata or keep existing
	if r.Metadata != nil {
		newLineItem.Metadata = r.Metadata
	} else {
		newLineItem.Metadata = existingLineItem.Metadata
	}

	// Set commitment fields - use provided values or keep existing
	if r.CommitmentAmount != nil {
		newLineItem.CommitmentAmount = r.CommitmentAmount
	} else {
		newLineItem.CommitmentAmount = existingLineItem.CommitmentAmount
	}

	if r.CommitmentQuantity != nil {
		newLineItem.CommitmentQuantity = r.CommitmentQuantity
	} else {
		newLineItem.CommitmentQuantity = existingLineItem.CommitmentQuantity
	}

	if r.CommitmentType != "" {
		newLineItem.CommitmentType = r.CommitmentType
	} else {
		newLineItem.CommitmentType = existingLineItem.CommitmentType
	}

	if r.OverageFactor != nil {
		newLineItem.OverageFactor = r.OverageFactor
	} else {
		newLineItem.OverageFactor = existingLineItem.OverageFactor
	}

	if r.EnableTrueUp != nil {
		newLineItem.EnableTrueUp = *r.EnableTrueUp
	} else {
		newLineItem.EnableTrueUp = existingLineItem.EnableTrueUp
	}

	if r.IsWindowCommitment != nil {
		newLineItem.IsWindowCommitment = *r.IsWindowCommitment
	} else {
		newLineItem.IsWindowCommitment = existingLineItem.IsWindowCommitment
	}

	return newLineItem
}
