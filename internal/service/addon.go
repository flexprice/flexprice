package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/subscription"
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

	if err := s.AddonRepo.Create(ctx, domainAddon); err != nil {
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

	addon, err := s.AddonRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	response := &dto.AddonResponse{
		Addon: addon,
	}

	// Get prices for this addon using filter
	priceService := NewPriceService(s.ServiceParams)
	prices, err := priceService.GetPricesByAddonID(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to fetch prices for addon", "addon_id", id, "error", err)
		return nil, err
	}

	if len(prices.Items) > 0 {
		response.Prices = make([]*dto.PriceResponse, len(prices.Items))
		copy(response.Prices, prices.Items)
	}

	// Get entitlements for this addon
	entitlementService := NewEntitlementService(s.ServiceParams)
	entitlements, err := entitlementService.GetAddonEntitlements(ctx, id)
	if err == nil && len(entitlements.Items) > 0 {
		response.Entitlements = make([]*dto.EntitlementResponse, len(entitlements.Items))
		for i, entitlement := range entitlements.Items {
			response.Entitlements[i] = &dto.EntitlementResponse{Entitlement: entitlement.Entitlement}
		}
	}

	return response, nil
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

	priceService := NewPriceService(s.ServiceParams)
	entitlementService := NewEntitlementService(s.ServiceParams)

	pricesResponse, err := s.getPricesByAddonID(ctx, priceService, domainAddon.ID)
	if err != nil {
		s.Logger.Errorw("failed to fetch prices for addon", "addon_id", domainAddon.ID, "error", err)
		return nil, err
	}

	entitlements, err := s.getAddonEntitlements(ctx, entitlementService, domainAddon.ID)
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

	priceService := NewPriceService(s.ServiceParams)
	entitlementService := NewEntitlementService(s.ServiceParams)

	// If prices expansion is requested, fetch them in bulk
	if filter.GetExpand().Has(types.ExpandPrices) {
		priceFilter := types.NewNoLimitPriceFilter().
			WithEntityIDs(addonIDs).
			WithEntityType(types.PRICE_ENTITY_TYPE_ADDON).
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
			pricesByAddonID[p.EntityID] = append(pricesByAddonID[p.EntityID], p)
		}
	}

	// If entitlements expansion is requested, fetch them in bulk
	if filter.GetExpand().Has(types.ExpandEntitlements) {
		entFilter := types.NewNoLimitEntitlementFilter().
			WithEntityIDs(addonIDs).
			WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_ADDON).
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
			entitlementsByAddonID[e.EntityID] = append(entitlementsByAddonID[e.EntityID], e)
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

	// Update the addon
	if err := s.AddonRepo.Update(ctx, domainAddon); err != nil {
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
	filter := types.NewAddonAssociationFilter()
	filter.AddonIDs = []string{id}
	filter.AddonStatus = lo.ToPtr(string(types.AddonStatusActive))
	filter.Limit = lo.ToPtr(1)

	activeSubscriptions, err := s.AddonAssociationRepo.List(ctx, filter)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to check addon usage").
			Mark(ierr.ErrSystem)
	}

	// Also check if any active line items exist for this addon
	lineItemFilter := types.NewSubscriptionLineItemFilter()
	lineItemFilter.EntityIDs = []string{id}
	lineItemFilter.EntityType = lo.ToPtr(types.SubscriptionLineItemEntityTypeAddon)
	lineItemFilter.Status = lo.ToPtr(types.StatusPublished)
	lineItemFilter.Limit = lo.ToPtr(1)

	activeLineItems, err := s.SubscriptionLineItemRepo.List(ctx, lineItemFilter)
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

// getPricesByAddonID fetches prices for a specific addon
func (s *addonService) getPricesByAddonID(ctx context.Context, priceService PriceService, addonID string) (*dto.ListPricesResponse, error) {
	if addonID == "" {
		return nil, ierr.NewError("addon_id is required").
			WithHint("Addon ID is required").
			Mark(ierr.ErrValidation)
	}

	// Use unlimited filter to fetch addon-scoped prices only
	priceFilter := types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{addonID}).
		WithEntityType(types.PRICE_ENTITY_TYPE_ADDON).
		WithStatus(types.StatusPublished).
		WithExpand(string(types.ExpandMeters))

	response, err := priceService.GetPrices(ctx, priceFilter)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// getAddonEntitlements fetches entitlements for a specific addon
func (s *addonService) getAddonEntitlements(ctx context.Context, entitlementService EntitlementService, addonID string) (*dto.ListEntitlementsResponse, error) {
	if addonID == "" {
		return nil, ierr.NewError("addon_id is required").
			WithHint("Addon ID is required").
			Mark(ierr.ErrValidation)
	}

	// Use unlimited filter to fetch addon-scoped entitlements only
	entFilter := types.NewNoLimitEntitlementFilter().
		WithEntityIDs([]string{addonID}).
		WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_ADDON).
		WithStatus(types.StatusPublished).
		WithExpand(string(types.ExpandFeatures))

	response, err := entitlementService.ListEntitlements(ctx, entFilter)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// createLineItemFromPrice creates a subscription line item from a price
func (s *addonService) createLineItemFromPrice(ctx context.Context, priceResponse *dto.PriceResponse, sub *subscription.Subscription, addonID, addonName string) *subscription.SubscriptionLineItem {
	price := priceResponse.Price

	lineItem := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: sub.ID,
		CustomerID:     sub.CustomerID,
		EntityID:       addonID,
		EntityType:     types.SubscriptionLineItemEntityTypeAddon,
		PriceID:        price.ID,
		PriceType:      price.Type,
		Currency:       sub.Currency,
		BillingPeriod:  price.BillingPeriod,
		InvoiceCadence: price.InvoiceCadence,
		TrialPeriod:    0,
		StartDate:      time.Now(),
		EndDate:        time.Time{},
		Metadata: map[string]string{
			"addon_id":        addonID,
			"subscription_id": sub.ID,
			"addon_quantity":  "1",
			"addon_status":    string(types.AddonStatusActive),
		},
		EnvironmentID: sub.EnvironmentID,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	// Set price-related fields
	if price.Type == types.PRICE_TYPE_USAGE && price.MeterID != "" && priceResponse.Meter != nil {
		lineItem.MeterID = price.MeterID
		lineItem.MeterDisplayName = priceResponse.Meter.Name
		lineItem.DisplayName = priceResponse.Meter.Name
		lineItem.Quantity = decimal.Zero
	} else {
		lineItem.DisplayName = addonName
		lineItem.Quantity = decimal.NewFromInt(1)
	}

	return lineItem
}
