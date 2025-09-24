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
	PriceID     string            `json:"price_id" validate:"required"`
	Quantity    decimal.Decimal   `json:"quantity,omitempty"`
	StartDate   *time.Time        `json:"start_date,omitempty"`
	EndDate     *time.Time        `json:"end_date,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	DisplayName string            `json:"display_name,omitempty"`
}

// DeleteSubscriptionLineItemRequest represents the request to delete a subscription line item
type DeleteSubscriptionLineItemRequest struct {
	EndDate *time.Time `json:"end_date,omitempty"`
}

type UpdateSubscriptionLineItemRequest struct {
	// EffectiveFrom for the existing line item (if not provided, defaults to now)
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`

	BillingModel types.BillingModel `json:"billing_model,omitempty"`

	// Amount is the new price amount that overrides the original price
	Amount *decimal.Decimal `json:"amount,omitempty"`

	// TierMode determines how to calculate the price for a given quantity
	TierMode types.BillingTier `json:"tier_mode,omitempty"`

	// Tiers determines the pricing tiers for this line item
	Tiers []CreatePriceTier `json:"tiers,omitempty"`

	// TransformQuantity determines how to transform the quantity for this line item
	TransformQuantity *price.TransformQuantity `json:"transform_quantity,omitempty"`

	// Metadata for the new line item
	Metadata map[string]string `json:"metadata,omitempty"`
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

	return nil
}

// ToSubscriptionLineItem converts the request to a domain subscription line item
func (r *CreateSubscriptionLineItemRequest) ToSubscriptionLineItem(ctx context.Context, params LineItemParams) *subscription.SubscriptionLineItem {
	lineItem := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: params.Subscription.ID,
		CustomerID:     params.Subscription.CustomerID,
		PriceID:        r.PriceID,
		PriceType:      params.Price.Type,
		Currency:       params.Subscription.Currency,
		BillingPeriod:  params.Subscription.BillingPeriod,
		InvoiceCadence: params.Price.InvoiceCadence,
		TrialPeriod:    params.Price.TrialPeriod,
		PriceUnitID:    params.Price.PriceUnitID,
		PriceUnit:      params.Price.PriceUnit,
		EntityType:     params.EntityType,
		DisplayName:    r.DisplayName,
		Metadata:       r.Metadata,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
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

	return lineItem
}

// Validate validates the delete subscription line item request
func (r *DeleteSubscriptionLineItemRequest) Validate() error {

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
			WithHint("When providing effective_from, you must also provide one of: amount, billing_model, tier_mode, tiers, or transform_quantity").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ShouldCreateNewLineItem checks if the request contains any critical fields that require creating a new line item
func (r *UpdateSubscriptionLineItemRequest) ShouldCreateNewLineItem() bool {
	return (r.Amount != nil && !r.Amount.IsZero()) ||
		r.BillingModel != "" ||
		r.TierMode != "" ||
		len(r.Tiers) > 0 ||
		r.TransformQuantity != nil
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

	return newLineItem
}
