package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

	// Addon Association operations
	ListAddonAssociations(ctx context.Context, filter *types.AddonAssociationFilter) (*dto.ListAddonAssociationsResponse, error)
	GetActiveAddonAssociation(ctx context.Context, req dto.GetActiveAddonAssociationRequest) (*dto.ListAddonAssociationsResponse, error)
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
	if err != nil {
		s.Logger.Errorw("failed to fetch entitlements for addon", "addon_id", id, "error", err)
		return nil, err
	}

	if len(entitlements.Items) > 0 {
		response.Entitlements = make([]*dto.EntitlementResponse, len(entitlements.Items))
		copy(response.Entitlements, entitlements.Items)
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

// ListAddonAssociations lists addon associations with optional expansions
func (s *addonService) ListAddonAssociations(ctx context.Context, filter *types.AddonAssociationFilter) (*dto.ListAddonAssociationsResponse, error) {
	if filter == nil {
		filter = types.NewAddonAssociationFilter()
	}
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	// Validate expand parameters against config
	if err := filter.GetExpand().Validate(types.AddonAssociationExpandConfig); err != nil {
		return nil, err
	}

	// Validate the filter
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	associations, err := s.AddonAssociationRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.AddonAssociationRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	resp := &dto.ListAddonAssociationsResponse{
		Items: make([]*dto.AddonAssociationResponse, len(associations)),
		Pagination: types.NewPaginationResponse(
			count,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}

	// Prepare expansion maps
	expand := filter.GetExpand()

	var addonMap map[string]*dto.AddonResponse
	if expand.Has(types.ExpandAddons) {
		addonMap = make(map[string]*dto.AddonResponse)
		addonIDs := lo.Uniq(lo.Map(associations, func(a *addonassociation.AddonAssociation, _ int) string {
			return a.AddonID
		}))
		if len(addonIDs) > 0 {
			addonFilter := types.NewNoLimitAddonFilter()
			addonFilter.AddonIDs = addonIDs
			addonFilter.Expand = lo.ToPtr(string(types.ExpandPrices))
			addonResults, err := s.GetAddons(ctx, addonFilter)
			if err != nil {
				return nil, err
			}
			for _, a := range addonResults.Items {
				if a != nil && a.Addon != nil {
					addonMap[a.Addon.ID] = a
				}
			}
		}
	}

	var subscriptionMap map[string]*dto.SubscriptionResponse
	if expand.Has(types.ExpandSubscription) {
		subscriptionMap = make(map[string]*dto.SubscriptionResponse)
		subscriptionIDs := lo.FilterMap(associations, func(a *addonassociation.AddonAssociation, _ int) (string, bool) {
			if a.EntityType == types.AddonAssociationEntityTypeSubscription {
				return a.EntityID, true
			}
			return "", false
		})
		if len(subscriptionIDs) > 0 {
			subService := NewSubscriptionService(s.ServiceParams)
			for _, subID := range lo.Uniq(subscriptionIDs) {
				subResp, err := subService.GetSubscription(ctx, subID)
				if err != nil {
					return nil, err
				}
				subscriptionMap[subID] = subResp
			}
		}
	}

	// Build response items
	for i, association := range associations {
		item := &dto.AddonAssociationResponse{
			AddonAssociation: association,
		}
		if addonMap != nil {
			if addonResp, ok := addonMap[association.AddonID]; ok {
				item.Addon = addonResp
			}
		}
		if subscriptionMap != nil && association.EntityType == types.AddonAssociationEntityTypeSubscription {
			if subResp, ok := subscriptionMap[association.EntityID]; ok {
				item.Subscription = subResp
			}
		}
		resp.Items[i] = item
	}

	return resp, nil
}

// GetActiveAddonAssociation retrieves active addon associations for a given entity at a point in time
func (s *addonService) GetActiveAddonAssociation(ctx context.Context, req dto.GetActiveAddonAssociationRequest) (*dto.ListAddonAssociationsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Use the start date from request, or default to current time
	periodStart := req.StartDate
	if periodStart == nil {
		now := time.Now()
		periodStart = &now
	}

	// Use end date if provided; if nil we treat it as point-in-time at start
	periodEnd := req.EndDate
	if periodEnd == nil {
		periodEnd = periodStart
	}

	// Build filter to fetch active addon associations using the generic list method
	filter := types.NewNoLimitAddonAssociationFilter()
	filter.QueryFilter = types.NewNoLimitQueryFilter()
	filter.EntityIDs = []string{req.EntityID}
	filter.EntityType = &req.EntityType
	filter.AddonStatus = lo.ToPtr(string(types.AddonStatusActive))
	filter.StartDate = periodStart
	filter.EndDate = periodEnd

	// Always expand addon details for active association lookup
	filter.QueryFilter.Expand = lo.ToPtr(string(types.ExpandAddons))
	if len(req.AddonIds) > 0 {
		filter.AddonIDs = req.AddonIds
	}

	associations, err := s.ListAddonAssociations(ctx, filter)
	if err != nil {
		return nil, err
	}

	return associations, nil
}
