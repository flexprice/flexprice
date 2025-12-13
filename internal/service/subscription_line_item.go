package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
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
		if req.OverageFactor != nil {
			existingLineItem.OverageFactor = req.OverageFactor
		}
		if req.EnableTrueUp != nil {
			existingLineItem.EnableTrueUp = *req.EnableTrueUp
		}
		if req.IsWindowCommitment != nil {
			existingLineItem.IsWindowCommitment = *req.IsWindowCommitment
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
