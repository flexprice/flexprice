package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// AddSubscriptionLineItem adds a new line item to an existing subscription
func (s *subscriptionService) AddSubscriptionLineItem(ctx context.Context, subscriptionID string, req dto.CreateSubscriptionLineItemRequest) (*dto.SubscriptionLineItemResponse, error) {
	// Get the subscription
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil  {
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
		Subscription: &dto.SubscriptionResponse{Subscription: sub},
	}

	// Get entity details and price with expanded data
	priceService := NewPriceService(s.ServiceParams)
	price, err := priceService.GetPrice(ctx, req.PriceID)
	if err != nil {
		return nil, err
	}

	// Validate with price for MinQuantity checks
	if err := req.Validate(price.Price); err != nil {
		return nil, err
	}

	// Set the price in params
	params.Price = price

	// Skip entitlement check if requested
	if req.SkipEntitlementCheck {
		switch price.EntityType {
		case types.PRICE_ENTITY_TYPE_PLAN:
			planService := NewPlanService(s.ServiceParams)
			planResponse, err := planService.GetPlan(ctx, price.EntityID)
			if err != nil {
				return nil, err
			}
			params.Plan = planResponse
			params.EntityType = types.SubscriptionLineItemEntityTypePlan
		case types.PRICE_ENTITY_TYPE_ADDON:
			addonService := NewAddonService(s.ServiceParams)
			addonResponse, err := addonService.GetAddon(ctx, price.EntityID)
			if err != nil {
				return nil, err
			}
			params.Addon = addonResponse
			params.EntityType = types.SubscriptionLineItemEntityTypeAddon
		case types.PRICE_ENTITY_TYPE_SUBSCRIPTION:
			subscriptionService := NewSubscriptionService(s.ServiceParams)
			subscriptionResponse, err := subscriptionService.GetSubscription(ctx, price.EntityID)
			if err != nil {
				return nil, err
			}
			params.Subscription = subscriptionResponse
			params.EntityType = types.SubscriptionLineItemEntityTypePlan
		default:
			return nil, ierr.NewError("unsupported entity type").
				WithHint("Unsupported entity type").
				WithReportableDetails(map[string]interface{}{
					"entity_type": price.EntityType,
				}).
				Mark(ierr.ErrValidation)
		}

	}

	// Create the line item
	lineItem := req.ToSubscriptionLineItem(ctx, params)

	// Validate line item commitment if configured
	// Get meter if this is a usage-based line item
	var meter *meter.Meter
	if lineItem.PriceType == types.PRICE_TYPE_USAGE && lineItem.MeterID != "" {
		meterFilter := types.NewNoLimitMeterFilter()
		meterFilter.MeterIDs = []string{lineItem.MeterID}
		meters, err := s.MeterRepo.List(ctx, meterFilter)
		if err == nil && len(meters) > 0 {
			meter = meters[0]
		}
	}

	if err := s.validateLineItemCommitment(ctx, lineItem, meter); err != nil {
		return nil, err
	}

	// Validate subscription-level commitment doesn't conflict
	if err := s.validateSubscriptionLevelCommitment(sub); err != nil {
		return nil, err
	}

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

	// Check if line item is already terminated
	if !lineItem.EndDate.IsZero() {
		return nil, ierr.NewError("line item is already terminated").
			WithHint("Cannot terminate a line item that has already been terminated").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": lineItemID,
				"end_date":     lineItem.EndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	// Set end date and update
	var effectiveFrom time.Time
	if req.EffectiveFrom != nil {
		effectiveFrom = req.EffectiveFrom.UTC()
	} else {
		effectiveFrom = time.Now().UTC()
	}

	// Validate effective from date is after start date
	if effectiveFrom.Before(lineItem.StartDate) {
		return nil, ierr.NewError("effective from date must be after start date").
			WithHint("The effective from date must be after the line item's start date").
			WithReportableDetails(map[string]interface{}{
				"line_item_id":   lineItemID,
				"start_date":     lineItem.StartDate,
				"effective_from": effectiveFrom,
			}).
			Mark(ierr.ErrValidation)
	}

	lineItem.EndDate = effectiveFrom

	if err := s.SubscriptionLineItemRepo.Update(ctx, lineItem); err != nil {
		return nil, err
	}

	return &dto.SubscriptionLineItemResponse{SubscriptionLineItem: lineItem}, nil
}

// UpdateSubscriptionLineItem updates a subscription line item by terminating the existing one and creating a new one
// This method reuses existing service methods for creating and deleting line items
func (s *subscriptionService) UpdateSubscriptionLineItem(ctx context.Context, lineItemID string, req dto.UpdateSubscriptionLineItemRequest) (*dto.SubscriptionLineItemResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the existing line item
	existingLineItem, err := s.SubscriptionLineItemRepo.Get(ctx, lineItemID)
	if err != nil {
		return nil, err
	}

	// Check if line item is already terminated
	if existingLineItem.Status != types.StatusPublished {
		return nil, ierr.NewError("line item is not active").
			WithHint("Cannot update an inactive line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": lineItemID,
				"status":       existingLineItem.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get the subscription
	sub, err := s.SubRepo.Get(ctx, existingLineItem.SubscriptionID)
	if err != nil {
		return nil, err
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

	// Determine end date for existing line item
	endDate := time.Now().UTC()
	if req.EffectiveFrom != nil {
		endDate = req.EffectiveFrom.UTC()
	}

	// Check if we need to create a new line item (with price overrides)
	if req.ShouldCreateNewLineItem() {
		// Validate line item is not already terminated
		if !existingLineItem.EndDate.IsZero() {
			return nil, ierr.NewError("line item is already terminated").
				WithHint("Terminated line items cannot be updated").
				WithReportableDetails(map[string]interface{}{
					"line_item_id": lineItemID,
					"end_date":     existingLineItem.EndDate,
				}).
				Mark(ierr.ErrValidation)
		}

		// Convert request to OverrideLineItemRequest format to reuse existing logic
		overrideReq := dto.OverrideLineItemRequest{
			PriceID:           existingLineItem.PriceID,
			Quantity:          &existingLineItem.Quantity,
			BillingModel:      req.BillingModel,
			Amount:            req.Amount,
			TierMode:          req.TierMode,
			Tiers:             req.Tiers,
			TransformQuantity: req.TransformQuantity,
		}

		// Get price map for validation (reuse existing logic)
		priceService := NewPriceService(s.ServiceParams)
		price, err := priceService.GetPrice(ctx, existingLineItem.PriceID)
		if err != nil {
			return nil, err
		}

		priceMap := map[string]*dto.PriceResponse{existingLineItem.PriceID: price}

		// Execute the complex update within a transaction
		var newLineItem *subscription.SubscriptionLineItem
		err = s.DB.WithTx(ctx, func(ctx context.Context) error {
			// Process the price override using existing method
			lineItems := []*subscription.SubscriptionLineItem{existingLineItem}
			err = s.ProcessSubscriptionPriceOverrides(ctx, sub, []dto.OverrideLineItemRequest{overrideReq}, lineItems, priceMap)
			if err != nil {
				return err
			}

			// The ProcessSubscriptionPriceOverrides method updates the line item's PriceID
			newPriceID := existingLineItem.PriceID

			// Terminate the existing line item using existing method
			deleteReq := dto.DeleteSubscriptionLineItemRequest{
				EffectiveFrom: &endDate,
			}
			_, err := s.DeleteSubscriptionLineItem(ctx, lineItemID, deleteReq)
			if err != nil {
				return err
			}

			// Create new line item using the DTO method
			newLineItem = req.ToSubscriptionLineItem(ctx, existingLineItem, newPriceID)
			newLineItem.StartDate = endDate // Start where the old one ends

			// Validate line item commitment if configured
			var meter *meter.Meter
			if newLineItem.PriceType == types.PRICE_TYPE_USAGE && newLineItem.MeterID != "" {
				meterFilter := types.NewNoLimitMeterFilter()
				meterFilter.MeterIDs = []string{newLineItem.MeterID}
				meters, err := s.MeterRepo.List(ctx, meterFilter)
				if err == nil && len(meters) > 0 {
					meter = meters[0]
				}
			}

			if err := s.validateLineItemCommitment(ctx, newLineItem, meter); err != nil {
				return err
			}

			// Validate subscription-level commitment doesn't conflict
			if err := s.validateSubscriptionLevelCommitment(sub); err != nil {
				return err
			}

			// Create the line item directly using repository
			if err := s.SubscriptionLineItemRepo.Create(ctx, newLineItem); err != nil {
				return err
			}
			return nil
		})

		if err != nil {
			return nil, err
		}

		s.Logger.Infow("updated subscription line item with price overrides",
			"subscription_id", sub.ID,
			"old_line_item_id", existingLineItem.ID,
			"new_line_item_id", newLineItem.ID,
			"end_date", endDate,
		)

		return &dto.SubscriptionLineItemResponse{SubscriptionLineItem: newLineItem}, nil
	} else {
		// Update metadata and commitment fields if provided
		if req.Metadata != nil {
			existingLineItem.Metadata = req.Metadata
		}

		// Update commitment fields if provided
		if req.CommitmentAmount != nil {
			existingLineItem.CommitmentAmount = req.CommitmentAmount
		}
		if req.CommitmentQuantity != nil {
			existingLineItem.CommitmentQuantity = req.CommitmentQuantity
		}
		if req.CommitmentType != "" {
			existingLineItem.CommitmentType = req.CommitmentType
		}
		if req.CommitmentOverageFactor != nil {
			existingLineItem.CommitmentOverageFactor = req.CommitmentOverageFactor
		}

		if req.CommitmentTrueUpEnabled != nil {
			existingLineItem.CommitmentTrueUpEnabled = *req.CommitmentTrueUpEnabled
		}

		if req.CommitmentWindowed != nil {
			existingLineItem.CommitmentWindowed = *req.CommitmentWindowed
		}

		// Validate line item commitment if configured
		var meter *meter.Meter
		if existingLineItem.PriceType == types.PRICE_TYPE_USAGE && existingLineItem.MeterID != "" {
			meterFilter := types.NewNoLimitMeterFilter()
			meterFilter.MeterIDs = []string{existingLineItem.MeterID}
			meters, err := s.MeterRepo.List(ctx, meterFilter)
			if err == nil && len(meters) > 0 {
				meter = meters[0]
			}
		}

		if err := s.validateLineItemCommitment(ctx, existingLineItem, meter); err != nil {
			return nil, err
		}

		// Validate subscription-level commitment doesn't conflict
		if err := s.validateSubscriptionLevelCommitment(sub); err != nil {
			return nil, err
		}

		if err := s.SubscriptionLineItemRepo.Update(ctx, existingLineItem); err != nil {
			return nil, err
		}

		s.Logger.Infow("updated subscription line item",
			"subscription_id", sub.ID,
			"line_item_id", existingLineItem.ID)

		return &dto.SubscriptionLineItemResponse{SubscriptionLineItem: existingLineItem}, nil
	}
}

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
	if lineItem.CommitmentOverageFactor == nil {
		return ierr.NewError("commitment_overage_factor is required when commitment is set").
			WithHint("Specify a commitment_overage_factor greater than 1.0").
			Mark(ierr.ErrValidation)
	}

	if lineItem.CommitmentOverageFactor.LessThanOrEqual(decimal.NewFromInt(1)) {
		return ierr.NewError("commitment_overage_factor must be greater than 1.0").
			WithHint("Overage factor determines the multiplier for usage beyond commitment").
			WithReportableDetails(map[string]interface{}{
				"commitment_overage_factor": lineItem.CommitmentOverageFactor,
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

	// Additional validation for window commitment
	// Window commitment is only supported for certain meters or configurations
	// but for now, we just allow it if set
	if lineItem.CommitmentWindowed {
		// Just validate that we're not doing something obviously wrong with windows
		// For example, maybe we need to check if the meter supports it, but ignoring for now
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
