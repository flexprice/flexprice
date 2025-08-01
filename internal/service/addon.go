package service

import (
	"context"
	"fmt"
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
	RemoveAddonFromSubscription(ctx context.Context, subscriptionID, addonID string, reason string) error

	// Pause addon
	PauseAddon(ctx context.Context, subscriptionID, addonID string, reason string) error

	// Resume addon
	ResumeAddon(ctx context.Context, subscriptionID, addonID string) error

	// Update addon quantity (creates new subscription line item)
	UpdateAddonQuantity(ctx context.Context, subscriptionID, addonID string, newQuantity int) error

	// Update addon price (creates new subscription line item)
	UpdateAddonPrice(ctx context.Context, subscriptionID, addonID string, newPriceID string) error

	// Get subscription addons
	GetSubscriptionAddons(ctx context.Context, subscriptionID string) ([]*addon.SubscriptionAddon, error)

	// Get addon usage
	GetAddonUsage(ctx context.Context, subscriptionID, addonID string, startTime, endTime time.Time) (*dto.AddonUsageResponse, error)

	// Calculate addon charges for billing period (integrated with billing service)
	CalculateAddonCharges(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error)
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

	// TODO: check if addon is in use by any subscriptions

	// Soft delete the addon
	if err := s.AddonRepo.Delete(ctx, id); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete addon").
			WithReportableDetails(map[string]interface{}{
				"addon_id": id,
			}).
			Mark(ierr.ErrSystem)
	}

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

	// Get addon to ensure its valid
	addon, err := s.GetAddon(ctx, req.AddonID)
	if err != nil {
		return nil, err
	}

	if addon.Addon.Status != types.StatusPublished {
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
	if addon.Addon.Type == types.AddonTypeSingleInstance {
		existingAddons, err := s.SubscriptionAddonRepo.GetBySubscriptionID(ctx, subscriptionID)
		if err != nil {
			return nil, err
		}

		for _, existingAddon := range existingAddons {
			if existingAddon.AddonID == req.AddonID && existingAddon.AddonStatus == types.AddonStatusActive {
				return nil, ierr.NewError("addon is already added to subscription").
					WithHint("Cannot add addon to subscription that already has an active instance").
					Mark(ierr.ErrValidation)
			}
		}
	}

	// Create subscription addon
	subscriptionAddon := req.ToDomain(ctx, subscriptionID)

	err = s.SubscriptionAddonRepo.Create(ctx, subscriptionAddon)
	if err != nil {
		return nil, err
	}

	// Create subscription line item for the addon
	lineItem := &subscriptionDomain.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: subscriptionID,
		CustomerID:     subscription.CustomerID,
		AddonID:        lo.ToPtr(req.AddonID),
		SourceType:     types.SubscriptionLineItemSourceTypeAddon,
		DisplayName:    addon.Addon.Name,
		Quantity:       decimal.NewFromInt(int64(req.Quantity)),
		Currency:       subscription.Currency,
		BillingPeriod:  subscription.BillingPeriod,
		InvoiceCadence: types.InvoiceCadenceAdvance,
		TrialPeriod:    0,
		StartDate:      time.Now(),
		EndDate:        time.Time{}, // No end date for active addons
		Metadata: map[string]string{
			"addon_id":        req.AddonID,
			"subscription_id": subscriptionID,
			"addon_quantity":  fmt.Sprintf("%d", req.Quantity),
			"addon_status":    string(types.AddonStatusActive),
		},
		EnvironmentID: subscription.EnvironmentID,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	err = s.LineItemRepo.Create(ctx, lineItem)
	if err != nil {
		// Rollback addon creation if line item creation fails
		s.Logger.Errorw("failed to create line item for addon, rolling back addon creation",
			"subscription_id", subscriptionID,
			"addon_id", req.AddonID,
			"error", err)
		return nil, err
	}

	s.Logger.Infow("added addon to subscription",
		"subscription_id", subscriptionID,
		"addon_id", req.AddonID,
		"price_id", "",
		"quantity", req.Quantity)

	return subscriptionAddon, nil
}

// RemoveAddonFromSubscription removes an addon from a subscription
func (s *addonService) RemoveAddonFromSubscription(
	ctx context.Context,
	subscriptionID, addonID string,
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

	// Update addon status to cancelled
	now := time.Now()
	targetAddon.AddonStatus = types.AddonStatusCancelled
	targetAddon.CancellationReason = reason
	targetAddon.CancelledAt = &now
	targetAddon.EndDate = &now

	err = s.SubscriptionAddonRepo.Update(ctx, targetAddon)
	if err != nil {
		return err
	}

	// End the corresponding line items for this addon
	subscription, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return err
	}

	for _, lineItem := range subscription.LineItems {
		if lineItem.Metadata["addon_id"] == addonID && lineItem.EndDate.IsZero() {
			lineItem.EndDate = now
			err = s.LineItemRepo.Update(ctx, lineItem)
			if err != nil {
				s.Logger.Errorw("failed to end line item for addon",
					"subscription_id", subscriptionID,
					"addon_id", addonID,
					"line_item_id", lineItem.ID,
					"error", err)
			}
		}
	}

	s.Logger.Infow("removed addon from subscription",
		"subscription_id", subscriptionID,
		"addon_id", addonID,
		"reason", reason)

	return nil
}

// PauseAddon pauses an addon on a subscription
func (s *addonService) PauseAddon(
	ctx context.Context,
	subscriptionID, addonID string,
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

	// Update addon status to paused
	targetAddon.AddonStatus = types.AddonStatusPaused
	targetAddon.CancellationReason = reason

	err = s.SubscriptionAddonRepo.Update(ctx, targetAddon)
	if err != nil {
		return err
	}

	// End the corresponding line items for this addon
	subscription, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return err
	}

	now := time.Now()
	for _, lineItem := range subscription.LineItems {
		if lineItem.Metadata["addon_id"] == addonID && lineItem.EndDate.IsZero() {
			lineItem.EndDate = now
			err = s.LineItemRepo.Update(ctx, lineItem)
			if err != nil {
				s.Logger.Errorw("failed to end line item for paused addon",
					"subscription_id", subscriptionID,
					"addon_id", addonID,
					"line_item_id", lineItem.ID,
					"error", err)
			}
		}
	}

	s.Logger.Infow("paused addon on subscription",
		"subscription_id", subscriptionID,
		"addon_id", addonID,
		"reason", reason)

	return nil
}

// ResumeAddon resumes a paused addon on a subscription
func (s *addonService) ResumeAddon(
	ctx context.Context,
	subscriptionID, addonID string,
) error {
	// Get subscription addon
	subscriptionAddons, err := s.SubscriptionAddonRepo.GetBySubscriptionID(ctx, subscriptionID)
	if err != nil {
		return err
	}

	var targetAddon *addon.SubscriptionAddon
	for _, sa := range subscriptionAddons {
		if sa.AddonID == addonID && sa.AddonStatus == types.AddonStatusPaused {
			targetAddon = sa
			break
		}
	}

	if targetAddon == nil {
		return ierr.NewError("addon not found or not paused").
			WithHint("Addon is not paused on this subscription").
			Mark(ierr.ErrNotFound)
	}

	// Update addon status to active
	targetAddon.AddonStatus = types.AddonStatusActive
	targetAddon.CancellationReason = ""

	err = s.SubscriptionAddonRepo.Update(ctx, targetAddon)
	if err != nil {
		return err
	}

	// Create new line item for the resumed addon
	subscription, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return err
	}

	// Get the original line item to copy its properties
	var originalLineItem *subscriptionDomain.SubscriptionLineItem
	for _, lineItem := range subscription.LineItems {
		if lineItem.Metadata["addon_id"] == addonID {
			originalLineItem = lineItem
			break
		}
	}

	if originalLineItem != nil {
		// Create new line item with current date
		newLineItem := &subscriptionDomain.SubscriptionLineItem{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:   subscriptionID,
			CustomerID:       subscription.CustomerID,
			PlanID:           originalLineItem.PlanID,
			PlanDisplayName:  originalLineItem.PlanDisplayName,
			PriceID:          originalLineItem.PriceID,
			PriceType:        originalLineItem.PriceType,
			MeterID:          originalLineItem.MeterID,
			MeterDisplayName: originalLineItem.MeterDisplayName,
			DisplayName:      originalLineItem.DisplayName,
			Quantity:         originalLineItem.Quantity,
			Currency:         subscription.Currency,
			BillingPeriod:    subscription.BillingPeriod,
			InvoiceCadence:   originalLineItem.InvoiceCadence,
			TrialPeriod:      originalLineItem.TrialPeriod,
			StartDate:        time.Now(),
			EndDate:          time.Time{}, // No end date for active addons
			Metadata:         originalLineItem.Metadata,
			EnvironmentID:    subscription.EnvironmentID,
			BaseModel:        types.GetDefaultBaseModel(ctx),
		}

		err = s.LineItemRepo.Create(ctx, newLineItem)
		if err != nil {
			s.Logger.Errorw("failed to create line item for resumed addon",
				"subscription_id", subscriptionID,
				"addon_id", addonID,
				"error", err)
			return err
		}
	}

	s.Logger.Infow("resumed addon on subscription",
		"subscription_id", subscriptionID,
		"addon_id", addonID)

	return nil
}

// UpdateAddonQuantity updates the quantity of an addon by creating a new line item
func (s *addonService) UpdateAddonQuantity(
	ctx context.Context,
	subscriptionID, addonID string,
	newQuantity int,
) error {
	if newQuantity <= 0 {
		return ierr.NewError("quantity must be greater than 0").
			Mark(ierr.ErrValidation)
	}

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

	// Get subscription to access line items
	subscription, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return err
	}

	// Find the current active line item for this addon
	var currentLineItem *subscriptionDomain.SubscriptionLineItem
	for _, lineItem := range subscription.LineItems {
		if lineItem.Metadata["addon_id"] == addonID && lineItem.EndDate.IsZero() {
			currentLineItem = lineItem
			break
		}
	}

	if currentLineItem == nil {
		return ierr.NewError("no active line item found for addon").
			Mark(ierr.ErrNotFound)
	}

	// End the current line item
	now := time.Now()
	currentLineItem.EndDate = now
	err = s.LineItemRepo.Update(ctx, currentLineItem)
	if err != nil {
		return err
	}

	// Create new line item with updated quantity
	newLineItem := &subscriptionDomain.SubscriptionLineItem{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:   subscriptionID,
		CustomerID:       subscription.CustomerID,
		AddonID:          lo.ToPtr(addonID),
		SourceType:       types.SubscriptionLineItemSourceTypeAddon,
		PlanDisplayName:  currentLineItem.PlanDisplayName,
		PriceID:          currentLineItem.PriceID,
		PriceType:        currentLineItem.PriceType,
		MeterID:          currentLineItem.MeterID,
		MeterDisplayName: currentLineItem.MeterDisplayName,
		DisplayName:      currentLineItem.DisplayName,
		Quantity:         decimal.NewFromInt(int64(newQuantity)),
		Currency:         subscription.Currency,
		BillingPeriod:    subscription.BillingPeriod,
		InvoiceCadence:   currentLineItem.InvoiceCadence,
		TrialPeriod:      currentLineItem.TrialPeriod,
		StartDate:        now,
		EndDate:          time.Time{}, // No end date for active addons
		Metadata: map[string]string{
			"addon_id":        addonID,
			"subscription_id": subscriptionID,
			"addon_quantity":  fmt.Sprintf("%d", newQuantity),
			"addon_status":    string(types.AddonStatusActive),
		},
		EnvironmentID: subscription.EnvironmentID,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	err = s.LineItemRepo.Create(ctx, newLineItem)
	if err != nil {
		return err
	}

	s.Logger.Infow("updated addon quantity",
		"subscription_id", subscriptionID,
		"addon_id", addonID,
		"new_quantity", newQuantity)

	return nil
}

// UpdateAddonPrice updates the price of an addon by creating a new line item
func (s *addonService) UpdateAddonPrice(
	ctx context.Context,
	subscriptionID, addonID string,
	newPriceID string,
) error {
	// Check if new price exists
	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)
	price, err := priceService.GetPrice(ctx, newPriceID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Price not found").
			Mark(ierr.ErrNotFound)
	}

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

	// Get subscription to access line items
	subscription, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return err
	}

	// Find the current active line item for this addon
	var currentLineItem *subscriptionDomain.SubscriptionLineItem
	for _, lineItem := range subscription.LineItems {
		if lineItem.Metadata["addon_id"] == addonID && lineItem.EndDate.IsZero() {
			currentLineItem = lineItem
			break
		}
	}

	if currentLineItem == nil {
		return ierr.NewError("no active line item found for addon").
			Mark(ierr.ErrNotFound)
	}

	// End the current line item
	now := time.Now()
	currentLineItem.EndDate = now
	err = s.LineItemRepo.Update(ctx, currentLineItem)
	if err != nil {
		return err
	}

	// Create new line item with updated price
	newLineItem := &subscriptionDomain.SubscriptionLineItem{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:   subscriptionID,
		CustomerID:       subscription.CustomerID,
		AddonID:          lo.ToPtr(addonID),
		SourceType:       types.SubscriptionLineItemSourceTypeAddon,
		PlanDisplayName:  currentLineItem.PlanDisplayName,
		PriceID:          newPriceID,
		PriceType:        types.PriceType(price.Type),
		MeterID:          price.MeterID,
		MeterDisplayName: price.Description,
		DisplayName:      price.Description,
		Quantity:         currentLineItem.Quantity,
		Currency:         subscription.Currency,
		BillingPeriod:    subscription.BillingPeriod,
		InvoiceCadence:   currentLineItem.InvoiceCadence,
		TrialPeriod:      currentLineItem.TrialPeriod,
		StartDate:        now,
		EndDate:          time.Time{}, // No end date for active addons
		Metadata: map[string]string{
			"addon_id":        addonID,
			"subscription_id": subscriptionID,
			"addon_quantity":  currentLineItem.Metadata["addon_quantity"],
			"addon_status":    string(types.AddonStatusActive),
		},
		EnvironmentID: subscription.EnvironmentID,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	err = s.LineItemRepo.Create(ctx, newLineItem)
	if err != nil {
		return err
	}

	s.Logger.Infow("updated addon price",
		"subscription_id", subscriptionID,
		"addon_id", addonID,
		"new_price_id", newPriceID)

	return nil
}

// GetSubscriptionAddons gets all addons for a subscription
func (s *addonService) GetSubscriptionAddons(
	ctx context.Context,
	subscriptionID string,
) ([]*addon.SubscriptionAddon, error) {
	return s.SubscriptionAddonRepo.GetBySubscriptionID(ctx, subscriptionID)
}

// GetAddonUsage gets usage for a specific addon
func (s *addonService) GetAddonUsage(
	ctx context.Context,
	subscriptionID, addonID string,
	startTime, endTime time.Time,
) (*dto.AddonUsageResponse, error) {
	// Get subscription addon
	subscriptionAddons, err := s.SubscriptionAddonRepo.GetBySubscriptionID(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	var targetAddon *addon.SubscriptionAddon
	for _, sa := range subscriptionAddons {
		if sa.AddonID == addonID {
			targetAddon = sa
			break
		}
	}

	if targetAddon == nil {
		return nil, ierr.NewError("addon not found on subscription").
			Mark(ierr.ErrNotFound)
	}

	// Get subscription to find the price ID for this addon
	subscription, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	var priceID string
	var quantity int
	for _, lineItem := range subscription.LineItems {
		if lineItem.Metadata["addon_id"] == addonID && lineItem.EndDate.IsZero() {
			priceID = lineItem.PriceID
			quantity = int(lineItem.Quantity.IntPart())
			break
		}
	}

	if priceID == "" {
		return nil, ierr.NewError("no active line item found for addon").
			Mark(ierr.ErrNotFound)
	}

	// Get usage from subscription service
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	usage, err := subscriptionService.GetUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: subscriptionID,
		StartTime:      startTime,
		EndTime:        endTime,
	})
	if err != nil {
		return nil, err
	}

	// Filter usage for this specific addon
	var addonUsage []*dto.SubscriptionUsageByMetersResponse
	for _, charge := range usage.Charges {
		if charge.Price.ID == priceID {
			addonUsage = append(addonUsage, charge)
		}
	}

	return &dto.AddonUsageResponse{
		SubscriptionID: subscriptionID,
		AddonID:        addonID,
		PriceID:        priceID,
		Quantity:       quantity,
		UsageLimit:     nil, // Addons don't have usage limits in the current model
		Charges:        addonUsage,
		PeriodStart:    startTime,
		PeriodEnd:      endTime,
	}, nil
}

// CalculateAddonCharges calculates charges for addons in a billing period
// This method now integrates with the billing service for consistent charge calculation
func (s *addonService) CalculateAddonCharges(
	ctx context.Context,
	subscriptionID string,
	periodStart, periodEnd time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
	// Get subscription with line items
	subscription, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, decimal.Zero, err
	}

	// Filter line items to only include addon line items
	var addonLineItems []*subscriptionDomain.SubscriptionLineItem
	for _, lineItem := range subscription.LineItems {
		if lineItem.Metadata["addon_id"] != "" && lineItem.IsActive(periodStart) {
			addonLineItems = append(addonLineItems, lineItem)
		}
	}

	if len(addonLineItems) == 0 {
		return []dto.CreateInvoiceLineItemRequest{}, decimal.Zero, nil
	}

	// Create a filtered subscription with only addon line items
	filteredSub := *subscription
	filteredSub.LineItems = addonLineItems

	// Use the billing service to calculate charges for addon line items
	billingService := NewBillingService(s.ServiceParams)

	// Calculate fixed charges for addons
	fixedCharges, fixedTotal, err := billingService.CalculateFixedCharges(ctx, &filteredSub, periodStart, periodEnd)
	if err != nil {
		return nil, decimal.Zero, err
	}

	// Calculate usage charges for addons
	usageCharges, usageTotal, err := billingService.CalculateUsageCharges(ctx, &filteredSub, nil, periodStart, periodEnd)
	if err != nil {
		return nil, decimal.Zero, err
	}

	// Combine all charges
	allCharges := append(fixedCharges, usageCharges...)
	totalAmount := fixedTotal.Add(usageTotal)

	// Add addon-specific metadata to line items
	for i := range allCharges {
		if allCharges[i].Metadata == nil {
			allCharges[i].Metadata = make(types.Metadata)
		}
		allCharges[i].Metadata["is_addon"] = "true"
		allCharges[i].Metadata["description"] = fmt.Sprintf("Addon: %s", *allCharges[i].DisplayName)
	}

	return allCharges, totalAmount, nil
}

// // AddAddonToSubscription adds an addon to a subscription
// func (s *addonService) AddAddonToSubscription(ctx context.Context, subscriptionID string, req dto.AddAddonToSubscriptionRequest) (*dto.SubscriptionAddonResponse, error) {
// 	if subscriptionID == "" {
// 		return nil, ierr.NewError("subscription ID is required").
// 			WithHint("Please provide a valid subscription ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	if err := req.Validate(); err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Please check the addon data").
// 			Mark(ierr.ErrValidation)
// 	}

// 	// Validate addon compatibility
// 	if err := s.ValidateAddonCompatibility(ctx, subscriptionID, req.AddonID); err != nil {
// 		return nil, err
// 	}

// 	// Check if this is a single-instance addon and already exists
// 	domainAddon, err := s.AddonRepo.GetByID(ctx, req.AddonID)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"addon_id": req.AddonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	if domainAddon.Type == types.AddonTypeSingleInstance {
// 		existingAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
// 		if err != nil {
// 			return nil, ierr.WithError(err).
// 				WithHint("Failed to check existing addons").
// 				WithReportableDetails(map[string]interface{}{
// 					"subscription_id": subscriptionID,
// 				}).
// 				Mark(ierr.ErrSystem)
// 		}

// 		for _, existing := range existingAddons {
// 			if existing.AddonID == req.AddonID && existing.AddonStatus == types.AddonStatusActive {
// 				return nil, ierr.NewError("single-instance addon already exists on subscription").
// 					WithHint("This addon can only be added once per subscription").
// 					WithReportableDetails(map[string]interface{}{
// 						"subscription_id": subscriptionID,
// 						"addon_id":        req.AddonID,
// 					}).
// 					Mark(ierr.ErrValidation)
// 			}
// 		}
// 	}

// 	// Convert to domain model
// 	subscriptionAddon := req.ToDomain(ctx, subscriptionID)
// 	if subscriptionAddon == nil {
// 		return nil, ierr.NewError("failed to convert request to domain").
// 			WithHint("Invalid addon data provided").
// 			Mark(ierr.ErrSystem)
// 	}

// 	// Calculate proration if needed
// 	if req.ProrationBehavior == types.ProrationBehaviorCreateProrations {
// 		proratedAmount, err := s.CalculateAddonProration(ctx, subscriptionID, req.AddonID, *subscriptionAddon.StartDate)
// 		if err != nil {
// 			return nil, err
// 		}
// 		subscriptionAddon.ProratedAmount = proratedAmount
// 	}

// 	// Create subscription addon
// 	if err := s.AddonRepo.CreateSubscriptionAddon(ctx, subscriptionAddon); err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Failed to add addon to subscription").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id": subscriptionID,
// 				"addon_id":        req.AddonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	response := &dto.SubscriptionAddonResponse{}
// 	return response.FromDomain(subscriptionAddon), nil
// }

// // RemoveAddonFromSubscription removes an addon from a subscription
// func (s *addonService) RemoveAddonFromSubscription(ctx context.Context, subscriptionID, addonID string) error {
// 	if subscriptionID == "" {
// 		return ierr.NewError("subscription ID is required").
// 			WithHint("Please provide a valid subscription ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	if addonID == "" {
// 		return ierr.NewError("addon ID is required").
// 			WithHint("Please provide a valid addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	// Find the subscription addon
// 	subscriptionAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Failed to retrieve subscription addons").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id": subscriptionID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	var targetAddon *addon.SubscriptionAddon
// 	for _, sa := range subscriptionAddons {
// 		if sa.AddonID == addonID && sa.AddonStatus == types.AddonStatusActive {
// 			targetAddon = sa
// 			break
// 		}
// 	}

// 	if targetAddon == nil {
// 		return ierr.NewError("addon not found on subscription").
// 			WithHint("The addon is not active on this subscription").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id": subscriptionID,
// 				"addon_id":        addonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Cancel the addon
// 	return s.CancelSubscriptionAddon(ctx, targetAddon.ID, "removed_by_user")
// }

// // UpdateSubscriptionAddon updates a subscription addon
// func (s *addonService) UpdateSubscriptionAddon(ctx context.Context, subscriptionAddonID string, req dto.UpdateSubscriptionAddonRequest) (*dto.SubscriptionAddonResponse, error) {
// 	if subscriptionAddonID == "" {
// 		return nil, ierr.NewError("subscription addon ID is required").
// 			WithHint("Please provide a valid subscription addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	if err := req.Validate(); err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Please check the update data").
// 			Mark(ierr.ErrValidation)
// 	}

// 	// Get existing subscription addon
// 	subscriptionAddon, err := s.AddonRepo.GetSubscriptionAddonByID(ctx, subscriptionAddonID)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Subscription addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Apply updates
// 	if req.Quantity != nil {
// 		subscriptionAddon.Quantity = *req.Quantity
// 	}
// 	if req.PriceID != nil {
// 		subscriptionAddon.PriceID = *req.PriceID
// 	}
// 	if req.ProrationBehavior != nil {
// 		subscriptionAddon.ProrationBehavior = *req.ProrationBehavior
// 	}
// 	if req.Metadata != nil {
// 		subscriptionAddon.Metadata = req.Metadata
// 	}

// 	// Update subscription addon
// 	if err := s.AddonRepo.UpdateSubscriptionAddon(ctx, subscriptionAddon); err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Failed to update subscription addon").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	response := &dto.SubscriptionAddonResponse{}
// 	return response.FromDomain(subscriptionAddon), nil
// }

// // GetSubscriptionAddons retrieves all addons for a subscription
// func (s *addonService) GetSubscriptionAddons(ctx context.Context, subscriptionID string) (*dto.ListSubscriptionAddonsResponse, error) {
// 	if subscriptionID == "" {
// 		return nil, ierr.NewError("subscription ID is required").
// 			WithHint("Please provide a valid subscription ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	subscriptionAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Failed to retrieve subscription addons").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id": subscriptionID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	return &dto.ListSubscriptionAddonsResponse{
// 		Items: dto.SubscriptionAddonDomainToResponses(subscriptionAddons),
// 	}, nil
// }

// // GetSubscriptionAddon retrieves a specific subscription addon
// func (s *addonService) GetSubscriptionAddon(ctx context.Context, subscriptionAddonID string) (*dto.SubscriptionAddonResponse, error) {
// 	if subscriptionAddonID == "" {
// 		return nil, ierr.NewError("subscription addon ID is required").
// 			WithHint("Please provide a valid subscription addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	subscriptionAddon, err := s.AddonRepo.GetSubscriptionAddonByID(ctx, subscriptionAddonID)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Subscription addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	response := &dto.SubscriptionAddonResponse{}
// 	return response.FromDomain(subscriptionAddon), nil
// }

// // CalculateAddonProration calculates the prorated amount for an addon change
// func (s *addonService) CalculateAddonProration(ctx context.Context, subscriptionID, addonID string, changeDate time.Time) (*decimal.Decimal, error) {
// 	if subscriptionID == "" {
// 		return nil, ierr.NewError("subscription ID is required").
// 			WithHint("Please provide a valid subscription ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	if addonID == "" {
// 		return nil, ierr.NewError("addon ID is required").
// 			WithHint("Please provide a valid addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	// Get subscription details
// 	sub, err := s.SubRepo.Get(ctx, subscriptionID)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Subscription not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id": subscriptionID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Get addon price
// 	_, err = s.AddonRepo.GetByID(ctx, addonID)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"addon_id": addonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Get addon prices (we'll use the first fixed price for proration)
// 	filter := types.NewPriceFilter()
// 	filter.Filters = []*types.FilterCondition{
// 		{Field: "addon_id", Operator: types.FilterOperatorEQ, Value: &addonID},
// 		{Field: "type", Operator: types.FilterOperatorEQ, Value: &types.PriceTypeFixed},
// 	}

// 	pricesResult, err := s.PriceRepo.List(ctx, filter)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Failed to retrieve addon prices").
// 			WithReportableDetails(map[string]interface{}{
// 				"addon_id": addonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	if len(pricesResult.Items) == 0 {
// 		// No fixed price found, return zero proration
// 		zero := decimal.Zero
// 		return &zero, nil
// 	}

// 	price := pricesResult.Items[0]

// 	// Calculate days remaining in current period
// 	periodStart := sub.CurrentPeriodStart
// 	periodEnd := sub.CurrentPeriodEnd
// 	totalDays := int(periodEnd.Sub(periodStart).Hours() / 24)
// 	remainingDays := int(periodEnd.Sub(changeDate).Hours() / 24)

// 	if remainingDays <= 0 {
// 		zero := decimal.Zero
// 		return &zero, nil
// 	}

// 	// Calculate prorated amount
// 	dailyRate := price.Amount.Div(decimal.NewFromInt(int64(totalDays)))
// 	proratedAmount := dailyRate.Mul(decimal.NewFromInt(int64(remainingDays)))

// 	return &proratedAmount, nil
// }

// // CancelSubscriptionAddon cancels a subscription addon
// func (s *addonService) CancelSubscriptionAddon(ctx context.Context, subscriptionAddonID, reason string) error {
// 	if subscriptionAddonID == "" {
// 		return ierr.NewError("subscription addon ID is required").
// 			WithHint("Please provide a valid subscription addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	subscriptionAddon, err := s.AddonRepo.GetSubscriptionAddonByID(ctx, subscriptionAddonID)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Subscription addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Update status to cancelled
// 	now := time.Now()
// 	subscriptionAddon.AddonStatus = types.AddonStatusCancelled
// 	subscriptionAddon.CancellationReason = reason
// 	subscriptionAddon.CancelledAt = &now
// 	subscriptionAddon.EndDate = &now

// 	if err := s.AddonRepo.UpdateSubscriptionAddon(ctx, subscriptionAddon); err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Failed to cancel subscription addon").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	return nil
// }

// // PauseSubscriptionAddon pauses a subscription addon
// func (s *addonService) PauseSubscriptionAddon(ctx context.Context, subscriptionAddonID string) error {
// 	if subscriptionAddonID == "" {
// 		return ierr.NewError("subscription addon ID is required").
// 			WithHint("Please provide a valid subscription addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	subscriptionAddon, err := s.AddonRepo.GetSubscriptionAddonByID(ctx, subscriptionAddonID)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Subscription addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	subscriptionAddon.AddonStatus = types.AddonStatusPaused
// 	if err := s.AddonRepo.UpdateSubscriptionAddon(ctx, subscriptionAddon); err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Failed to pause subscription addon").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	return nil
// }

// // ResumeSubscriptionAddon resumes a paused subscription addon
// func (s *addonService) ResumeSubscriptionAddon(ctx context.Context, subscriptionAddonID string) error {
// 	if subscriptionAddonID == "" {
// 		return ierr.NewError("subscription addon ID is required").
// 			WithHint("Please provide a valid subscription addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	subscriptionAddon, err := s.AddonRepo.GetSubscriptionAddonByID(ctx, subscriptionAddonID)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Subscription addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	subscriptionAddon.AddonStatus = types.AddonStatusActive
// 	if err := s.AddonRepo.UpdateSubscriptionAddon(ctx, subscriptionAddon); err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Failed to resume subscription addon").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	return nil
// }

// // ValidateAddonCompatibility validates if an addon can be added to a subscription
// func (s *addonService) ValidateAddonCompatibility(ctx context.Context, subscriptionID, addonID string) error {
// 	if subscriptionID == "" {
// 		return ierr.NewError("subscription ID is required").
// 			WithHint("Please provide a valid subscription ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	if addonID == "" {
// 		return ierr.NewError("addon ID is required").
// 			WithHint("Please provide a valid addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	// Get subscription
// 	sub, err := s.SubRepo.GetByID(ctx, subscriptionID)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Subscription not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id": subscriptionID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Get addon
// 	domainAddon, err := s.AddonRepo.GetByID(ctx, addonID)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"addon_id": addonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Check if subscription is in a valid state for addon changes
// 	if sub.SubscriptionStatus != types.SubscriptionStatusActive &&
// 		sub.SubscriptionStatus != types.SubscriptionStatusTrialing {
// 		return ierr.NewError("subscription is not in a valid state for addon changes").
// 			WithHint("Subscription must be active or trialing").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id":     subscriptionID,
// 				"subscription_status": sub.SubscriptionStatus,
// 			}).
// 			Mark(ierr.ErrInvalidOperation)
// 	}

// 	// Get addon prices
// 	filter := types.NewPriceFilter()

// 	pricesResult, err := s.PriceRepo.List(ctx, filter)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Failed to retrieve addon prices").
// 			WithReportableDetails(map[string]interface{}{
// 				"addon_id": addonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	// Check currency compatibility
// 	for _, price := range pricesResult {
// 		if price.Currency != sub.Currency {
// 			return ierr.NewError("addon price currencxy does not match subscription currency").
// 				WithHint("All addon prices must use the same currency as the subscription").
// 				WithReportableDetails(map[string]interface{}{
// 					"subscription_id":       subscriptionID,
// 					"subscription_currency": sub.Currency,
// 					"addon_currency":        price.Currency,
// 				}).
// 				Mark(ierr.ErrValidation)
// 		}
// 	}

// 	// Check billing period compatibility (addon should align with subscription billing)
// 	for _, price := range pricesResult.Items {
// 		if price.BillingPeriod != sub.BillingPeriod {
// 			return ierr.NewError("addon billing period does not match subscription billing period").
// 				WithHint("Addon billing period must match subscription billing period").
// 				WithReportableDetails(map[string]interface{}{
// 					"subscription_id":             subscriptionID,
// 					"subscription_billing_period": sub.BillingPeriod,
// 					"addon_billing_period":        price.BillingPeriod,
// 				}).
// 				Mark(ierr.ErrValidation)
// 		}
// 	}

// 	return nil
// }
