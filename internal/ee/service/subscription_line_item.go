package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

	// 4. Resolve price and params (no DB write for inline price; caller creates price inside tx)
	price, params, resolvedReq, usedInlinePrice, inlineCreatePriceReq, err := s.resolvePriceAndLineItemParams(ctx, sub, req)
	if err != nil {
		return nil, err
	}

	// 5–7. Build line item, apply defaults, validate, and persist inside a single transaction
	// so that inline Price create is rolled back if validations or line item create fail
	var lineItem *subscription.SubscriptionLineItem
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {

		if usedInlinePrice && inlineCreatePriceReq != nil {
			createdPrice, createErr := NewPriceService(s.ServiceParams).CreatePrice(txCtx, *inlineCreatePriceReq)
			if createErr != nil {
				return createErr
			}
			price = createdPrice
			params.Price = createdPrice
			resolvedReq.PriceID = createdPrice.ID
		}

		lineItem = resolvedReq.ToSubscriptionLineItem(txCtx, *params)
		if usedInlinePrice {
			s.applySubscriptionScopedLineItemDefaults(lineItem, sub, price)
		}

		// Materialize bucket prices: create a SUBSCRIPTION-scoped Price for each
		// CommitmentTimeBuckets entry and assign the resulting slice (with PriceIDs
		// and stable UUIDs) onto the line item. This must happen inside the
		// transaction so that any created prices are rolled back on failure.
		if len(resolvedReq.CommitmentTimeBuckets) > 0 {
			// ToSubscriptionLineItem already built lineItem.CommitmentTimeBuckets
			// (IDs + commitment fields, empty PriceID); create a SUBSCRIPTION-scoped
			// price per bucket and fill in the PriceIDs.
			if err := s.createBucketPrices(txCtx, lineItem.ID, subscriptionID, resolvedReq.CommitmentTimeBuckets, lineItem.CommitmentTimeBuckets, nil); err != nil {
				return err
			}
			_, _, hasCumulative := getSubscriptionCommitmentPeriodBounds(sub, sub.CurrentPeriodStart)
			if err := s.validateBucketArray(txCtx, lineItem.MeterID, lineItem.CommitmentWindowed, hasCumulative, lineItem.CommitmentTimeBuckets); err != nil {
				return err
			}
		}

		if types.BillingPeriodGreaterThan(sub.BillingPeriod, lineItem.BillingPeriod) {
			return ierr.NewError("line item billing period cannot be shorter than subscription billing period").
				WithHint("The line item's billing period must be equal to or longer than the subscription").
				WithReportableDetails(map[string]interface{}{
					"subscription_id":             sub.ID,
					"subscription_billing_period": sub.BillingPeriod,
					"line_item_id":                lineItem.ID,
					"line_item_billing_period":    lineItem.BillingPeriod,
				}).
				Mark(ierr.ErrValidation)
		}

		if err := s.validateLineItemCommitment(txCtx, lineItem); err != nil {
			return err
		}

		sub.LineItems = append(sub.LineItems, lineItem)
		if err := s.validateSubscriptionLevelCommitment(sub); err != nil {
			return err
		}
		return s.SubscriptionLineItemRepo.Create(txCtx, lineItem)
	})
	if err != nil {
		return nil, err
	}

	// Apply proration for the add if requested. Skip usage prices (unknown future consumption).
	if req.ProrationBehavior == types.ProrationBehaviorCreateProrations &&
		lineItem.PriceType != types.PRICE_TYPE_USAGE {

		effectiveDate := time.Now().UTC()
		if req.StartDate != nil {
			effectiveDate = req.StartDate.UTC()
		}

		// Find the billing period that contains effectiveDate so proration uses the right boundaries.
		period, err := types.FindPeriodForDate(
			effectiveDate,
			sub.CurrentPeriodStart,
			sub.CurrentPeriodEnd,
			sub.BillingAnchor,
			sub.BillingPeriodCount,
			sub.BillingPeriod,
			sub.CustomerTimezone,
		)
		if err != nil {
			return nil, err
		}

		priceSvc := NewPriceService(s.ServiceParams)
		priceResp, err := priceSvc.GetPrice(ctx, lineItem.PriceID)
		if err != nil {
			return nil, err
		}

		// Temporarily override current period on a copy so LineItemProrationService
		// uses the period that actually contains effectiveDate.
		subCopy := *sub
		subCopy.CurrentPeriodStart = period.Start
		subCopy.CurrentPeriodEnd = period.End

		prorationReq := LineItemProrationRequest{
			Subscription:   &subCopy,
			EffectiveDate:  effectiveDate,
			Behavior:       req.ProrationBehavior,
			IdempotencyKey: types.GenerateUUIDWithPrefix("proration_add"),
			Entries: []LineItemProrationEntry{
				{
					LineItem:    lineItem,
					Price:       priceResp.Price,
					Action:      types.ProrationActionAddItem,
					NewQuantity: lineItem.Quantity,
				},
			},
		}
		if applyErr := NewLineItemProrationService(s.ServiceParams).Apply(ctx, prorationReq); applyErr != nil {
			s.Logger.Info(ctx, "proration apply failed for line item add",
				"line_item_id", lineItem.ID, "error", applyErr)
		}
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
// For inline price (req.Price != nil), it does NOT persist the price; it returns inlineCreatePriceReq so the
// caller can create the price inside a transaction. For existing price, it fetches and validates only (no write).
func (s *subscriptionService) resolvePriceAndLineItemParams(ctx context.Context, sub *subscription.Subscription, req dto.CreateSubscriptionLineItemRequest) (price *dto.PriceResponse, params *dto.LineItemParams, resolvedReq dto.CreateSubscriptionLineItemRequest, usedInlinePrice bool, inlineCreatePriceReq *dto.CreatePriceRequest, err error) {
	priceService := NewPriceService(s.ServiceParams)
	subResp := &dto.SubscriptionResponse{Subscription: sub}

	if req.Price != nil {
		// Inline price: validate and prepare create request; caller persists price inside transaction
		createPriceReq := req.Price.ToCreatePriceRequest(sub)
		if err := createPriceReq.Validate(); err != nil {
			return nil, nil, dto.CreateSubscriptionLineItemRequest{}, false, nil, err
		}
		params = &dto.LineItemParams{
			Subscription: subResp,
			Price:        nil, // set after CreatePrice inside tx
			EntityType:   types.SubscriptionLineItemEntityTypeSubscription,
		}
		resolvedReq = dto.CreateSubscriptionLineItemRequest{
			PriceID:                 "", // set to createdPrice.ID inside tx
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
			CommitmentTimeBuckets:   req.CommitmentTimeBuckets,
		}
		return nil, params, resolvedReq, true, &createPriceReq, nil
	}

	// Existing price: fetch and validate, then resolve entity params
	existingPrice, getErr := priceService.GetPrice(ctx, req.PriceID)
	if getErr != nil {
		return nil, nil, dto.CreateSubscriptionLineItemRequest{}, false, nil, getErr
	}
	if err := req.Validate(existingPrice.Price, sub); err != nil {
		return nil, nil, dto.CreateSubscriptionLineItemRequest{}, false, nil, err
	}
	params, resolveErr := s.buildLineItemParamsForPrice(ctx, existingPrice, req.SkipEntitlementCheck)
	if resolveErr != nil {
		return nil, nil, dto.CreateSubscriptionLineItemRequest{}, false, nil, resolveErr
	}
	if params.Subscription == nil {
		params.Subscription = subResp
	}
	return existingPrice, params, req, false, nil, nil
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

	// Capture a snapshot before mutating EndDate — the proration service uses EndDate==zero
	// to distinguish "active recurring" from "onetime" (pre-existing EndDate at period boundary).
	lineItemForProration := *lineItem

	lineItem.EndDate = effectiveFrom

	if err := s.SubscriptionLineItemRepo.Update(ctx, lineItem); err != nil {
		return nil, err
	}

	// Apply proration for the removal if requested. Skip usage prices.
	// Use lineItemForProration (EndDate still zero) so Compute doesn't treat this as onetime.
	if req.ProrationBehavior == types.ProrationBehaviorCreateProrations &&
		lineItemForProration.PriceType != types.PRICE_TYPE_USAGE {

		sub, err := s.SubRepo.Get(ctx, lineItem.SubscriptionID)
		if err != nil {
			s.Logger.Info(ctx, "could not load subscription for delete proration",
				"line_item_id", lineItemID, "error", err)
		} else {
			period, err := types.FindPeriodForDate(
				effectiveFrom,
				sub.CurrentPeriodStart,
				sub.CurrentPeriodEnd,
				sub.BillingAnchor,
				sub.BillingPeriodCount,
				sub.BillingPeriod,
				sub.CustomerTimezone,
			)
			if err != nil {
				s.Logger.Info(ctx, "could not find period for delete proration",
					"line_item_id", lineItemID, "error", err)
			} else {
				priceSvc := NewPriceService(s.ServiceParams)
				priceResp, err := priceSvc.GetPrice(ctx, lineItem.PriceID)
				if err != nil {
					s.Logger.Info(ctx, "could not load price for delete proration",
						"line_item_id", lineItemID, "error", err)
				} else {
					subCopy := *sub
					subCopy.CurrentPeriodStart = period.Start
					subCopy.CurrentPeriodEnd = period.End

					prorationReq := LineItemProrationRequest{
						Subscription:   &subCopy,
						EffectiveDate:  effectiveFrom,
						Behavior:       req.ProrationBehavior,
						IdempotencyKey: types.GenerateUUIDWithPrefix("proration_del"),
						Entries: []LineItemProrationEntry{
							{
								LineItem: &lineItemForProration,
								Price:    priceResp.Price,
								Action:   types.ProrationActionRemoveItem,
							},
						},
					}
					if applyErr := NewLineItemProrationService(s.ServiceParams).Apply(ctx, prorationReq); applyErr != nil {
						s.Logger.Info(ctx, "proration apply failed for line item delete",
							"line_item_id", lineItemID, "error", applyErr)
					}
				}
			}
		}
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

			// Materialize bucket prices when the update request carries
			// commitment_time_buckets: ToSubscriptionLineItem rebuilt
			// newLineItem.CommitmentTimeBuckets from the request (fresh IDs, empty
			// PriceIDs). createBucketPrices reuses an existing bucket's price +
			// ID when that bucket is unchanged, and only creates new prices for
			// changed/added buckets; old prices for removed/changed buckets are left
			// intact (still referenced by historical invoices).
			//
			// Omitting commitment_time_buckets (nil pointer) inherits the existing
			// line item's buckets (copied by ToSubscriptionLineItem); supplying an
			// explicit empty slice clears them.
			if req.CommitmentTimeBuckets != nil {
				if err := s.createBucketPrices(ctx, newLineItem.ID, existingLineItem.SubscriptionID, *req.CommitmentTimeBuckets, newLineItem.CommitmentTimeBuckets, existingLineItem.CommitmentTimeBuckets); err != nil {
					return err
				}
				_, _, hasCumulative := getSubscriptionCommitmentPeriodBounds(sub, sub.CurrentPeriodStart)
				if err := s.validateBucketArray(ctx, newLineItem.MeterID, newLineItem.CommitmentWindowed, hasCumulative, newLineItem.CommitmentTimeBuckets); err != nil {
					return err
				}
			}

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

		s.Logger.Info(ctx, "updated subscription line item with price overrides",
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

		s.Logger.Info(ctx, "updated subscription line item",
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

	// No top-level commitment and no per-bucket commitments — nothing to validate.
	if !lineItem.HasAnyCommitment() {
		return nil
	}

	// Rules shared by top-level and per-bucket commitments ------------------

	// Price must be PRICE_TYPE_USAGE
	if lineItem.PriceType != types.PRICE_TYPE_USAGE {
		return ierr.NewError("commitment is only allowed for usage-based pricing").
			WithHint("Line item must have price_type='usage' to use commitment pricing").
			WithReportableDetails(map[string]interface{}{
				"price_type": lineItem.PriceType,
			}).
			Mark(ierr.ErrValidation)
	}

	// Window commitment requires a meter with a bucket_size.
	if lineItem.CommitmentWindowed {
		if lineItem.MeterID == "" {
			return ierr.NewError("meter is required for window-based commitment").
				WithHint("Window commitment requires a meter with bucket_size configured").
				Mark(ierr.ErrValidation)
		}
		m, err := s.MeterRepo.GetMeter(ctx, lineItem.MeterID)
		if err != nil {
			return err
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

	// Rules for the top-level commitment fields only -------------------------
	// (Per-bucket commitment fields are validated by TimeOfDayBucket.Validate.)
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

	// Rule 3: Validate commitment type matches what was set
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
// and validates the resulting commitment configuration. When the config carries
// per-bucket commitments, a SUBSCRIPTION-scoped price is materialized per bucket.
//
// Returns the matched config (nil when the line item has none) so callers can
// key it by line item ID for createBucketPricesForLineItems — the lookup here
// uses the line item's CURRENT PriceID, which price overrides may mutate later.
func (s *subscriptionService) applyLineItemCommitmentFromMap(
	ctx context.Context,
	sub *subscription.Subscription,
	lineItem *subscription.SubscriptionLineItem,
	commitments map[string]*dto.LineItemCommitmentConfig,
) (*dto.LineItemCommitmentConfig, error) {
	if lineItem == nil || len(commitments) == 0 {
		return nil, nil
	}

	cfg, ok := commitments[lineItem.PriceID]
	if !ok || cfg == nil {
		return nil, nil
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
	if len(cfg.CommitmentTimeBuckets) > 0 {
		// Build domain buckets (IDs + commitment fields) only. Bucket PRICES are
		// deliberately NOT created here — this runs while line items are being
		// assembled, before the persisting transaction starts. The caller must
		// invoke createBucketPricesForLineItems inside that transaction so price
		// rows roll back together with the line items.
		lineItem.CommitmentTimeBuckets = cfg.ToDomainBuckets()
	}
	if err := s.validateLineItemCommitment(ctx, lineItem); err != nil {
		return nil, err
	}

	return cfg, nil
}

// createBucketPricesForLineItems creates the bucket price rows for any line item
// that carries commitment time buckets (built earlier by
// applyLineItemCommitmentFromMap) and runs array-level bucket validation.
//
// cfgs is keyed by LINE ITEM ID (collected from applyLineItemCommitmentFromMap's
// return), not by price id: price overrides mutate a line item's PriceID after
// commitments are applied, so a price-id lookup would miss overridden items and
// leave their buckets unpriced.
//
// MUST be called inside the transaction that persists the line items.
func (s *subscriptionService) createBucketPricesForLineItems(
	ctx context.Context,
	sub *subscription.Subscription,
	lineItems []*subscription.SubscriptionLineItem,
	cfgs map[string]*dto.LineItemCommitmentConfig,
) error {
	if len(cfgs) == 0 {
		return nil
	}
	_, _, hasCumulative := getSubscriptionCommitmentPeriodBounds(sub, sub.CurrentPeriodStart)
	for _, li := range lineItems {
		if li == nil || len(li.CommitmentTimeBuckets) == 0 {
			continue
		}
		cfg := cfgs[li.ID]
		if cfg == nil || len(cfg.CommitmentTimeBuckets) == 0 {
			continue
		}
		if err := s.createBucketPrices(ctx, li.ID, sub.ID, cfg.CommitmentTimeBuckets, li.CommitmentTimeBuckets, nil); err != nil {
			return err
		}
		if err := s.validateBucketArray(ctx, li.MeterID, li.CommitmentWindowed, hasCumulative, li.CommitmentTimeBuckets); err != nil {
			return err
		}
	}
	return nil
}

// ListSubscriptionLineItems returns subscription line items matching the filter with pagination and optional price expansion.
func (s *subscriptionService) ListSubscriptionLineItems(ctx context.Context, filter *types.SubscriptionLineItemFilter) (*dto.ListSubscriptionLineItemsResponse, error) {
	if filter == nil {
		filter = types.NewSubscriptionLineItemFilter()
	}
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}
	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	expand := filter.GetExpand()
	if !expand.IsEmpty() {
		if err := expand.Validate(types.SubscriptionLineItemListExpandConfig); err != nil {
			return nil, err
		}
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	items, err := s.SubscriptionLineItemRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.SubscriptionLineItemRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	shouldExpandPrices := expand.Has(types.ExpandPrices) ||
		expand.GetNested(types.ExpandSubscriptionLineItems).Has(types.ExpandPrices)

	responses := make([]*dto.SubscriptionLineItemResponse, len(items))
	if shouldExpandPrices && len(items) > 0 {
		priceIDs := lo.Uniq(lo.Map(items, func(item *subscription.SubscriptionLineItem, _ int) string {
			return item.PriceID
		}))
		priceService := NewPriceService(s.ServiceParams)
		priceFilter := types.NewNoLimitPriceFilter().
			WithPriceIDs(priceIDs).
			WithAllowExpiredPrices(true)

		var priceExpand types.Expand
		if expand.Has(types.ExpandPrices) {
			priceExpand = expand.GetNested(types.ExpandPrices)
		} else if expand.GetNested(types.ExpandSubscriptionLineItems).Has(types.ExpandPrices) {
			priceExpand = expand.GetNested(types.ExpandSubscriptionLineItems).GetNested(types.ExpandPrices)
		}
		if !priceExpand.IsEmpty() {
			priceFilter = priceFilter.WithExpand(priceExpand.String())
		}

		prices, err := priceService.GetPrices(ctx, priceFilter)
		if err != nil {
			return nil, err
		}
		priceMap := make(map[string]*dto.PriceResponse, len(prices.Items))
		for _, p := range prices.Items {
			priceMap[p.ID] = p
		}
		for i, lineItem := range items {
			responses[i] = &dto.SubscriptionLineItemResponse{
				SubscriptionLineItem: lineItem,
				Price:                priceMap[lineItem.PriceID],
			}
		}
	} else {
		for i, lineItem := range items {
			responses[i] = &dto.SubscriptionLineItemResponse{SubscriptionLineItem: lineItem}
		}
	}

	return &dto.ListSubscriptionLineItemsResponse{
		Items: responses,
		Pagination: types.NewPaginationResponse(
			count,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}, nil
}

// validateMultiCadence enforces mutual exclusion between multi-cadence and proration.
// Line items are allowed to have any mix of billing periods; alignment is not required.
func (s *subscriptionService) validateMultiCadence(sub *subscription.Subscription) error {
	if len(sub.LineItems) == 0 {
		return nil
	}

	if sub.HasMixedBillingPeriods() && sub.ProrationBehavior == types.ProrationBehaviorCreateProrations {
		return ierr.NewError("proration is not supported for subscriptions with mixed billing periods").
			WithHint("Set proration_behavior to 'none' when using different billing periods on the same subscription").
			WithReportableDetails(map[string]interface{}{
				"proration_behavior": sub.ProrationBehavior,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// validateBucketArray runs array-level validation on a bucket slice — every
// bucket priced, overlap detection, meter-window alignment, and
// cumulative-commitment guard. Called from create and update flows AFTER
// createBucketPrices, so every bucket is expected to carry its PriceID.
//
// When meterID is empty (e.g. line items not tied to a meter), the alignment
// check is skipped — overlap still runs.
//
// hasCumulativeCommitment is computed by the caller (which already holds the
// subscription) and, when true, rejects per-bucket commitments because the two
// models are incompatible. Keeping the lookup out of this method leaves it
// focused on array-level validation only.
//
// commitmentWindowed is the line item's *effective* windowed flag (after merging
// request + inherited values on update). Buckets only bill via the windowed path,
// so they require commitment_windowed=true; this guard closes the gap left open
// by the DTO layer, which can only cross-check both fields when supplied together.
func (s *subscriptionService) validateBucketArray(
	ctx context.Context,
	meterID string,
	commitmentWindowed bool,
	hasCumulativeCommitment bool,
	buckets types.TimeOfDayBuckets,
) error {
	if len(buckets) == 0 {
		return nil
	}
	if !commitmentWindowed {
		return ierr.NewError("commitment_time_buckets requires commitment_windowed=true").
			WithHint("Set commitment_windowed=true to apply commitment only during the configured hours").
			Mark(ierr.ErrValidation)
	}
	if hasCumulativeCommitment {
		return ierr.NewError("per-bucket commitment cannot be combined with cumulative subscription commitment").
			WithHint("Pick one model: per-bucket OR cumulative.").
			Mark(ierr.ErrValidation)
	}
	// Billing depends on every bucket carrying its own price; this runs after
	// createBucketPrices, so an empty PriceID here is a wiring bug.
	for i, b := range buckets {
		if b.PriceID == "" {
			return ierr.NewError("bucket is missing its price").
				WithHint("Every commitment time bucket must have a price").
				WithReportableDetails(map[string]interface{}{"bucket_index": i, "bucket_id": b.ID}).
				Mark(ierr.ErrSystem)
		}
	}
	if err := buckets.ValidateNoOverlap(); err != nil {
		return err
	}
	if meterID != "" {
		m, err := s.MeterRepo.GetMeter(ctx, meterID)
		if err != nil {
			return err
		}
		if err := buckets.ValidateWindowAlignment(m.Aggregation.BucketSize.ToMinutes()); err != nil {
			return err
		}
	}
	return nil
}

// createBucketPrices creates (inserts) a SUBSCRIPTION-scoped price row for each
// new bucket request and writes the resulting PriceID onto the matching domain
// bucket. reqs and buckets are positionally aligned (length must match): the DTO
// layer builds buckets (ID + commitment fields, empty PriceID) and the service
// fills in the price here.
//
// A request that carries an id refers to an existing bucket (update flows): its
// price is reused from the matching bucket in existing instead of creating a new
// row. Commitment fields still come from the request, so a bucket's commitment
// can change while its price is kept. An id that matches no existing bucket is
// rejected.
//
// MUST be called inside the same transaction that persists the line item, so
// created price rows roll back if the line item write fails.
func (s *subscriptionService) createBucketPrices(
	ctx context.Context,
	lineItemID string,
	subscriptionID string,
	reqs []dto.CommitmentBucketRequest,
	buckets types.TimeOfDayBuckets,
	existing types.TimeOfDayBuckets,
) error {
	if len(reqs) != len(buckets) {
		return ierr.NewError("bucket request/domain length mismatch").
			WithHint("Each commitment bucket request must map to exactly one domain bucket").
			WithReportableDetails(map[string]interface{}{
				"requests": len(reqs),
				"buckets":  len(buckets),
			}).
			Mark(ierr.ErrSystem)
	}

	existingByID := make(map[string]types.TimeOfDayBucket, len(existing))
	for _, ex := range existing {
		existingByID[ex.ID] = ex
	}

	priceService := NewPriceService(s.ServiceParams)
	for i := range reqs {
		// Reuse: request references an existing bucket by id — keep its price.
		if reqs[i].ID != "" {
			ex, ok := existingByID[reqs[i].ID]
			if !ok {
				return ierr.NewError("unknown bucket id").
					WithHint("Bucket id must reference an existing bucket on this line item; omit id (and provide price) to create a new bucket").
					WithReportableDetails(map[string]interface{}{"bucket_id": reqs[i].ID, "bucket_index": i}).
					Mark(ierr.ErrValidation)
			}
			buckets[i].PriceID = ex.PriceID
			continue
		}

		priceReq := *reqs[i].Price
		priceReq.EntityType = types.PRICE_ENTITY_TYPE_SUBSCRIPTION
		priceReq.EntityID = subscriptionID
		priceReq.SkipEntityValidation = true
		priceReq.Metadata = map[string]string{
			"sub_line_item_id": lineItemID,
			"bucket_id":        buckets[i].ID,
		}

		resp, err := priceService.CreatePrice(ctx, priceReq)
		if err != nil {
			return err
		}
		buckets[i].PriceID = resp.ID
	}
	return nil
}

// validateSubscriptionLevelCommitment validates that subscription and line items don't both have commitment
func (s *subscriptionService) validateSubscriptionLevelCommitment(sub *subscription.Subscription) error {
	if !sub.HasCommitment() {
		return nil
	}

	// Check if any line item has a commitment (top-level or per-bucket)
	for _, lineItem := range sub.LineItems {
		if lineItem.HasAnyCommitment() {
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
