package dto

import (
	"context"
	"time"

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

	// Set entity-specific fields
	switch params.EntityType {
	case types.SubscriptionLineItemEntityTypePlan:
		if params.Plan != nil {
			lineItem.EntityID = params.Plan.ID
			lineItem.PlanDisplayName = params.Plan.Name
			if lineItem.DisplayName == "" {
				lineItem.DisplayName = params.Plan.Name
			}
		}
	case types.SubscriptionLineItemEntityTypeAddon:
		if params.Addon != nil {
			lineItem.EntityID = params.Addon.ID
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

	// Set quantity if provided, otherwise default to 1 for non-usage prices
	if !r.Quantity.IsZero() {
		lineItem.Quantity = r.Quantity
	} else {
		lineItem.Quantity = params.Price.GetDefaultQuantity()
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

	// Handle usage-based pricing
	if params.Price.Type == types.PRICE_TYPE_USAGE && params.Price.MeterID != "" {
		lineItem.MeterID = params.Price.MeterID
		if params.Price.Meter != nil {
			lineItem.MeterDisplayName = params.Price.Meter.Name
			// If no display name is set, use meter name
			if lineItem.DisplayName == "" {
				lineItem.DisplayName = params.Price.Meter.Name
			}
		}
	}

	return lineItem
}

// Validate validates the delete subscription line item request
func (r *DeleteSubscriptionLineItemRequest) Validate() error {
	
	return nil
}
