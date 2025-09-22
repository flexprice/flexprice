package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// AddSubscriptionLineItem adds a new line item to an existing subscription
func (s *subscriptionService) AddSubscriptionLineItem(ctx context.Context, subscriptionID string, req dto.CreateSubscriptionLineItemRequest) (*dto.SubscriptionLineItemResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the subscription
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Validate subscription status
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions can have line items added").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
				"status":          sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Initialize line item params
	params := dto.LineItemParams{
		Subscription: sub,
	}

	// Get entity details and price with expanded data
	priceService := NewPriceService(s.ServiceParams)
	price, err := priceService.GetPrice(ctx, req.PriceID)
	if err != nil {
		return nil, err
	}

	// Set the price in params
	params.Price = price

	if price.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
		planService := NewPlanService(s.ServiceParams)
		planResponse, err := planService.GetPlan(ctx, price.EntityID)
		if err != nil {
			return nil, err
		}
		params.Plan = planResponse
		params.EntityType = types.SubscriptionLineItemEntityTypePlan
	} else if price.EntityType == types.PRICE_ENTITY_TYPE_ADDON {
		addonService := NewAddonService(s.ServiceParams)
		addonResponse, err := addonService.GetAddon(ctx, price.EntityID)
		if err != nil {
			return nil, err
		}
		params.Addon = addonResponse
		params.EntityType = types.SubscriptionLineItemEntityTypeAddon
	} else {
		return nil, ierr.NewError("unsupported entity type").
			WithHint("Unsupported entity type").
			WithReportableDetails(map[string]interface{}{
				"entity_type": price.EntityType,
			}).
			Mark(ierr.ErrValidation)
	}

	// Create the line item
	lineItem := req.ToSubscriptionLineItem(ctx, params)

	if err := s.SubscriptionLineItemRepo.Create(ctx, lineItem); err != nil {
		return nil, err
	}

	return &dto.SubscriptionLineItemResponse{SubscriptionLineItem: lineItem}, nil
}

// DeleteSubscriptionLineItem marks a line item as deleted by setting its end date
func (s *subscriptionService) DeleteSubscriptionLineItem(ctx context.Context, lineItemID string, req dto.DeleteSubscriptionLineItemRequest) (*dto.SubscriptionLineItemResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the line item
	lineItem, err := s.SubscriptionLineItemRepo.Get(ctx, lineItemID)
	if err != nil {
		return nil, err
	}

	// Set end date and update
	lineItem.EndDate = req.EndDate.UTC()
	if lineItem.EndDate.IsZero() {
		lineItem.EndDate = time.Now().UTC()
	}

	if err := s.SubscriptionLineItemRepo.Update(ctx, lineItem); err != nil {
		return nil, err
	}

	return &dto.SubscriptionLineItemResponse{SubscriptionLineItem: lineItem}, nil
}

// UpdateSubscriptionLineItem updates a subscription line item by terminating the existing one and creating a new one
// This method reuses existing service methods for creating and deleting line items
func (s *subscriptionService) UpdateSubscriptionLineItem(ctx context.Context, req dto.UpdateSubscriptionLineItemRequest) (*dto.SubscriptionLineItemResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the existing line item
	existingLineItem, err := s.SubscriptionLineItemRepo.Get(ctx, req.LineItemID)
	if err != nil {
		return nil, err
	}

	// Check if line item is already terminated
	if existingLineItem.Status == types.StatusDeleted {
		return nil, ierr.NewError("line item is already terminated").
			WithHint("Cannot update a terminated line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": req.LineItemID,
				"status":       existingLineItem.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get the subscription
	sub, err := s.SubRepo.Get(ctx, existingLineItem.SubscriptionID)
	if err != nil {
		return nil, err
	}

	// Additional security check: ensure line item belongs to the subscription
	if existingLineItem.SubscriptionID != sub.ID {
		return nil, ierr.NewError("line item does not belong to the subscription").
			WithHint("Line item and subscription ID mismatch").
			WithReportableDetails(map[string]interface{}{
				"line_item_subscription_id": existingLineItem.SubscriptionID,
				"subscription_id":           sub.ID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate subscription status
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions can have line items updated").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
				"status":          sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Use the existing line item's price
	targetPriceID := existingLineItem.PriceID

	// Determine end date for existing line item
	endDate := time.Now().UTC()
	if req.EndDate != nil {
		endDate = req.EndDate.UTC()

		// Validate end date is not before line item start date
		if endDate.Before(existingLineItem.StartDate) {
			return nil, ierr.NewError("end_date cannot be before line item start date").
				WithHint("End date must be on or after the line item start date").
				WithReportableDetails(map[string]interface{}{
					"line_item_start_date": existingLineItem.StartDate,
					"provided_end_date":    endDate,
				}).
				Mark(ierr.ErrValidation)
		}

	}

	// Validate that we have meaningful overrides (not just empty values)
	hasValidOverrides := false
	if req.Amount != nil && !req.Amount.IsZero() {
		hasValidOverrides = true
	}
	if req.BillingModel != "" {
		hasValidOverrides = true
	}
	if req.TierMode != "" {
		hasValidOverrides = true
	}
	if len(req.Tiers) > 0 {
		hasValidOverrides = true
	}
	if req.TransformQuantity != nil {
		hasValidOverrides = true
	}

	// Create subscription-scoped price if overrides are provided
	var newPriceID string
	if hasValidOverrides {

		if !existingLineItem.EndDate.IsZero() {
			return nil, ierr.NewError("line item is already terminated").
				WithHint("Terminated line items cannot be updated").
				WithReportableDetails(map[string]interface{}{
					"line_item_id": req.LineItemID,
					"end_date":     existingLineItem.EndDate,
				}).
				Mark(ierr.ErrValidation)
		}

		// Convert request to OverrideLineItemRequest format to reuse existing logic
		overrideReq := dto.OverrideLineItemRequest{
			PriceID:           targetPriceID,
			Quantity:          &existingLineItem.Quantity,
			BillingModel:      req.BillingModel,
			Amount:            req.Amount,
			TierMode:          req.TierMode,
			Tiers:             req.Tiers,
			TransformQuantity: req.TransformQuantity,
		}

		// Get price map for validation (reuse existing logic)
		priceService := NewPriceService(s.ServiceParams)
		price, err := priceService.GetPrice(ctx, targetPriceID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to retrieve price for line item update").
				WithReportableDetails(map[string]interface{}{
					"price_id": targetPriceID,
				}).
				Mark(ierr.ErrDatabase)
		}

		// Validate price is still active and compatible
		if price.Status != types.StatusPublished {
			return nil, ierr.NewError("price is not active").
				WithHint("Cannot update line item with inactive price").
				WithReportableDetails(map[string]interface{}{
					"price_id": targetPriceID,
					"status":   price.Status,
				}).
				Mark(ierr.ErrValidation)
		}

		// Validate price compatibility with subscription
		if !price.IsEligibleForSubscription(sub.Currency, sub.BillingPeriod, sub.BillingPeriodCount) {
			return nil, ierr.NewError("price is not compatible with subscription").
				WithHint("Price currency and billing period must match subscription").
				WithReportableDetails(map[string]interface{}{
					"subscription_id":             sub.ID,
					"price_id":                    targetPriceID,
					"subscription_currency":       sub.Currency,
					"subscription_billing_period": sub.BillingPeriod,
					"price_currency":              price.Currency,
					"price_billing_period":        price.BillingPeriod,
				}).
				Mark(ierr.ErrValidation)
		}

		priceMap := map[string]*dto.PriceResponse{targetPriceID: price}
		lineItemsByPriceID := map[string]*subscription.SubscriptionLineItem{targetPriceID: existingLineItem}

		// Validate the override request
		if err := overrideReq.Validate(priceMap, lineItemsByPriceID, sub.PlanID); err != nil {
			return nil, err
		}

		// Process the price override using existing method
		lineItems := []*subscription.SubscriptionLineItem{existingLineItem}
		err = s.ProcessSubscriptionPriceOverrides(ctx, sub, []dto.OverrideLineItemRequest{overrideReq}, lineItems, priceMap)
		if err != nil {
			return nil, err
		}

		// The ProcessSubscriptionPriceOverrides method updates the line item's PriceID
		newPriceID = existingLineItem.PriceID
	} else {
		newPriceID = targetPriceID
	}

	// Execute the update within a transaction
	var newLineItem *subscription.SubscriptionLineItem
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Terminate the existing line item using existing method
		deleteReq := dto.DeleteSubscriptionLineItemRequest{
			EndDate: &endDate,
		}
		_, err := s.DeleteSubscriptionLineItem(ctx, req.LineItemID, deleteReq)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to terminate existing line item").
				WithReportableDetails(map[string]interface{}{
					"line_item_id": req.LineItemID,
				}).
				Mark(ierr.ErrDatabase)
		}

		// Create new line item using the DTO method
		newLineItem = req.ToSubscriptionLineItem(ctx, existingLineItem, newPriceID)
		newLineItem.StartDate = endDate // Start where the old one ends

		// Validate the new line item before creating
		if newLineItem.PriceID == "" {
			return ierr.NewError("new line item must have a valid price ID").
				WithHint("Price ID is required for line item creation").
				Mark(ierr.ErrValidation)
		}

		// Create the line item directly using repository
		if err := s.SubscriptionLineItemRepo.Create(ctx, newLineItem); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create new line item").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": sub.ID,
					"price_id":        newPriceID,
				}).
				Mark(ierr.ErrDatabase)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	s.Logger.Infow("updated subscription line item",
		"subscription_id", sub.ID,
		"old_line_item_id", existingLineItem.ID,
		"new_line_item_id", newLineItem.ID,
		"price_id", newPriceID,
		"end_date", endDate,
		"has_overrides", req.Amount != nil || req.BillingModel != "" || req.TierMode != "" || len(req.Tiers) > 0 || req.TransformQuantity != nil)

	return &dto.SubscriptionLineItemResponse{SubscriptionLineItem: newLineItem}, nil
}
