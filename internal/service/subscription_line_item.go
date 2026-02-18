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
	// 1. Load subscription
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// 2. Validate request (including date bounds when sub is passed)
	if err := req.Validate(nil, sub); err != nil {
		return nil, err
	}

	// 3. Validate subscription status
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions can have line items added").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
				"status":          sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// 4. Resolve price and params
	price, params, resolvedReq, usedInlinePrice, err := s.resolvePriceAndLineItemParams(ctx, sub, req)
	if err != nil {
		return nil, err
	}

	// 5. Build line item
	lineItem := resolvedReq.ToSubscriptionLineItem(ctx, *params)
	if usedInlinePrice {
		s.applySubscriptionScopedLineItemDefaults(lineItem, sub, price)
	}

	// 6. Validate line item
	if err := s.validateLineItemCommitment(ctx, lineItem); err != nil {
		return nil, err
	}
	// Reject if subscription has commitment and the new line item also has commitment
	// (the new line item is not in sub.LineItems yet, so validateSubscriptionLevelCommitment does not see it)
	if lineItem.HasCommitment() && sub.HasCommitment() {
		return nil, ierr.NewError("cannot set commitment on both subscription and line item").
			WithHint("Use either subscription-level commitment or line-item-level commitment, not both").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":               subscriptionID,
				"subscription_commitment":       sub.CommitmentAmount,
				"line_item_commitment_amount":   lineItem.CommitmentAmount,
				"line_item_commitment_quantity": lineItem.CommitmentQuantity,
			}).
			Mark(ierr.ErrValidation)
	}
	if err := s.validateSubscriptionLevelCommitment(sub); err != nil {
		return nil, err
	}

	// 7. Persist and return
	if err := s.SubscriptionLineItemRepo.Create(ctx, lineItem); err != nil {
		return nil, err
	}
	return &dto.SubscriptionLineItemResponse{SubscriptionLineItem: lineItem}, nil
}

// buildLineItemParamsForPrice builds LineItemParams for a price, resolving Plan/Addon/Subscription when skipEntitlementCheck is true.
func (s *subscriptionService) buildLineItemParamsForPrice(ctx context.Context, price *dto.PriceResponse, skipEntitlementCheck bool) (*dto.LineItemParams, error) {
	params := &dto.LineItemParams{Price: price}
	if !skipEntitlementCheck {
		return params, nil
	}
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
		subService := NewSubscriptionService(s.ServiceParams)
		subResponse, err := subService.GetSubscription(ctx, price.EntityID)
		if err != nil {
			return nil, err
		}
		params.Subscription = subResponse
		params.EntityType = types.SubscriptionLineItemEntityTypeSubscription
	default:
		return nil, ierr.NewError("unsupported entity type").
			WithHint("Unsupported entity type").
			WithReportableDetails(map[string]interface{}{
				"entity_type": price.EntityType,
			}).
			Mark(ierr.ErrValidation)
	}
	return params, nil
}

// resolvePriceAndLineItemParams resolves the price and params for a new line item (inline or existing price).
func (s *subscriptionService) resolvePriceAndLineItemParams(ctx context.Context, sub *subscription.Subscription, req dto.CreateSubscriptionLineItemRequest) (price *dto.PriceResponse, params *dto.LineItemParams, resolvedReq dto.CreateSubscriptionLineItemRequest, usedInlinePrice bool, err error) {
	priceService := NewPriceService(s.ServiceParams)
	subResp := &dto.SubscriptionResponse{Subscription: sub}

	if req.Price != nil {
		// Inline price: create subscription-scoped price, then build params and resolved request
		createPriceReq := req.Price.ToCreatePriceRequest(sub)
		if err := createPriceReq.Validate(); err != nil {
			return nil, nil, dto.CreateSubscriptionLineItemRequest{}, false, err
		}
		createdPrice, createErr := priceService.CreatePrice(ctx, createPriceReq)
		if createErr != nil {
			return nil, nil, dto.CreateSubscriptionLineItemRequest{}, false, createErr
		}
		params = &dto.LineItemParams{
			Subscription: subResp,
			Price:        createdPrice,
			EntityType:   types.SubscriptionLineItemEntityTypeSubscription,
		}
		resolvedReq = dto.CreateSubscriptionLineItemRequest{
			PriceID:                 createdPrice.ID,
			Quantity:                req.Quantity,
			StartDate:               req.StartDate,
			EndDate:                 req.EndDate,
			Metadata:                req.Metadata,
			DisplayName:             req.DisplayName,
			SubscriptionPhaseID:     req.SubscriptionPhaseID,
			SkipEntitlementCheck:    true,
			CommitmentAmount:        req.CommitmentAmount,
			CommitmentQuantity:      req.CommitmentQuantity,
			CommitmentType:          req.CommitmentType,
			CommitmentOverageFactor: req.CommitmentOverageFactor,
			CommitmentTrueUpEnabled: req.CommitmentTrueUpEnabled,
			CommitmentWindowed:      req.CommitmentWindowed,
			CommitmentDuration:      req.CommitmentDuration,
		}
		return createdPrice, params, resolvedReq, true, nil
	}

	// Existing price: fetch and validate, then resolve entity params
	existingPrice, getErr := priceService.GetPrice(ctx, req.PriceID)
	if getErr != nil {
		return nil, nil, dto.CreateSubscriptionLineItemRequest{}, false, getErr
	}
	if err := req.Validate(existingPrice.Price, sub); err != nil {
		return nil, nil, dto.CreateSubscriptionLineItemRequest{}, false, err
	}
	params, resolveErr := s.buildLineItemParamsForPrice(ctx, existingPrice, req.SkipEntitlementCheck)
	if resolveErr != nil {
		return nil, nil, dto.CreateSubscriptionLineItemRequest{}, false, resolveErr
	}
	if params.Subscription == nil {
		params.Subscription = subResp
	}
	return existingPrice, params, req, false, nil
}

// applySubscriptionScopedLineItemDefaults sets entity and display name on a line item created from an inline (subscription-scoped) price.
func (s *subscriptionService) applySubscriptionScopedLineItemDefaults(lineItem *subscription.SubscriptionLineItem, sub *subscription.Subscription, price *dto.PriceResponse) {
	lineItem.EntityID = sub.ID
	lineItem.EntityType = types.SubscriptionLineItemEntityTypeSubscription
	if lineItem.PlanDisplayName == "" && price != nil && price.DisplayName != "" {
		lineItem.PlanDisplayName = price.DisplayName
	}
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

	// Validate effective from date is on or after start date
	if effectiveFrom.Before(lineItem.StartDate) {
		return nil, ierr.NewError("effective from date must be on or after start date").
			WithHint("The effective from date must be on or after the line item's start date").
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

	// Effective date must not be before the line item's start date (avoids end_date < start_date)
	if !existingLineItem.StartDate.IsZero() && endDate.Before(existingLineItem.StartDate) {
		return nil, ierr.NewError("effective date must be on or after line item start date").
			WithHint("The effective date for terminating this line item cannot be before the line item's start date").
			WithReportableDetails(map[string]interface{}{
				"line_item_id":   lineItemID,
				"start_date":     existingLineItem.StartDate,
				"effective_from": endDate,
			}).
			Mark(ierr.ErrValidation)
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

		// Get price for override logic (and ensure endDate >= existing line item start already validated above)
		priceService := NewPriceService(s.ServiceParams)
		price, err := priceService.GetPrice(ctx, existingLineItem.PriceID)
		if err != nil {
			return nil, err
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
			if err := s.validateLineItemCommitment(ctx, newLineItem); err != nil {
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
		if err := s.validateLineItemCommitment(ctx, existingLineItem); err != nil {
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
func (s *subscriptionService) validateLineItemCommitment(ctx context.Context, lineItem *subscription.SubscriptionLineItem) error {
	if lineItem == nil {
		return nil
	}

	// If no commitment is configured, no validation needed
	if !lineItem.HasCommitment() {
		return nil
	}

	// Fetch meter details only when needed for window-based commitment validation.
	var m *meter.Meter
	if lineItem.CommitmentWindowed && lineItem.MeterID != "" {
		var err error
		m, err = s.MeterRepo.GetMeter(ctx, lineItem.MeterID)
		if err != nil {
			return err
		}
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
		if m == nil {
			return ierr.NewError("meter is required for window-based commitment").
				WithHint("Window commitment requires a meter with bucket_size configured").
				Mark(ierr.ErrValidation)
		}

		if !m.HasBucketSize() {
			return ierr.NewError("window commitment requires meter with bucket_size").
				WithHint("Configure bucket_size on the meter to use window-based commitment").
				WithReportableDetails(map[string]interface{}{
					"meter_id":         m.ID,
					"aggregation_type": m.Aggregation.Type,
					"bucket_size":      m.Aggregation.BucketSize,
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

// applyLineItemCommitmentFromMap applies commitment config (keyed by price_id) onto a line item
// and validates the resulting commitment configuration.
func (s *subscriptionService) applyLineItemCommitmentFromMap(
	ctx context.Context,
	lineItem *subscription.SubscriptionLineItem,
	commitments map[string]*dto.LineItemCommitmentConfig,
) error {
	if lineItem == nil || len(commitments) == 0 {
		return nil
	}

	cfg, ok := commitments[lineItem.PriceID]
	if !ok || cfg == nil {
		return nil
	}

	if cfg.CommitmentAmount != nil {
		lineItem.CommitmentAmount = cfg.CommitmentAmount
	}
	if cfg.CommitmentQuantity != nil {
		lineItem.CommitmentQuantity = cfg.CommitmentQuantity
	}
	if cfg.CommitmentType != "" {
		lineItem.CommitmentType = cfg.CommitmentType
	}
	if cfg.OverageFactor != nil {
		lineItem.CommitmentOverageFactor = cfg.OverageFactor
	}
	if cfg.EnableTrueUp != nil {
		lineItem.CommitmentTrueUpEnabled = *cfg.EnableTrueUp
	}
	if cfg.IsWindowCommitment != nil {
		lineItem.CommitmentWindowed = *cfg.IsWindowCommitment
	}
	if cfg.CommitmentDuration != nil {
		lineItem.CommitmentDuration = cfg.CommitmentDuration
	}
	if err := s.validateLineItemCommitment(ctx, lineItem); err != nil {
		return err
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
