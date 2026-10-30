package service

import (
	"context"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/addonpricesync"
	domainPrice "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
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

	// GetAddonCreditGrants lists the ADDON-scoped credit grants defined on an addon
	GetAddonCreditGrants(ctx context.Context, id string) (*dto.ListCreditGrantsResponse, error)

	// Addon Association operations
	ListAddonAssociations(ctx context.Context, filter *types.AddonAssociationFilter) (*dto.ListAddonAssociationsResponse, error)
	GetActiveAddonAssociation(ctx context.Context, req dto.GetActiveAddonAssociationRequest) (*dto.ListAddonAssociationsResponse, error)

	// SyncAddonPrices reconciles existing subscriptions holding the addon with
	// its current usage prices (overridden + newly added usage charges).
	SyncAddonPrices(ctx context.Context, addonID string) (*dto.SyncAddonPricesResponse, error)
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
		s.Logger.Error(ctx, "failed to fetch prices for addon", "addon_id", id, "error", err)
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
		s.Logger.Error(ctx, "failed to fetch entitlements for addon", "addon_id", id, "error", err)
		return nil, err
	}

	if len(entitlements.Items) > 0 {
		response.Entitlements = make([]*dto.EntitlementResponse, len(entitlements.Items))
		copy(response.Entitlements, entitlements.Items)
	}

	// Get credit grants for this addon
	creditGrants, err := NewCreditGrantService(s.ServiceParams).GetCreditGrantsByAddon(ctx, id)
	if err != nil {
		s.Logger.Error(ctx, "failed to fetch credit grants for addon", "addon_id", id, "error", err)
		return nil, err
	}

	if len(creditGrants.Items) > 0 {
		response.CreditGrants = creditGrants.Items
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
		s.Logger.Error(ctx, "failed to fetch prices for addon", "addon_id", domainAddon.ID, "error", err)
		return nil, err
	}

	entitlements, err := entitlementService.GetAddonEntitlements(ctx, domainAddon.ID)
	if err != nil {
		s.Logger.Error(ctx, "failed to fetch entitlements for addon", "addon_id", domainAddon.ID, "error", err)
		return nil, err
	}

	creditGrants, err := NewCreditGrantService(s.ServiceParams).GetCreditGrantsByAddon(ctx, domainAddon.ID)
	if err != nil {
		s.Logger.Error(ctx, "failed to fetch credit grants for addon", "addon_id", domainAddon.ID, "error", err)
		return nil, err
	}

	return &dto.AddonResponse{
		Addon:        domainAddon,
		Prices:       pricesResponse.Items,
		Entitlements: entitlements.Items,
		CreditGrants: creditGrants.Items,
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

	count := len(result)
	if !filter.IsUnlimited() {
		count, err = s.AddonRepo.Count(ctx, filter)
		if err != nil {
			return nil, err
		}
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
	creditGrantsByAddonID := make(map[string][]*dto.CreditGrantResponse)

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

	// If credit grants expansion is requested, fetch them per addon
	if filter.GetExpand().Has(types.ExpandCreditGrant) {
		for _, addonID := range addonIDs {
			creditGrants, err := s.CreditGrantRepo.GetByAddon(ctx, addonID)
			if err != nil {
				return nil, err
			}
			for _, cg := range creditGrants {
				creditGrantsByAddonID[addonID] = append(creditGrantsByAddonID[addonID], &dto.CreditGrantResponse{CreditGrant: cg})
			}
		}
	}

	// If price sync status expansion is requested, compute it per addon.
	syncStatusByAddonID := make(map[string]*dto.AddonPriceSyncStatusResponse)
	if filter.GetExpand().Has(types.ExpandPriceSyncStatus) {
		for _, addonID := range addonIDs {
			status, err := s.addonPriceSyncStatus(ctx, addonID)
			if err != nil {
				return nil, err
			}
			syncStatusByAddonID[addonID] = status
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
		if creditGrants, ok := creditGrantsByAddonID[addon.ID]; ok {
			response.Items[i].CreditGrants = creditGrants
		}
		if syncStatus, ok := syncStatusByAddonID[addon.ID]; ok {
			response.Items[i].PriceSyncStatus = syncStatus
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
// GetAddonCreditGrants lists the ADDON-scoped credit grants defined on an addon.
func (s *addonService) GetAddonCreditGrants(ctx context.Context, id string) (*dto.ListCreditGrantsResponse, error) {
	if id == "" {
		return nil, ierr.NewError("addon ID is required").
			WithHint("Please provide a valid addon ID").
			Mark(ierr.ErrValidation)
	}

	// Ensure the addon exists (returns 404 otherwise)
	if _, err := s.AddonRepo.GetByID(ctx, id); err != nil {
		return nil, err
	}

	creditGrantService := NewCreditGrantService(s.ServiceParams)
	return creditGrantService.GetCreditGrantsByAddon(ctx, id)
}

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

	s.Logger.Info(ctx, "addon deleted successfully",
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

	count := len(associations)
	if !filter.IsUnlimited() {
		count, err = s.AddonAssociationRepo.Count(ctx, filter)
		if err != nil {
			return nil, err
		}
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

	periodStart := req.StartDate
	periodEnd := req.EndDate

	if req.EntityType == types.AddonAssociationEntityTypeSubscription && (periodStart == nil || periodEnd == nil) {
		sub, err := s.SubRepo.Get(ctx, req.EntityID)
		if err != nil {
			return nil, err
		}
		if periodStart == nil {
			t := sub.CurrentPeriodStart
			periodStart = &t
		}
		if periodEnd == nil {
			t := sub.CurrentPeriodEnd
			periodEnd = &t
		}
	}

	// Build filter to fetch active addon associations using the generic list method
	filter := types.NewNoLimitAddonAssociationFilter()
	filter.QueryFilter = types.NewNoLimitQueryFilter()
	filter.EntityIDs = []string{req.EntityID}
	filter.EntityType = &req.EntityType
	addonStatuses := req.AddonStatuses
	if len(addonStatuses) == 0 {
		addonStatuses = []types.AddonStatus{types.AddonStatusActive}
	}
	filter.AddonStatuses = lo.Map(addonStatuses, func(status types.AddonStatus, _ int) string {
		return string(status)
	})
	filter.StartDate = periodStart
	filter.EndDate = periodEnd
	filter.ActiveAt = req.ActiveAt

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

// SyncAddonPrices reconciles existing subscriptions that hold the addon with its
// current usage prices. It is keyed on the addon association: the target sequence
// is max(prices.sequence) over the addon's published usage prices, and each
// association is stamped to it after reconciliation. Usage-only, raises no
// invoice, idempotent via stamping. The plan price sync (SyncPlanPricesV2) is a
// close reference for the overall shape; this differs by being per-association
// and by matching catalog prices with the divisor-compatible addon attach rule.
func (s *addonService) SyncAddonPrices(ctx context.Context, addonID string) (*dto.SyncAddonPricesResponse, error) {
	syncStart := types.Now()

	if addonID == "" {
		return nil, ierr.NewError("addon ID is required").
			WithHint("Please provide a valid addon ID").
			Mark(ierr.ErrValidation)
	}

	addonModel, err := s.AddonRepo.GetByID(ctx, addonID)
	if err != nil {
		return nil, err
	}

	targetSeq, err := s.AddonPriceSyncRepo.CurrentAddonSequence(ctx, addonID)
	if err != nil {
		return nil, err
	}
	if targetSeq == 0 {
		s.Logger.Info(ctx, "no addon usage prices to sync", "addon_id", addonID)
		return &dto.SyncAddonPricesResponse{
			AddonID: addonID,
			Message: "No addon usage prices to sync",
		}, nil
	}

	activeUsagePrices, err := s.activeAddonUsagePrices(ctx, addonID)
	if err != nil {
		return nil, err
	}

	// targetSeq is MAX over all published usage prices, but the create pass only
	// materializes non-expired ones. Stamping to targetSeq is still safe: a
	// future-start price is created here (with its future start), and an expired
	// price needs no line item (the terminate pass ends any existing one), so every
	// price up to targetSeq is created, already present, overridden, or ended before
	// an association is stamped past it.

	addonResp := &dto.AddonResponse{Addon: addonModel}

	lineItemsFound := 0
	lineItemsCreated := 0
	lineItemsTerminated := 0
	associationsProcessed := 0

	// Page through behind associations doing create -> terminate -> stamp. Stamping
	// is the cursor: stamped associations fall out of the next discovery.
	for {
		refs, listErr := s.AddonPriceSyncRepo.ListBehindAssociations(ctx, addonpricesync.ListBehindAssociationsParams{
			AddonID:   addonID,
			TargetSeq: targetSeq,
			Limit:     4000,
		})
		if listErr != nil {
			return nil, listErr
		}
		if len(refs) == 0 {
			break
		}

		associationIDs := lo.Map(refs, func(r addonpricesync.AddonAssociationRef, _ int) string { return r.AssociationID })

		found, created, createErr := s.createAddonSyncLineItems(ctx, addonResp, activeUsagePrices, refs)
		if createErr != nil {
			return nil, createErr
		}
		lineItemsFound += found
		lineItemsCreated += created

		terminated, termErr := s.AddonPriceSyncRepo.TerminateEndedAddonLineItems(ctx, addonpricesync.TerminateEndedAddonLineItemsParams{
			AddonID:        addonID,
			AssociationIDs: associationIDs,
		})
		if termErr != nil {
			return nil, termErr
		}
		lineItemsTerminated += terminated

		stamped, stampErr := s.AddonPriceSyncRepo.StampAssociations(ctx, addonpricesync.StampAssociationsParams{
			TargetSeq:      targetSeq,
			AssociationIDs: associationIDs,
		})
		if stampErr != nil {
			return nil, stampErr
		}
		associationsProcessed += stamped
	}

	s.Logger.Info(ctx, "completed addon usage price synchronization",
		"addon_id", addonID,
		"target_sequence", targetSeq,
		"line_items_created", lineItemsCreated,
		"line_items_terminated", lineItemsTerminated,
		"associations_processed", associationsProcessed,
		"total_duration_ms", time.Since(syncStart).Milliseconds())

	return &dto.SyncAddonPricesResponse{
		AddonID: addonID,
		Message: "Addon usage prices synchronized to subscription line items successfully",
		Summary: dto.SyncAddonPricesSummary{
			LineItemsFoundForCreation: lineItemsFound,
			LineItemsCreated:          lineItemsCreated,
			LineItemsTerminated:       lineItemsTerminated,
		},
	}, nil
}

// addonPriceSyncStatus reports how many of the addon's active recurring
// associations are still behind the addon's current usage-price sequence, so the
// frontend can disable the sync trigger once every association is caught up.
func (s *addonService) addonPriceSyncStatus(ctx context.Context, addonID string) (*dto.AddonPriceSyncStatusResponse, error) {
	targetSeq, err := s.AddonPriceSyncRepo.CurrentAddonSequence(ctx, addonID)
	if err != nil {
		return nil, err
	}

	if targetSeq == 0 {
		return &dto.AddonPriceSyncStatusResponse{Synced: true}, nil
	}

	unsyncedCount, err := s.AddonPriceSyncRepo.CountUnsyncedAssociations(ctx, addonID, targetSeq)
	if err != nil {
		return nil, err
	}

	return &dto.AddonPriceSyncStatusResponse{
		CurrentSequence:          targetSeq,
		UnsyncedAssociationCount: unsyncedCount,
		Synced:                   unsyncedCount == 0,
	}, nil
}

// activeAddonUsagePrices returns the addon's published usage prices that are not
// yet ended — the candidates for the create pass. Future-start prices are included
// (their line item is created with a future start); only prices whose end_date is
// already in the past are excluded.
func (s *addonService) activeAddonUsagePrices(ctx context.Context, addonID string) ([]*domainPrice.Price, error) {
	filter := types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{addonID}).
		WithEntityType(types.PRICE_ENTITY_TYPE_ADDON).
		WithStatus(types.StatusPublished).
		WithAllowExpiredPrices(true)

	prices, err := s.PriceRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	now := types.Now()
	return lo.Filter(prices, func(p *domainPrice.Price, _ int) bool {
		if p.Type != types.PRICE_TYPE_USAGE {
			return false
		}
		return p.EndDate == nil || p.EndDate.After(now)
	}), nil
}

// addonLineItemKey identifies an existing addon line item by its association and price id.
type addonLineItemKey struct {
	associationID string
	priceID       string
}

// addonOverrideKey identifies a per-subscription override by subscription id and the
// overridden charge's lineage root.
type addonOverrideKey struct {
	subscriptionID string
	rootPriceID    string
}

// createAddonSyncLineItems creates missing usage line items for a page of behind
// associations. A charge is skipped when the association already has a line item
// for it, or when the subscription overrode the charge (subscription-scoped price
// tracing to the charge's lineage root).
func (s *addonService) createAddonSyncLineItems(
	ctx context.Context,
	addonResp *dto.AddonResponse,
	activeUsagePrices []*domainPrice.Price,
	refs []addonpricesync.AddonAssociationRef,
) (found int, created int, err error) {
	if len(activeUsagePrices) == 0 {
		return 0, 0, nil
	}

	associationIDs := lo.Map(refs, func(r addonpricesync.AddonAssociationRef, _ int) string { return r.AssociationID })
	subscriptionIDs := lo.Uniq(lo.Map(refs, func(r addonpricesync.AddonAssociationRef, _ int) string { return r.SubscriptionID }))

	// Must mirror the discovery filter: SubRepo.List defaults to active-only when
	// subscription_status is unset, which would drop trialing subs.
	subFilter := types.NewNoLimitSubscriptionFilter()
	subFilter.SubscriptionIDs = subscriptionIDs
	subFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}
	subs, err := s.SubRepo.List(ctx, subFilter)
	if err != nil {
		return 0, 0, err
	}
	subMap := lo.KeyBy(subs, func(sub *subscription.Subscription) string { return sub.ID })

	// Existing addon line items for the page's associations -> (association, price) set.
	liFilter := types.NewNoLimitSubscriptionLineItemFilter()
	liFilter.AddonAssociationIDs = associationIDs
	liFilter.EntityType = lo.ToPtr(types.SubscriptionLineItemEntityTypeAddon)
	// Match the plan sync exists guard (status = 'published'); the no-limit filter
	// leaves status unset, which the repo reads as "all statuses".
	liFilter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	existingItems, err := s.SubscriptionLineItemRepo.List(ctx, liFilter)
	if err != nil {
		return 0, 0, err
	}
	existing := make(map[addonLineItemKey]struct{}, len(existingItems))
	for _, li := range existingItems {
		if li.AddonAssociationID == nil {
			continue
		}
		existing[addonLineItemKey{associationID: *li.AddonAssociationID, priceID: li.PriceID}] = struct{}{}
	}

	// Subscription-scoped override prices for the page's subs -> (subscription, root) set.
	overrideFilter := types.NewNoLimitPriceFilter().
		WithEntityIDs(subscriptionIDs).
		WithEntityType(types.PRICE_ENTITY_TYPE_SUBSCRIPTION).
		WithStatus(types.StatusPublished).
		WithAllowExpiredPrices(true)
	overridePrices, err := s.PriceRepo.List(ctx, overrideFilter)
	if err != nil {
		return 0, 0, err
	}
	overridden := make(map[addonOverrideKey]struct{}, len(overridePrices))
	for _, op := range overridePrices {
		if op.ParentPriceID == "" {
			continue
		}
		overridden[addonOverrideKey{subscriptionID: op.EntityID, rootPriceID: op.ParentPriceID}] = struct{}{}
	}

	usageResponses := lo.Map(activeUsagePrices, func(p *domainPrice.Price, _ int) *dto.PriceResponse {
		return &dto.PriceResponse{Price: p}
	})

	commitmentsOn, err := NewCommitmentService(s.ServiceParams).IsEnabled(ctx)
	if err != nil {
		return 0, 0, err
	}

	var toCreate []*subscription.SubscriptionLineItem
	for _, ref := range refs {
		sub, ok := subMap[ref.SubscriptionID]
		if !ok {
			continue
		}
		for _, pr := range filterAddonPricesForSubscription(usageResponses, sub) {
			price := pr.Price
			if _, exists := existing[addonLineItemKey{associationID: ref.AssociationID, priceID: price.ID}]; exists {
				continue
			}
			if _, isOverridden := overridden[addonOverrideKey{subscriptionID: ref.SubscriptionID, rootPriceID: price.GetRootPriceID()}]; isOverridden {
				continue
			}
			lineItem := s.buildAddonSyncLineItem(ctx, sub, price, addonResp, ref.AssociationID, ref.EndDate)
			// A usage price joining a subscription can break an active commitment's scope rules (meter, bucket size, overage);
			// with commitments on it is checked and inserted under the commitments lock, and a violating price is logged and skipped.
			if commitmentsOn && price.Type == types.PRICE_TYPE_USAGE {
				found++
				ok, err := createSyncLineItem(ctx, s.ServiceParams, sub, price, lineItem)
				if err != nil {
					return found, created, err
				}

				if ok {
					created++
				}
				continue
			}
			toCreate = append(toCreate, lineItem)
		}
	}

	found += len(toCreate)
	if len(toCreate) == 0 {
		return found, created, nil
	}

	const bulkInsertBatchSize = 1500
	for i := 0; i < len(toCreate); i += bulkInsertBatchSize {
		end := min(i+bulkInsertBatchSize, len(toCreate))
		if err := s.SubscriptionLineItemRepo.CreateBulk(ctx, toCreate[i:end]); err != nil {
			s.Logger.Error(ctx, "failed to create addon line items in bulk batch",
				"addon_id", addonResp.ID,
				"batch_start", i,
				"batch_end", end,
				"error", err)
			return found, created, err
		}
		created += end - i
	}
	return found, created, nil
}

// buildAddonSyncLineItem builds an addon usage line item for the association,
// reusing the shared line-item builder and stamping the association id.
func (s *addonService) buildAddonSyncLineItem(
	ctx context.Context,
	sub *subscription.Subscription,
	price *domainPrice.Price,
	addonResp *dto.AddonResponse,
	associationID string,
	associationEndDate *time.Time,
) *subscription.SubscriptionLineItem {
	metadata := make(map[string]string, len(price.Metadata)+1)
	for k, v := range price.Metadata {
		metadata[k] = v
	}
	metadata["added_by"] = "addon_sync_api"

	// Cap the line item at the association's end date (set when the addon is cancelled
	// at period end) so a synced line never bills past the association.
	endDate := price.EndDate
	if associationEndDate != nil && (endDate == nil || associationEndDate.Before(*endDate)) {
		endDate = associationEndDate
	}

	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:     price.ID,
		Quantity:    decimal.Zero,
		Metadata:    metadata,
		DisplayName: price.DisplayName,
		StartDate:   price.StartDate,
		EndDate:     endDate,
	}

	lineItem := req.ToSubscriptionLineItem(ctx, dto.LineItemParams{
		Subscription: &dto.SubscriptionResponse{Subscription: sub},
		Price:        &dto.PriceResponse{Price: price},
		Addon:        addonResp,
		EntityType:   types.SubscriptionLineItemEntityTypeAddon,
	})
	lineItem.AddonAssociationID = lo.ToPtr(associationID)
	return lineItem
}
