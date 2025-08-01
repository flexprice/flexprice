package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/price"
	subscriptionDomain "github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// AddonService interface defines the business logic for addon management
type AddonService interface {
	// Addon CRUD operations
	CreateAddon(ctx context.Context, req dto.CreateAddonRequest) (*dto.CreateAddonResponse, error)
	GetAddon(ctx context.Context, id string) (*dto.AddonResponse, error)
	GetAddonByLookupKey(ctx context.Context, lookupKey string) (*dto.AddonResponse, error)
	GetAddons(ctx context.Context, filter *types.AddonFilter) (*dto.ListAddonsResponse, error)
	UpdateAddon(ctx context.Context, id string, req dto.UpdateAddonRequest) (*dto.AddonResponse, error)
	DeleteAddon(ctx context.Context, id string) error

	// Add addon to subscription
	AddAddonToSubscription(ctx context.Context, subscriptionID string, req *dto.AddAddonToSubscriptionRequest) (*addon.SubscriptionAddon, error)

	// Remove addon from subscription
	RemoveAddonFromSubscription(ctx context.Context, subscriptionID string, addonID string, reason string) error
}

type addonService struct {
	ServiceParams
}

func NewAddonService(params ServiceParams) AddonService {
	return &addonService{
		ServiceParams: params,
	}
}

// CreateAddon creates a new addon with associated prices and entitlements
func (s *addonService) CreateAddon(ctx context.Context, req dto.CreateAddonRequest) (*dto.CreateAddonResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Convert request to domain model
	domainAddon := req.ToAddon(ctx)

	// Start a transaction to create addon, prices, and entitlements
	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		// 1. Create the addon
		if err := s.AddonRepo.Create(ctx, domainAddon); err != nil {
			return err
		}

		// 2. Create prices in bulk if present
		if len(req.Prices) > 0 {
			prices := make([]*price.Price, len(req.Prices))
			for i, priceReq := range req.Prices {
				price, err := priceReq.ToPrice(ctx)
				if err != nil {
					return ierr.WithError(err).
						WithHint("Failed to create price").
						Mark(ierr.ErrValidation)
				}
				price.AddonID = lo.ToPtr(domainAddon.ID)
				prices[i] = price
			}

			// Create prices in bulk
			if err := s.PriceRepo.CreateBulk(ctx, prices); err != nil {
				return err
			}
		}

		// 3. Create entitlements in bulk if present
		if len(req.Entitlements) > 0 {
			entitlements := make([]*entitlement.Entitlement, len(req.Entitlements))
			for i, entReq := range req.Entitlements {
				ent := entReq.ToEntitlement(ctx, lo.ToPtr(domainAddon.ID))
				ent.AddonID = lo.ToPtr(domainAddon.ID)
				entitlements[i] = ent
			}

			// Create entitlements in bulk
			if _, err := s.EntitlementRepo.CreateBulk(ctx, entitlements); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Return response
	return &dto.CreateAddonResponse{
		AddonResponse: &dto.AddonResponse{
			Addon: domainAddon,
		},
	}, nil
}

// GetAddon retrieves an addon by ID
func (s *addonService) GetAddon(ctx context.Context, id string) (*dto.AddonResponse, error) {
	if id == "" {
		return nil, ierr.NewError("addon ID is required").
			WithHint("Please provide a valid addon ID").
			Mark(ierr.ErrValidation)
	}

	domainAddon, err := s.AddonRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)
	entitlementService := NewEntitlementService(s.ServiceParams)

	pricesResponse, err := priceService.GetPricesByAddonID(ctx, domainAddon.ID)
	if err != nil {
		s.Logger.Errorw("failed to fetch prices for addon", "addon_id", domainAddon.ID, "error", err)
		return nil, err
	}

	entitlements, err := entitlementService.GetAddonEntitlements(ctx, domainAddon.ID)
	if err != nil {
		s.Logger.Errorw("failed to fetch entitlements for addon", "addon_id", domainAddon.ID, "error", err)
		return nil, err
	}

	return &dto.AddonResponse{
		Addon:        domainAddon,
		Prices:       pricesResponse.Items,
		Entitlements: entitlements.Items,
	}, nil
}

// GetAddonByLookupKey retrieves an addon by lookup key
func (s *addonService) GetAddonByLookupKey(ctx context.Context, lookupKey string) (*dto.AddonResponse, error) {
	if lookupKey == "" {
		return nil, ierr.NewError("lookup key is required").
			WithHint("Please provide a valid lookup key").
			Mark(ierr.ErrValidation)
	}

	domainAddon, err := s.AddonRepo.GetByLookupKey(ctx, lookupKey)
	if err != nil {
		return nil, err
	}

	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)
	entitlementService := NewEntitlementService(s.ServiceParams)

	pricesResponse, err := priceService.GetPricesByAddonID(ctx, domainAddon.ID)
	if err != nil {
		s.Logger.Errorw("failed to fetch prices for addon", "addon_id", domainAddon.ID, "error", err)
		return nil, err
	}

	entitlements, err := entitlementService.GetAddonEntitlements(ctx, domainAddon.ID)
	if err != nil {
		s.Logger.Errorw("failed to fetch entitlements for addon", "addon_id", domainAddon.ID, "error", err)
		return nil, err
	}

	return &dto.AddonResponse{
		Addon:        domainAddon,
		Prices:       pricesResponse.Items,
		Entitlements: entitlements.Items,
	}, nil
}

// GetAddons lists addons with filtering
func (s *addonService) GetAddons(ctx context.Context, filter *types.AddonFilter) (*dto.ListAddonsResponse, error) {
	if filter == nil {
		filter = types.NewAddonFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	result, err := s.AddonRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.AddonRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	items := lo.Map(result, func(addon *addon.Addon, _ int) *dto.AddonResponse {
		return &dto.AddonResponse{
			Addon: addon,
		}
	})

	response := &dto.ListAddonsResponse{
		Items: items,
		Pagination: types.NewPaginationResponse(
			count,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}

	if len(items) == 0 {
		return response, nil
	}

	// Expand prices and entitlements if requested
	addonIDs := lo.Map(result, func(addon *addon.Addon, _ int) string {
		return addon.ID
	})

	// Create maps for storing expanded data
	pricesByAddonID := make(map[string][]*dto.PriceResponse)
	entitlementsByAddonID := make(map[string][]*dto.EntitlementResponse)

	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)
	entitlementService := NewEntitlementService(s.ServiceParams)

	// If prices expansion is requested, fetch them in bulk
	if filter.GetExpand().Has(types.ExpandPrices) {
		priceFilter := types.NewNoLimitPriceFilter().
			WithAddonIDs(addonIDs).
			WithStatus(types.StatusPublished)

		// If meters should be expanded, propagate the expansion to prices
		if filter.GetExpand().Has(types.ExpandMeters) {
			priceFilter = priceFilter.WithExpand(string(types.ExpandMeters))
		}

		prices, err := priceService.GetPrices(ctx, priceFilter)
		if err != nil {
			return nil, err
		}

		for _, p := range prices.Items {
			pricesByAddonID[lo.FromPtr(p.AddonID)] = append(pricesByAddonID[lo.FromPtr(p.AddonID)], p)
		}
	}

	// If entitlements expansion is requested, fetch them in bulk
	if filter.GetExpand().Has(types.ExpandEntitlements) {
		entFilter := types.NewNoLimitEntitlementFilter().
			WithAddonIDs(addonIDs).
			WithStatus(types.StatusPublished)

		// If features should be expanded, propagate the expansion to entitlements
		if filter.GetExpand().Has(types.ExpandFeatures) {
			entFilter = entFilter.WithExpand(string(types.ExpandFeatures))
		}

		entitlements, err := entitlementService.ListEntitlements(ctx, entFilter)
		if err != nil {
			return nil, err
		}

		for _, e := range entitlements.Items {
			entitlementsByAddonID[*e.AddonID] = append(entitlementsByAddonID[*e.AddonID], e)
		}
	}

	// Attach expanded data to responses
	for i, addon := range result {
		if prices, ok := pricesByAddonID[addon.ID]; ok {
			response.Items[i].Prices = prices
		}
		if entitlements, ok := entitlementsByAddonID[addon.ID]; ok {
			response.Items[i].Entitlements = entitlements
		}
	}

	return response, nil
}

// UpdateAddon updates an existing addon
func (s *addonService) UpdateAddon(ctx context.Context, id string, req dto.UpdateAddonRequest) (*dto.AddonResponse, error) {
	if id == "" {
		return nil, ierr.NewError("addon ID is required").
			WithHint("Please provide a valid addon ID").
			Mark(ierr.ErrValidation)
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get existing addon
	domainAddon, err := s.AddonRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply basic updates
	if req.Name != nil {
		domainAddon.Name = *req.Name
	}
	if req.Description != nil {
		domainAddon.Description = *req.Description
	}
	if req.Metadata != nil {
		domainAddon.Metadata = req.Metadata
	}

	// Start a transaction for updating addon, prices, and entitlements
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// 1. Update the addon
		if err := s.AddonRepo.Update(ctx, domainAddon); err != nil {
			return err
		}

		// 2. Handle prices
		if len(req.Prices) > 0 {
			// Create maps for tracking
			reqPriceMap := make(map[string]dto.UpdateAddonPriceRequest)
			for _, reqPrice := range req.Prices {
				if reqPrice.ID != "" {
					reqPriceMap[reqPrice.ID] = reqPrice
				}
			}

			// Track prices to delete
			pricesToDelete := make([]string, 0)

			// Get existing prices for this addon
			priceFilter := types.NewPriceFilter()
			priceFilter.AddonIDs = []string{id}
			existingPrices, err := s.PriceRepo.List(ctx, priceFilter)
			if err != nil {
				return err
			}

			// Handle existing prices
			for _, price := range existingPrices {
				if reqPrice, ok := reqPriceMap[price.ID]; ok {
					// Update existing price
					if reqPrice.Description != "" {
						price.Description = reqPrice.Description
					}
					if reqPrice.Metadata != nil {
						price.Metadata = reqPrice.Metadata
					}
					if reqPrice.LookupKey != "" {
						price.LookupKey = reqPrice.LookupKey
					}
					if err := s.PriceRepo.Update(ctx, price); err != nil {
						return err
					}
				} else {
					// Delete price not in request
					pricesToDelete = append(pricesToDelete, price.ID)
				}
			}

			// Delete prices in bulk
			if len(pricesToDelete) > 0 {
				if err := s.PriceRepo.DeleteBulk(ctx, pricesToDelete); err != nil {
					return err
				}
			}

			// Create new prices
			newPrices := make([]*price.Price, 0)
			for _, reqPrice := range req.Prices {
				if reqPrice.ID == "" {
					newPrice, err := reqPrice.ToPrice(ctx)
					if err != nil {
						return ierr.WithError(err).
							WithHint("Failed to create price").
							Mark(ierr.ErrValidation)
					}
					newPrice.AddonID = lo.ToPtr(domainAddon.ID)
					newPrices = append(newPrices, newPrice)
				}
			}

			if len(newPrices) > 0 {
				if err := s.PriceRepo.CreateBulk(ctx, newPrices); err != nil {
					return err
				}
			}
		}

		// 3. Handle entitlements
		if len(req.Entitlements) > 0 {
			// Create maps for tracking
			reqEntMap := make(map[string]dto.UpdateAddonEntitlementRequest)
			for _, reqEnt := range req.Entitlements {
				if reqEnt.ID != "" {
					reqEntMap[reqEnt.ID] = reqEnt
				}
			}

			// Track entitlements to delete
			entsToDelete := make([]string, 0)

			// Get existing entitlements for this addon using filters
			entFilter := types.NewNoLimitEntitlementFilter()
			entFilter.AddonIDs = []string{id}
			existingEntitlements, err := s.EntitlementRepo.List(ctx, entFilter)
			if err != nil {
				return err
			}

			// Handle existing entitlements
			for _, ent := range existingEntitlements {
				if reqEnt, ok := reqEntMap[ent.ID]; ok {
					// Update existing entitlement
					ent.IsEnabled = reqEnt.IsEnabled
					if reqEnt.UsageLimit != nil {
						ent.UsageLimit = reqEnt.UsageLimit
					}
					if reqEnt.UsageResetPeriod != "" {
						ent.UsageResetPeriod = reqEnt.UsageResetPeriod
					}
					ent.IsSoftLimit = reqEnt.IsSoftLimit
					if reqEnt.StaticValue != "" {
						ent.StaticValue = reqEnt.StaticValue
					}
					if _, err := s.EntitlementRepo.Update(ctx, ent); err != nil {
						return err
					}
				} else {
					// Delete entitlement not in request
					entsToDelete = append(entsToDelete, ent.ID)
				}
			}

			// Delete entitlements in bulk
			if len(entsToDelete) > 0 {
				if err := s.EntitlementRepo.DeleteBulk(ctx, entsToDelete); err != nil {
					return err
				}
			}

			// Create new entitlements
			newEntitlements := make([]*entitlement.Entitlement, 0)
			for _, reqEnt := range req.Entitlements {
				if reqEnt.ID == "" {
					ent := reqEnt.ToEntitlement(ctx, lo.ToPtr(domainAddon.ID))
					newEntitlements = append(newEntitlements, ent)
				}
			}

			if len(newEntitlements) > 0 {
				if _, err := s.EntitlementRepo.CreateBulk(ctx, newEntitlements); err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &dto.AddonResponse{
		Addon: domainAddon,
	}, nil
}

// DeleteAddon soft deletes an addon
func (s *addonService) DeleteAddon(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("addon ID is required").
			WithHint("Please provide a valid addon ID").
			Mark(ierr.ErrValidation)
	}

	// Check if addon exists
	_, err := s.AddonRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	// Check if addon is in use by any subscriptions
	filter := types.NewSubscriptionAddonFilter()
	filter.AddonIDs = []string{id}
	filter.AddonStatuses = []types.AddonStatus{types.AddonStatusActive}
	filter.Limit = lo.ToPtr(1)

	activeSubscriptions, err := s.SubscriptionAddonRepo.List(ctx, filter)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to check addon usage").
			Mark(ierr.ErrSystem)
	}

	// Also check if any active line items exist for this addon
	lineItemFilter := types.NewSubscriptionLineItemFilter()
	lineItemFilter.AddonIDs = []string{id}
	lineItemFilter.Status = lo.ToPtr(types.StatusPublished)
	lineItemFilter.Limit = lo.ToPtr(1)

	activeLineItems, err := s.LineItemRepo.List(ctx, lineItemFilter)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to check addon line item usage").
			Mark(ierr.ErrSystem)
	}

	if len(activeSubscriptions) > 0 || len(activeLineItems) > 0 {
		return ierr.NewError("cannot delete addon that is in use").
			WithHint("Addon is currently active on one or more subscriptions. Remove it from all subscriptions before deleting.").
			WithReportableDetails(map[string]interface{}{
				"addon_id":                   id,
				"active_subscriptions_count": len(activeSubscriptions),
				"active_line_items_count":    len(activeLineItems),
			}).
			Mark(ierr.ErrValidation)
	}

	// Soft delete the addon
	if err := s.AddonRepo.Delete(ctx, id); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete addon").
			WithReportableDetails(map[string]interface{}{
				"addon_id": id,
			}).
			Mark(ierr.ErrSystem)
	}

	s.Logger.Infow("addon deleted successfully",
		"addon_id", id)

	return nil
}

// AddAddonToSubscription adds an addon to a subscription
func (s *addonService) AddAddonToSubscription(
	ctx context.Context,
	subscriptionID string,
	req *dto.AddAddonToSubscriptionRequest,
) (*addon.SubscriptionAddon, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get a to ensure its valid
	a, err := s.GetAddon(ctx, req.AddonID)
	if err != nil {
		return nil, err
	}

	if a.Addon.Status != types.StatusPublished {
		return nil, ierr.NewError("addon is not published").
			WithHint("Cannot add inactive addon to subscription").
			Mark(ierr.ErrValidation)
	}

	// Check if subscription exists and is active
	// TODO: Rethink should be take complete subscription object or just id?
	subscription, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	if subscription.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Cannot add addon to inactive subscription").
			Mark(ierr.ErrValidation)
	}

	// Check if addon is already added to subscription only for single instance addons
	if a.Addon.Type == types.AddonTypeSingleInstance {
		filter := types.NewSubscriptionAddonFilter()
		filter.AddonIDs = []string{req.AddonID}
		filter.SubscriptionIDs = []string{subscriptionID}
		filter.AddonStatuses = []types.AddonStatus{types.AddonStatusActive}
		filter.Limit = lo.ToPtr(1)

		existingAddons, err := s.SubscriptionAddonRepo.List(ctx, filter)
		if err != nil {
			return nil, err
		}

		if len(existingAddons) > 0 {
			return nil, ierr.NewError("addon is already added to subscription").
				WithHint("Cannot add addon to subscription that already has an active instance").
				Mark(ierr.ErrValidation)
		}
	}

	// Get prices for the addon
	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)
	priceFilter := types.NewNoLimitPriceFilter().
		WithAddonIDs([]string{req.AddonID}).
		WithExpand(string(types.ExpandMeters))
	pricesResponse, err := priceService.GetPrices(ctx, priceFilter)
	if err != nil {
		return nil, err
	}

	if len(pricesResponse.Items) == 0 {
		return nil, ierr.NewError("no prices found for addon").
			WithHint("The addon must have at least one price to be added to a subscription").
			WithReportableDetails(map[string]interface{}{
				"addon_id": req.AddonID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Create price map for easy lookup
	priceMap := make(map[string]*dto.PriceResponse, len(pricesResponse.Items))
	for _, p := range pricesResponse.Items {
		priceMap[p.Price.ID] = p
	}

	// Create subscription addon
	subscriptionAddon := req.ToDomain(ctx, subscriptionID)

	// Create line items for the addon
	lineItems := make([]*subscriptionDomain.SubscriptionLineItem, 0, len(pricesResponse.Items))
	for _, priceResponse := range pricesResponse.Items {
		price := priceResponse.Price
		lineItem := &subscriptionDomain.SubscriptionLineItem{
			ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID: subscriptionID,
			CustomerID:     subscription.CustomerID,
			AddonID:        lo.ToPtr(req.AddonID),
			SourceType:     types.SubscriptionLineItemSourceTypeAddon,
			DisplayName:    a.Addon.Name,
			Quantity:       decimal.Zero,
			Currency:       subscription.Currency,
			BillingPeriod:  price.BillingPeriod,
			InvoiceCadence: price.InvoiceCadence,
			TrialPeriod:    0,
			StartDate:      time.Now(),
			EndDate:        time.Time{},
			Metadata: map[string]string{
				"addon_id":        req.AddonID,
				"subscription_id": subscriptionID,
				"addon_quantity":  "1",
				"addon_status":    string(types.AddonStatusActive),
			},
			EnvironmentID: subscription.EnvironmentID,
			BaseModel:     types.GetDefaultBaseModel(ctx),
		}

		// Set price-related fields
		lineItem.PriceID = price.ID
		lineItem.PriceType = price.Type
		if price.Type == types.PRICE_TYPE_USAGE && price.MeterID != "" && priceResponse.Meter != nil {
			lineItem.MeterID = price.MeterID
			lineItem.MeterDisplayName = priceResponse.Meter.Name
			lineItem.DisplayName = priceResponse.Meter.Name
			lineItem.Quantity = decimal.Zero
		} else {
			lineItem.DisplayName = a.Addon.Name
			if lineItem.Quantity.IsZero() {
				lineItem.Quantity = decimal.NewFromInt(1)
			}
		}

		lineItems = append(lineItems, lineItem)
	}

	var result *addon.SubscriptionAddon

	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Create subscription addon
		err = s.SubscriptionAddonRepo.Create(ctx, subscriptionAddon)
		if err != nil {
			return err
		}

		// Create line items for the addon
		for _, lineItem := range lineItems {
			err = s.LineItemRepo.Create(ctx, lineItem)
			if err != nil {
				return err
			}
		}

		result = subscriptionAddon
		return nil
	})

	if err != nil {
		return nil, err
	}

	s.Logger.Infow("added addon to subscription",
		"subscription_id", subscriptionID,
		"addon_id", req.AddonID,
		"prices_count", len(pricesResponse.Items),
		"line_items_count", len(lineItems),
	)

	return result, nil
}

// RemoveAddonFromSubscription removes an addon from a subscription
func (s *addonService) RemoveAddonFromSubscription(
	ctx context.Context,
	subscriptionID string,
	addonID string,
	reason string,
) error {
	// Get subscription addon
	subscriptionAddons, err := s.SubscriptionAddonRepo.GetBySubscriptionID(ctx, subscriptionID)
	if err != nil {
		return err
	}

	var targetAddon *addon.SubscriptionAddon
	for _, sa := range subscriptionAddons {
		if sa.AddonID == addonID && sa.AddonStatus == types.AddonStatusActive {
			targetAddon = sa
			break
		}
	}

	if targetAddon == nil {
		return ierr.NewError("addon not found on subscription").
			WithHint("Addon is not active on this subscription").
			Mark(ierr.ErrNotFound)
	}

	// Update addon status to cancelled and delete line items in a transaction
	now := time.Now()
	targetAddon.AddonStatus = types.AddonStatusCancelled
	targetAddon.CancellationReason = reason
	targetAddon.CancelledAt = &now
	targetAddon.EndDate = &now

	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Update subscription addon
		err = s.SubscriptionAddonRepo.Update(ctx, targetAddon)
		if err != nil {
			return err
		}

		// End the corresponding line items for this addon (soft delete approach)
		subscription, err := s.SubRepo.Get(ctx, subscriptionID)
		if err != nil {
			return err
		}

		lineItemsEnded := 0
		for _, lineItem := range subscription.LineItems {
			// Debug logging to understand line item matching
			s.Logger.Infow("checking line item for addon removal",
				"subscription_id", subscriptionID,
				"addon_id", addonID,
				"line_item_id", lineItem.ID,
				"line_item_addon_id", lineItem.AddonID,
				"line_item_metadata", lineItem.Metadata,
				"source_type", lineItem.SourceType)

			// Check both metadata and direct addon_id field
			metadataMatch := lineItem.Metadata != nil && lineItem.Metadata["addon_id"] == addonID
			addonIDMatch := lineItem.AddonID != nil && *lineItem.AddonID == addonID

			if metadataMatch || addonIDMatch {
				s.Logger.Infow("found matching line item for addon removal",
					"subscription_id", subscriptionID,
					"addon_id", addonID,
					"line_item_id", lineItem.ID,
					"metadata_match", metadataMatch,
					"addon_id_match", addonIDMatch)

				// End the line item (soft delete approach like Togai)
				lineItem.EndDate = now
				lineItem.Status = types.StatusDeleted

				// Add metadata for audit trail
				if lineItem.Metadata == nil {
					lineItem.Metadata = make(map[string]string)
				}
				lineItem.Metadata["removal_reason"] = reason
				lineItem.Metadata["removed_at"] = now.Format(time.RFC3339)
				lineItem.Metadata["removed_by"] = types.GetUserID(ctx)

				err = s.LineItemRepo.Update(ctx, lineItem)
				if err != nil {
					s.Logger.Errorw("failed to end line item for addon",
						"subscription_id", subscriptionID,
						"addon_id", addonID,
						"line_item_id", lineItem.ID,
						"error", err)
					return err
				}
				lineItemsEnded++
			}
		}

		s.Logger.Infow("ended line items for addon removal",
			"subscription_id", subscriptionID,
			"addon_id", addonID,
			"line_items_ended", lineItemsEnded,
			"removal_reason", reason)

		return nil
	})

	if err != nil {
		return err
	}

	s.Logger.Infow("removed addon from subscription",
		"subscription_id", subscriptionID,
		"addon_id", addonID,
		"reason", reason)

	return nil
}
