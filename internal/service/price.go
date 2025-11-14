package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/priceunit"
	ierr "github.com/flexprice/flexprice/internal/errors"
	temporalService "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type PriceService interface {
	CreatePrice(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error)
	CreateBulkPrice(ctx context.Context, req dto.CreateBulkPriceRequest) (*dto.CreateBulkPriceResponse, error)
	GetPrice(ctx context.Context, id string) (*dto.PriceResponse, error)
	GetPricesByPlanID(ctx context.Context, req dto.GetPricesByPlanRequest) (*dto.ListPricesResponse, error)
	GetPricesBySubscriptionID(ctx context.Context, subscriptionID string) (*dto.ListPricesResponse, error)
	GetPricesByAddonID(ctx context.Context, addonID string) (*dto.ListPricesResponse, error)
	GetPricesByCostsheetID(ctx context.Context, costsheetID string) (*dto.ListPricesResponse, error)
	GetPrices(ctx context.Context, filter *types.PriceFilter) (*dto.ListPricesResponse, error)
	UpdatePrice(ctx context.Context, id string, req dto.UpdatePriceRequest) (*dto.PriceResponse, error)
	DeletePrice(ctx context.Context, id string, req dto.DeletePriceRequest) error
	CalculateCost(ctx context.Context, price *price.Price, quantity decimal.Decimal) decimal.Decimal

	// CalculateBucketedCost calculates cost for bucketed max values where each value represents max in its time bucket
	CalculateBucketedCost(ctx context.Context, price *price.Price, bucketedValues []decimal.Decimal) decimal.Decimal

	// CalculateCostWithBreakup calculates the cost for a given price and quantity
	// and returns detailed information about the calculation
	CalculateCostWithBreakup(ctx context.Context, price *price.Price, quantity decimal.Decimal, round bool) dto.CostBreakup

	// CalculateCostSheetPrice calculates the cost for a given price and quantity
	// specifically for costsheet calculations
	CalculateCostSheetPrice(ctx context.Context, price *price.Price, quantity decimal.Decimal) decimal.Decimal
}

type priceService struct {
	ServiceParams
}

func NewPriceService(params ServiceParams) PriceService {
	return &priceService{ServiceParams: params}
}

func (s *priceService) CreatePrice(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Validate that the entity exists based on entity type
	if !req.SkipEntityValidation {
		if err := s.validateEntityExists(ctx, req.EntityType, req.EntityID); err != nil {
			return nil, err
		}
	}

	// Get display name if needed (before price creation)
	s.getDisplayName(ctx, &req)

	// Convert request to Price domain object
	p, err := req.ToPrice(ctx)
	if err != nil {
		return nil, err
	}

	// Apply price unit conversion if price type is CUSTOM
	// This converts amounts/tiers and updates the Price object
	if p.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM {
		if err := s.applyPriceUnitConversionToPrice(ctx, p, req); err != nil {
			return nil, err
		}
	}

	// Validate group if provided
	if req.GroupID != "" {
		if err := s.validateGroup(ctx, []*price.Price{p}); err != nil {
			return nil, err
		}
	}

	if err := s.PriceRepo.Create(ctx, p); err != nil {
		return nil, err
	}

	response := &dto.PriceResponse{Price: p}

	// Sync new price to Chargebee if it belongs to a plan
	if p.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
		s.syncPriceToChargebeeIfEnabled(ctx, p.ID, p.EntityID)
	}

	// Sync new price to QuickBooks if it belongs to a plan
	if p.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
		s.syncPriceToQuickBooksIfEnabled(ctx, p.ID, p.EntityID)
	}
	return response, nil
}

// getDisplayName extracts the display name from the entity (plan/addon) or meter
// if SkipEntityValidation is false and DisplayName is empty.
// If SkipEntityValidation is true, it sets default names: "Recurring" for FIXED and "Usage" for USAGE.
// This should be called before price creation repository operations.
func (s *priceService) getDisplayName(ctx context.Context, req *dto.CreatePriceRequest) {
	// If display name is already set, don't override it
	if req.DisplayName != "" {
		return
	}

	// If validation is skipped, use default names
	if req.SkipEntityValidation {
		switch req.Type {
		case types.PRICE_TYPE_FIXED:
			req.DisplayName = "Recurring"
		case types.PRICE_TYPE_USAGE:
			req.DisplayName = "Usage"
		}
		return
	}

	// Extract from entity when validation is enabled
	switch req.Type {
	case types.PRICE_TYPE_FIXED:
		// For FIXED prices, extract plan/addon name
		if req.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
			plan, err := s.PlanRepo.Get(ctx, req.EntityID)
			if err == nil && plan != nil {
				req.DisplayName = plan.Name
			}
		} else if req.EntityType == types.PRICE_ENTITY_TYPE_ADDON {
			addon, err := s.AddonRepo.GetByID(ctx, req.EntityID)
			if err == nil && addon != nil {
				req.DisplayName = addon.Name
			}
		}
	case types.PRICE_TYPE_USAGE:
		// For USAGE prices, extract meter name
		if req.MeterID != "" {
			meterService := NewMeterService(s.MeterRepo)
			meter, err := meterService.GetMeter(ctx, req.MeterID)
			if err == nil && meter != nil {
				req.DisplayName = meter.Name
			}
		}
	}
}

// validateEntityExists validates that the entity exists based on the entity type
func (s *priceService) validateEntityExists(ctx context.Context, entityType types.PriceEntityType, entityID string) error {
	switch entityType {
	case types.PRICE_ENTITY_TYPE_PLAN:
		plan, err := s.PlanRepo.Get(ctx, entityID)
		if err != nil || plan == nil {
			return ierr.NewError("plan not found").
				WithHint("The specified plan does not exist").
				WithReportableDetails(map[string]interface{}{
					"plan_id": entityID,
				}).
				Mark(ierr.ErrNotFound)
		}
	case types.PRICE_ENTITY_TYPE_ADDON:
		addon, err := s.AddonRepo.GetByID(ctx, entityID)
		if err != nil || addon == nil {
			return ierr.NewError("addon not found").
				WithHint("The specified addon does not exist").
				WithReportableDetails(map[string]interface{}{
					"addon_id": entityID,
				}).
				Mark(ierr.ErrNotFound)
		}
	case types.PRICE_ENTITY_TYPE_SUBSCRIPTION:
		subscription, err := s.SubRepo.Get(ctx, entityID)
		if err != nil || subscription == nil {
			return ierr.NewError("subscription not found").
				WithHint("The specified subscription does not exist").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": entityID,
				}).
				Mark(ierr.ErrNotFound)
		}
	case types.PRICE_ENTITY_TYPE_COSTSHEET:
		costsheet, err := s.CostSheetRepo.GetByID(ctx, entityID)
		if err != nil || costsheet == nil {
			return ierr.NewError("costsheet not found").
				WithHint("The specified costsheet  does not exist").
				WithReportableDetails(map[string]interface{}{
					"costsheet_id": entityID,
				}).
				Mark(ierr.ErrNotFound)
		}
	default:
		return ierr.NewError("unsupported entity type").
			WithHint("The specified entity type is not supported").
			WithReportableDetails(map[string]interface{}{
				"entity_type": entityType,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (s *priceService) CreateBulkPrice(ctx context.Context, req dto.CreateBulkPriceRequest) (*dto.CreateBulkPriceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var response *dto.CreateBulkPriceResponse

	// Use transaction to ensure all prices are created or none
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		response = &dto.CreateBulkPriceResponse{
			Items: make([]*dto.PriceResponse, 0),
		}

		// Process all prices
		var regularPrices []*price.Price

		for _, priceReq := range req.Items {
			// Get display name if needed (before price creation)
			s.getDisplayName(txCtx, &priceReq)
			// Convert request to Price domain object
			p, err := priceReq.ToPrice(txCtx)
			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to create price").
					Mark(ierr.ErrValidation)
			}

			// Apply price unit conversion if price type is CUSTOM
			if p.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM {
				if err := s.applyPriceUnitConversionToPrice(txCtx, p, priceReq); err != nil {
					return err
				}
			}

			regularPrices = append(regularPrices, p)
		}

		// Create regular prices in bulk if any exist
		if len(regularPrices) > 0 {
			// Validate groups if provided
			if err := s.validateGroup(txCtx, regularPrices); err != nil {
				return err
			}
			if err := s.PriceRepo.CreateBulk(txCtx, regularPrices); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to create prices in bulk").
					Mark(ierr.ErrDatabase)
			}

			// Add successful regular prices to response
			for _, p := range regularPrices {
				priceResp := &dto.PriceResponse{Price: p}
				response.Items = append(response.Items, priceResp)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sync prices to integrations if available and prices are for a plan
	s.Logger.Debugw("Bulk price sync check",
		"response_nil", response == nil,
		"response_items_count", func() int {
			if response == nil {
				return -1
			}
			return len(response.Items)
		}())

	if response != nil && len(response.Items) > 0 {
		// Use background context for sync operations to prevent request context cancellation
		// from affecting the sync process. Each sync is independent and should complete
		// regardless of whether the HTTP request times out.
		// However, we need to preserve tenant/environment context values for database queries.
		syncCtx := context.Background()
		syncCtx = context.WithValue(syncCtx, types.CtxTenantID, types.GetTenantID(ctx))
		syncCtx = context.WithValue(syncCtx, types.CtxUserID, types.GetUserID(ctx))
		syncCtx = context.WithValue(syncCtx, types.CtxEnvironmentID, types.GetEnvironmentID(ctx))
		if roles := ctx.Value(types.CtxRoles); roles != nil {
			syncCtx = context.WithValue(syncCtx, types.CtxRoles, roles)
		}

		for _, priceResp := range response.Items {
			if priceResp.Price.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
				// Sync to Chargebee with background context
				s.syncPriceToChargebeeIfEnabled(syncCtx, priceResp.Price.ID, priceResp.Price.EntityID)

				// Sync to QuickBooks via Temporal workflow (async, non-blocking)
				s.syncPriceToQuickBooksIfEnabled(syncCtx, priceResp.Price.ID, priceResp.Price.EntityID)
			}
		}
	}

	return response, nil
}

func (s *priceService) GetPrice(ctx context.Context, id string) (*dto.PriceResponse, error) {
	if id == "" {
		return nil, ierr.NewError("price_id is required").
			WithHint("Price ID is required").
			Mark(ierr.ErrValidation)
	}

	price, err := s.PriceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	response := &dto.PriceResponse{Price: price}

	// Set entity information
	response.EntityType = price.EntityType
	response.EntityID = price.EntityID

	if price.MeterID != "" {
		meterService := NewMeterService(s.MeterRepo)
		meter, err := meterService.GetMeter(ctx, price.MeterID)
		if err != nil {
			s.Logger.Warnw("failed to fetch meter", "meter_id", price.MeterID, "error", err)
			return nil, err
		}
		response.Meter = dto.ToMeterResponse(meter)
	}

	if price.GroupID != "" {
		groupService := NewGroupService(s.ServiceParams)
		group, err := groupService.GetGroup(ctx, price.GroupID)
		if err != nil {
			s.Logger.Warnw("failed to fetch group", "group_id", price.GroupID, "error", err)
			// Don't fail the request if group fetch fails, just continue
		} else {
			response.Group = group
		}
	}

	return response, nil
}

func (s *priceService) GetPricesByPlanID(ctx context.Context, req dto.GetPricesByPlanRequest) (*dto.ListPricesResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	priceFilter := types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{req.PlanID}).
		WithStatus(types.StatusPublished).
		WithEntityType(types.PRICE_ENTITY_TYPE_PLAN).
		WithAllowExpiredPrices(req.AllowExpired).
		WithExpand(string(types.ExpandMeters) + "," + string(types.ExpandGroups))

	response, err := s.GetPrices(ctx, priceFilter)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// GetPricesBySubscriptionID fetches subscription-scoped prices for a specific subscription
func (s *priceService) GetPricesBySubscriptionID(ctx context.Context, subscriptionID string) (*dto.ListPricesResponse, error) {
	if subscriptionID == "" {
		return nil, ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}

	// Use unlimited filter to fetch subscription-scoped prices only
	priceFilter := types.NewNoLimitPriceFilter().
		WithSubscriptionID(subscriptionID).
		WithEntityType(types.PRICE_ENTITY_TYPE_SUBSCRIPTION).
		WithStatus(types.StatusPublished).
		WithExpand(string(types.ExpandMeters))

	response, err := s.GetPrices(ctx, priceFilter)
	if err != nil {
		return nil, err
	}

	return response, nil
}

func (s *priceService) GetPricesByAddonID(ctx context.Context, addonID string) (*dto.ListPricesResponse, error) {

	if addonID == "" {
		return nil, ierr.NewError("addon_id is required").
			WithHint("Addon ID is required").
			Mark(ierr.ErrValidation)
	}

	priceFilter := types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{addonID}).
		WithEntityType(types.PRICE_ENTITY_TYPE_ADDON).
		WithStatus(types.StatusPublished).
		WithExpand(string(types.ExpandMeters))

	response, err := s.GetPrices(ctx, priceFilter)
	if err != nil {
		return nil, err
	}

	return response, nil
}

func (s *priceService) GetPricesByCostsheetID(ctx context.Context, costsheetID string) (*dto.ListPricesResponse, error) {
	if costsheetID == "" {
		return nil, ierr.NewError("costsheet v2 id is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	priceFilter := types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{costsheetID}).
		WithStatus(types.StatusPublished).
		WithEntityType(types.PRICE_ENTITY_TYPE_COSTSHEET).
		WithExpand(string(types.ExpandMeters))

	response, err := s.GetPrices(ctx, priceFilter)
	if err != nil {
		return nil, err
	}

	return response, nil
}

func (s *priceService) GetPrices(ctx context.Context, filter *types.PriceFilter) (*dto.ListPricesResponse, error) {
	meterService := NewMeterService(s.MeterRepo)

	// Validate expand fields
	if err := filter.GetExpand().Validate(types.PriceExpandConfig); err != nil {
		return nil, err
	}

	// Get prices
	prices, err := s.PriceRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	priceCount, err := s.PriceRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Build response
	response := &dto.ListPricesResponse{
		Items: make([]*dto.PriceResponse, len(prices)),
		Pagination: types.NewPaginationResponse(
			priceCount,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}

	if len(prices) == 0 {
		return response, nil
	}

	// If meters are requested to be expanded, fetch all meters in one query
	var metersByID map[string]*dto.MeterResponse
	if filter.GetExpand().Has(types.ExpandMeters) && len(prices) > 0 {
		// Fetch all meters in one query
		metersResponse, err := meterService.GetAllMeters(ctx)
		if err != nil {
			return nil, err
		}

		// Create a map for quick meter lookup
		metersByID = make(map[string]*dto.MeterResponse, len(metersResponse.Items))
		for _, m := range metersResponse.Items {
			metersByID[m.ID] = m
		}

		s.Logger.Debugw("fetched meters for prices", "count", len(metersResponse.Items))
	}

	// Collect entity IDs based on entity type for efficient bulk fetching
	var planIDs []string
	var addonIDs []string
	var groupIDs []string

	// Separate prices by entity type to collect IDs
	for _, p := range prices {
		if p.EntityType == types.PRICE_ENTITY_TYPE_PLAN && p.EntityID != "" {
			planIDs = append(planIDs, p.EntityID)
		} else if p.EntityType == types.PRICE_ENTITY_TYPE_ADDON && p.EntityID != "" {
			addonIDs = append(addonIDs, p.EntityID)
		}
		if p.GroupID != "" {
			groupIDs = append(groupIDs, p.GroupID)
		}
	}

	// If plans are requested to be expanded, fetch plans in bulk
	var plansByID map[string]*dto.PlanResponse
	planService := NewPlanService(s.ServiceParams)
	if filter.GetExpand().Has(types.ExpandPlan) && len(planIDs) > 0 {
		// Remove duplicates
		planIDs = lo.Uniq(planIDs)

		// Fetch plans in bulk
		planFilter := types.NewNoLimitPlanFilter()
		planFilter.PlanIDs = planIDs

		plansResponse, err := planService.GetPlans(ctx, planFilter)
		if err != nil {
			return nil, err
		}

		// Create a map for plan lookup
		plansByID = make(map[string]*dto.PlanResponse, len(plansResponse.Items))
		for _, p := range plansResponse.Items {
			plansByID[p.Plan.ID] = p
		}
	}

	// If addons are requested to be expanded, fetch addons in bulk
	var addonsByID map[string]*dto.AddonResponse
	addonService := NewAddonService(s.ServiceParams)
	if filter.GetExpand().Has(types.ExpandAddons) && len(addonIDs) > 0 {
		// Remove duplicates
		addonIDs = lo.Uniq(addonIDs)

		// Fetch addons in bulk
		addonFilter := types.NewNoLimitAddonFilter()
		addonFilter.AddonIDs = addonIDs
		addonsResponse, err := addonService.GetAddons(ctx, addonFilter)
		if err != nil {
			return nil, err
		}

		// Create a map for addon lookup
		addonsByID = make(map[string]*dto.AddonResponse, len(addonsResponse.Items))
		for _, a := range addonsResponse.Items {
			addonsByID[a.Addon.ID] = a
		}
	}

	// If groups are requested to be expanded, fetch groups in bulk
	var groupsByID map[string]*dto.GroupResponse
	if filter.GetExpand().Has(types.ExpandGroups) && len(groupIDs) > 0 {
		// Remove duplicates
		groupIDs = lo.Uniq(groupIDs)

		groupService := NewGroupService(s.ServiceParams)
		groupFilter := &types.GroupFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
			GroupIDs:    groupIDs,
		}

		groupsResponse, err := groupService.ListGroups(ctx, groupFilter)
		if err != nil {
			s.Logger.Warnw("failed to fetch groups in bulk", "error", err)
			// Don't fail the request, just continue without groups
			groupsByID = make(map[string]*dto.GroupResponse)
		} else {
			// Create a map for group lookup
			groupsByID = make(map[string]*dto.GroupResponse, len(groupsResponse.Items))
			for _, g := range groupsResponse.Items {
				groupsByID[g.ID] = g
			}
		}
	}

	// Build response with expanded fields
	for i, p := range prices {
		response.Items[i] = &dto.PriceResponse{Price: p}

		// Add meter if requested and available
		if filter.GetExpand().Has(types.ExpandMeters) && p.MeterID != "" {
			if m, ok := metersByID[p.MeterID]; ok {
				response.Items[i].Meter = m
			}
		}

		// Add plan if requested and available
		if filter.GetExpand().Has(types.ExpandPlan) && p.EntityType == types.PRICE_ENTITY_TYPE_PLAN && p.EntityID != "" {
			if plan, ok := plansByID[p.EntityID]; ok {
				response.Items[i].Plan = plan
			}
		}

		// Add addon if requested and available
		if filter.GetExpand().Has(types.ExpandAddons) && p.EntityType == types.PRICE_ENTITY_TYPE_ADDON && p.EntityID != "" {
			if addon, ok := addonsByID[p.EntityID]; ok {
				response.Items[i].Addon = addon
			}
		}

		// Add group if requested and available
		if filter.GetExpand().Has(types.ExpandGroups) && p.GroupID != "" {
			if group, ok := groupsByID[p.GroupID]; ok {
				response.Items[i].Group = group
			}
		}
	}

	return response, nil
}

func (s *priceService) UpdatePrice(ctx context.Context, id string, req dto.UpdatePriceRequest) (*dto.PriceResponse, error) {
	// Validate the request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the existing price
	existingPrice, err := s.PriceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Check if the request has critical fields
	if req.ShouldCreateNewPrice() {
		if existingPrice.EndDate != nil {
			return nil, ierr.NewError("price is already terminated").
				WithHint("Cannot update a terminated price").
				WithReportableDetails(map[string]interface{}{
					"price_id": id,
				}).
				Mark(ierr.ErrValidation)
		}

		var newPriceResp *dto.PriceResponse

		// Set termination end date - use EndDate from request if provided, otherwise use current time
		terminationEndDate := time.Now().UTC()
		if req.EffectiveFrom != nil {
			terminationEndDate = *req.EffectiveFrom
		}

		if err := s.DB.WithTx(ctx, func(ctx context.Context) error {
			// Terminate the existing price
			existingPrice.EndDate = &terminationEndDate

			// Validate group if provided
			if req.GroupID != "" {
				existingPrice.GroupID = req.GroupID
				if err := s.validateGroup(ctx, []*price.Price{existingPrice}); err != nil {
					return err
				}
			}

			if err := s.PriceRepo.Update(ctx, existingPrice); err != nil {
				return err
			}

			// Convert update request to create request - this handles all the field mapping
			createReq := req.ToCreatePriceRequest(existingPrice)

			// Set start date for new price to be exactly when the old price ends
			createReq.StartDate = &terminationEndDate

			// Create the new price - this will use all existing validation logic
			newPriceResp, err = s.CreatePrice(ctx, createReq)
			return err

		}); err != nil {
			return nil, err
		}

		s.Logger.Infow("price updated with termination and recreation",
			"old_price_id", existingPrice.ID,
			"new_price_id", newPriceResp.ID,
			"termination_end_date", terminationEndDate,
			"new_price_start_date", terminationEndDate,
			"entity_type", existingPrice.EntityType,
			"entity_id", existingPrice.EntityID)

		// Sync new price to Chargebee if it belongs to a plan
		if newPriceResp.Price.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
			s.syncPriceToChargebeeIfEnabled(ctx, newPriceResp.Price.ID, newPriceResp.Price.EntityID)
		}

		// Sync new price to QuickBooks if it belongs to a plan
		if newPriceResp.Price.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
			s.syncPriceToQuickBooksIfEnabled(ctx, newPriceResp.Price.ID, newPriceResp.Price.EntityID)
		}

		return newPriceResp, nil
	} else {
		// No critical fields - simple update

		// Update non-critical fields
		if req.LookupKey != "" {
			existingPrice.LookupKey = req.LookupKey
		}
		if req.Description != "" {
			existingPrice.Description = req.Description
		}
		if req.Metadata != nil {
			existingPrice.Metadata = req.Metadata
		}
		if req.EffectiveFrom != nil {
			existingPrice.EndDate = req.EffectiveFrom
		}

		if req.GroupID != "" {
			existingPrice.GroupID = req.GroupID
			if err := s.validateGroup(ctx, []*price.Price{existingPrice}); err != nil {
				return nil, err
			}
		}

		// Update the price in database
		if err := s.PriceRepo.Update(ctx, existingPrice); err != nil {
			return nil, err
		}

		response := &dto.PriceResponse{Price: existingPrice}

		return response, nil
	}
}

func (s *priceService) DeletePrice(ctx context.Context, id string, req dto.DeletePriceRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}

	price, err := s.PriceRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Check if price is already terminated
	if price.EndDate != nil {
		return ierr.NewError("price is already terminated").
			WithHint("Cannot terminate a price that has already been terminated").
			WithReportableDetails(map[string]interface{}{
				"price_id": id,
				"end_date": price.EndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	// Set end date and validate
	var endDate time.Time
	if req.EndDate != nil {
		endDate = req.EndDate.UTC()
	} else {
		endDate = time.Now().UTC()
	}

	// Validate end date is after start date
	if price.StartDate != nil && price.StartDate.After(endDate) {
		return ierr.NewError("end date must be after start date").
			WithHint("The termination date must be after the price's start date").
			WithReportableDetails(map[string]interface{}{
				"price_id":   id,
				"start_date": price.StartDate,
				"end_date":   endDate,
			}).
			Mark(ierr.ErrValidation)
	}

	price.EndDate = &endDate

	if err := s.PriceRepo.Update(ctx, price); err != nil {
		return err
	}

	return nil
}

// calculateBucketedMaxCost calculates cost for bucketed max values
// Each value in the array represents max usage in its time bucket
func (s *priceService) calculateBucketedMaxCost(ctx context.Context, price *price.Price, bucketedValues []decimal.Decimal) decimal.Decimal {
	totalCost := decimal.Zero

	// For tiered pricing, handle each bucket's max value according to tier mode
	if price.BillingModel == types.BILLING_MODEL_TIERED {
		// Process each bucket's max value independently through its appropriate tier
		for _, maxValue := range bucketedValues {
			bucketCost := s.calculateTieredCost(ctx, price, maxValue)
			totalCost = totalCost.Add(bucketCost)
		}
	} else {
		// For non-tiered pricing (flat fee, package), process each bucket independently
		for _, maxValue := range bucketedValues {
			bucketCost := s.calculateSingletonCost(ctx, price, maxValue)
			totalCost = totalCost.Add(bucketCost)
		}
	}

	return totalCost.Round(types.GetCurrencyPrecision(price.Currency))
}

// calculateSingletonCost calculates cost for a single value
// This is used both for regular values and individual bucket values
func (s *priceService) calculateSingletonCost(ctx context.Context, price *price.Price, quantity decimal.Decimal) decimal.Decimal {
	cost := decimal.Zero
	if quantity.IsZero() {
		return cost
	}

	switch price.BillingModel {
	case types.BILLING_MODEL_FLAT_FEE:
		cost = price.CalculateAmount(quantity)

	case types.BILLING_MODEL_PACKAGE:
		if price.TransformQuantity.DivideBy <= 0 {
			return decimal.Zero
		}

		// Calculate how many complete packages are needed to cover the quantity
		packagesNeeded := quantity.Div(decimal.NewFromInt(int64(price.TransformQuantity.DivideBy)))

		// Round based on mode
		if price.TransformQuantity.Round == types.ROUND_DOWN {
			packagesNeeded = packagesNeeded.Floor()
		} else {
			// Default to rounding up for packages
			packagesNeeded = packagesNeeded.Ceil()
		}

		// Calculate total cost by multiplying package price by number of packages
		cost = price.CalculateAmount(packagesNeeded)

	case types.BILLING_MODEL_TIERED:
		cost = s.calculateTieredCost(ctx, price, quantity)
	}

	return cost
}

// CalculateCost calculates the cost for a given price and quantity
// returns the cost in main currency units (e.g., 1.00 = $1.00)
func (s *priceService) CalculateCost(ctx context.Context, price *price.Price, quantity decimal.Decimal) decimal.Decimal {
	return s.calculateSingletonCost(ctx, price, quantity)
}

// CalculateBucketedCost calculates cost for bucketed max values where each value represents max in its time bucket
func (s *priceService) CalculateBucketedCost(ctx context.Context, price *price.Price, bucketedValues []decimal.Decimal) decimal.Decimal {
	return s.calculateBucketedMaxCost(ctx, price, bucketedValues)
}

// calculateTieredCost calculates cost for tiered pricing
func (s *priceService) calculateTieredCost(ctx context.Context, price *price.Price, quantity decimal.Decimal) decimal.Decimal {
	cost := decimal.Zero
	if len(price.Tiers) == 0 {
		s.Logger.WithContext(ctx).Errorf("no tiers found for price %s", price.ID)
		return cost
	}

	// Sort price tiers by up_to value
	sort.Slice(price.Tiers, func(i, j int) bool {
		return price.Tiers[i].GetTierUpTo() < price.Tiers[j].GetTierUpTo()
	})

	switch price.TierMode {
	case types.BILLING_TIER_VOLUME:
		selectedTierIndex := len(price.Tiers) - 1
		// Find the tier that the quantity falls into
		// up_to is INCLUSIVE - if up_to is 1000, quantity 1000 belongs to this tier
		// Note: Quantity is already decimal, up_to is converted to decimal for comparison
		// Edge cases: Handles decimal quantities like 1000.5, 1024.75, etc.
		// If quantity > up_to (even by small decimals like 1000.001 > 1000), it goes to next tier
		for i, tier := range price.Tiers {
			if tier.UpTo == nil {
				selectedTierIndex = i
				break
			}
			// Use LessThanOrEqual to make up_to INCLUSIVE
			// Handles decimal quantities: 1000.5 <= 1000.5 (inclusive)
			// Edge case: 1000.001 > 1000, so 1000.001 goes to next tier
			if quantity.LessThanOrEqual(decimal.NewFromUint64(*tier.UpTo)) {
				selectedTierIndex = i
				break
			}
		}

		selectedTier := price.Tiers[selectedTierIndex]

		// Calculate tier cost with full precision and handling of flat amount
		tierCost := selectedTier.CalculateTierAmount(quantity, price.Currency)

		s.Logger.WithContext(ctx).Debugf(
			"volume tier total cost for quantity %s: %s price: %s tier : %+v",
			quantity.String(),
			tierCost.String(),
			price.ID,
			selectedTier,
		)

		cost = cost.Add(tierCost)

	case types.BILLING_TIER_SLAB:
		remainingQuantity := quantity
		tierStartQuantity := decimal.Zero
		for _, tier := range price.Tiers {
			var tierQuantity = remainingQuantity
			if tier.UpTo != nil {
				upTo := decimal.NewFromUint64(*tier.UpTo)
				tierCapacity := upTo.Sub(tierStartQuantity)

				// Use the minimum of remaining quantity and tier capacity
				if remainingQuantity.GreaterThan(tierCapacity) {
					tierQuantity = tierCapacity
				}

				// Update tier start for next iteration
				tierStartQuantity = upTo
			}

			// Calculate tier cost with full precision and handling of flat amount
			tierCost := tier.CalculateTierAmount(tierQuantity, price.Currency)
			cost = cost.Add(tierCost)
			remainingQuantity = remainingQuantity.Sub(tierQuantity)

			s.Logger.WithContext(ctx).Debugf(
				"slab tier total cost for quantity %s: %s price: %s tier : %+v",
				quantity.String(),
				tierCost.String(),
				price.ID,
				tier,
			)

			if remainingQuantity.LessThanOrEqual(decimal.Zero) {
				break
			}
		}
	default:
		s.Logger.WithContext(ctx).Errorf("invalid tier mode: %s", price.TierMode)
		return decimal.Zero
	}

	return cost
}

// CalculateCostWithBreakup calculates the cost with detailed breakdown information
func (s *priceService) CalculateCostWithBreakup(ctx context.Context, price *price.Price, quantity decimal.Decimal, round bool) dto.CostBreakup {
	result := dto.CostBreakup{
		EffectiveUnitCost: decimal.Zero,
		SelectedTierIndex: -1,
		TierUnitAmount:    decimal.Zero,
		FinalCost:         decimal.Zero,
	}

	// Return early for zero quantity, but keep the tier unit amount
	if quantity.IsZero() && price.BillingModel != types.BILLING_MODEL_PACKAGE {
		return result
	}

	switch price.BillingModel {
	case types.BILLING_MODEL_FLAT_FEE:
		result.FinalCost = price.CalculateAmount(quantity)
		result.EffectiveUnitCost = price.Amount
		result.TierUnitAmount = price.Amount

	case types.BILLING_MODEL_PACKAGE:
		if price.TransformQuantity.DivideBy <= 0 {
			return result
		}

		// Calculate the tier unit amount (price per unit in a full package)
		result.TierUnitAmount = price.Amount.Div(decimal.NewFromInt(int64(price.TransformQuantity.DivideBy)))

		// Return early for zero quantity, but keep the tier unit amount we just calculated
		if quantity.IsZero() {
			return result
		}

		// Calculate how many complete packages are needed to cover the quantity
		packagesNeeded := quantity.Div(decimal.NewFromInt(int64(price.TransformQuantity.DivideBy)))

		// Round based on the specified mode
		if price.TransformQuantity.Round == types.ROUND_DOWN {
			packagesNeeded = packagesNeeded.Floor()
		} else {
			// Default to rounding up for packages
			packagesNeeded = packagesNeeded.Ceil()
		}

		// Calculate total cost by multiplying package price by number of packages
		result.FinalCost = price.CalculateAmount(packagesNeeded)

		// Calculate effective unit cost (cost per actual unit used)
		result.EffectiveUnitCost = result.FinalCost.Div(quantity)

		return result

	case types.BILLING_MODEL_TIERED:
		result = s.calculateTieredCostWithBreakup(ctx, price, quantity)
	}

	if round {
		result.FinalCost = result.FinalCost.Round(types.GetCurrencyPrecision(price.Currency))
	}

	return result
}

// calculateTieredCostWithBreakup calculates tiered cost with detailed breakdown
func (s *priceService) calculateTieredCostWithBreakup(ctx context.Context, price *price.Price, quantity decimal.Decimal) dto.CostBreakup {
	result := dto.CostBreakup{
		EffectiveUnitCost: decimal.Zero,
		SelectedTierIndex: -1,
		TierUnitAmount:    decimal.Zero,
		FinalCost:         decimal.Zero,
	}

	if len(price.Tiers) == 0 {
		s.Logger.WithContext(ctx).Errorf("no tiers found for price %s", price.ID)
		return result
	}

	// Sort price tiers by up_to value
	sort.Slice(price.Tiers, func(i, j int) bool {
		return price.Tiers[i].GetTierUpTo() < price.Tiers[j].GetTierUpTo()
	})

	switch price.TierMode {
	case types.BILLING_TIER_VOLUME:
		selectedTierIndex := len(price.Tiers) - 1
		// Find the tier that the quantity falls into
		// up_to is INCLUSIVE - if up_to is 1000, quantity 1000 belongs to this tier
		for i, tier := range price.Tiers {
			if tier.UpTo == nil {
				selectedTierIndex = i
				break
			}
			// Use LessThanOrEqual to make up_to INCLUSIVE
			// Handles decimal quantities: 1000.5 <= 1000.5 (inclusive)
			// Edge case: 1000.001 > 1000, so 1000.001 goes to next tier
			if quantity.LessThanOrEqual(decimal.NewFromUint64(*tier.UpTo)) {
				selectedTierIndex = i
				break
			}
		}

		selectedTier := price.Tiers[selectedTierIndex]
		result.SelectedTierIndex = selectedTierIndex
		result.TierUnitAmount = selectedTier.UnitAmount

		// Calculate tier cost with full precision and handling of flat amount
		result.FinalCost = selectedTier.CalculateTierAmount(quantity, price.Currency)

		// Calculate effective unit cost (handle zero quantity case)
		if !quantity.IsZero() {
			result.EffectiveUnitCost = result.FinalCost.Div(quantity)
		} else {
			result.EffectiveUnitCost = decimal.Zero
		}

		s.Logger.WithContext(ctx).Debugf(
			"volume tier total cost for quantity %s: %s price: %s tier : %+v",
			quantity.String(),
			result.FinalCost.String(),
			price.ID,
			selectedTier,
		)

	case types.BILLING_TIER_SLAB:
		remainingQuantity := quantity
		tierStartQuantity := decimal.Zero
		for i, tier := range price.Tiers {
			var tierQuantity = remainingQuantity
			if tier.UpTo != nil {
				upTo := decimal.NewFromUint64(*tier.UpTo)
				tierCapacity := upTo.Sub(tierStartQuantity)

				// Use the minimum of remaining quantity and tier capacity
				if remainingQuantity.GreaterThan(tierCapacity) {
					tierQuantity = tierCapacity
				}

				// Update tier start for next iteration
				tierStartQuantity = upTo
			}

			// Calculate tier cost with full precision and handling of flat amount
			tierCost := tier.CalculateTierAmount(tierQuantity, price.Currency)
			result.FinalCost = result.FinalCost.Add(tierCost)

			// Record the last tier used (will be the highest tier the quantity applies to)
			if tierQuantity.GreaterThan(decimal.Zero) {
				result.SelectedTierIndex = i
				result.TierUnitAmount = tier.UnitAmount
			}

			remainingQuantity = remainingQuantity.Sub(tierQuantity)

			s.Logger.WithContext(ctx).Debugf(
				"slab tier total cost for quantity %s: %s price: %s tier : %+v",
				quantity.String(),
				tierCost.String(),
				price.ID,
				tier,
			)

			if remainingQuantity.LessThanOrEqual(decimal.Zero) {
				break
			}
		}

		// Calculate effective unit cost (handle zero quantity case)
		if !quantity.IsZero() {
			result.EffectiveUnitCost = result.FinalCost.Div(quantity)
		} else {
			result.EffectiveUnitCost = decimal.Zero
		}
	default:
		s.Logger.WithContext(ctx).Errorf("invalid tier mode: %s", price.TierMode)
	}

	return result
}

// CalculateCostSheetPrice calculates the cost for a given price and quantity
// specifically for costsheet calculations. This is similar to CalculateCost
// but may have specific rules for costsheet pricing.
func (s *priceService) CalculateCostSheetPrice(ctx context.Context, price *price.Price, quantity decimal.Decimal) decimal.Decimal {
	// For now, we'll use the same calculation as CalculateCost
	// In the future, we can add costsheet-specific pricing rules here
	return s.CalculateCost(ctx, price, quantity)
}

// validateGroup validates that a group exists and is of type "price"
func (s *priceService) validateGroup(ctx context.Context, prices []*price.Price) error {
	// 1. Get all group IDs from prices
	groupIDs := make([]string, 0)
	for _, price := range prices {
		if price.GroupID == "" {
			continue
		}
		groupIDs = append(groupIDs, price.GroupID)
	}

	groupIDs = lo.Uniq(groupIDs)

	// 2. Validate groups if any
	if len(groupIDs) == 0 {
		return nil
	}

	// 3. Validate group
	groupService := NewGroupService(s.ServiceParams)
	if err := groupService.ValidateGroupBulk(ctx, groupIDs, types.GroupEntityTypePrice); err != nil {
		return err
	}
	return nil
}

// syncPriceToChargebeeIfEnabled syncs a price to Chargebee if the integration is enabled
// This is a non-blocking operation - errors are logged but don't fail the price operation
func (s *priceService) syncPriceToChargebeeIfEnabled(ctx context.Context, priceID, planID string) {
	// Early return if integration factory is not available
	if s.IntegrationFactory == nil {
		return
	}

	// Get Chargebee integration
	chargebeeIntegration, err := s.IntegrationFactory.GetChargebeeIntegration(ctx)
	if err != nil {
		s.Logger.Debugw("Chargebee integration not available, skipping sync",
			"price_id", priceID,
			"plan_id", planID,
			"error", err)
		return
	}

	// Check if Chargebee connection exists
	if !chargebeeIntegration.Client.HasChargebeeConnection(ctx) {
		s.Logger.Debugw("Chargebee connection not configured, skipping sync",
			"price_id", priceID,
			"plan_id", planID)
		return
	}

	// Get plan using repository
	plan, err := s.PlanRepo.Get(ctx, planID)
	if err != nil {
		s.Logger.Errorw("failed to get plan for Chargebee sync",
			"price_id", priceID,
			"plan_id", planID,
			"error", err)
		return
	}

	// Get price using repository
	priceModel, err := s.PriceRepo.Get(ctx, priceID)
	if err != nil {
		s.Logger.Errorw("failed to get price for Chargebee sync",
			"price_id", priceID,
			"plan_id", planID,
			"error", err)
		return
	}

	// Sync to Chargebee (non-blocking - log errors but don't fail)
	if syncErr := chargebeeIntegration.PlanSyncSvc.SyncPlanToChargebee(ctx, plan, []*price.Price{priceModel}); syncErr != nil {
		s.Logger.Errorw("failed to sync price to Chargebee",
			"price_id", priceID,
			"plan_id", planID,
			"error", syncErr)
	} else {
		s.Logger.Infow("successfully synced price to Chargebee",
			"price_id", priceID,
			"plan_id", planID)
	}
}

// applyPriceUnitConversionToPrice applies price unit conversion to an existing Price object
// This method handles:
// 1. Fetching and validating the price unit entity
// 2. Converting price unit amounts to base currency amounts and updating p.Amount
// 3. Converting price unit tiers to base currency tiers and updating p.Tiers
// 4. Setting price unit metadata (PriceUnitID, ConversionRate) on the Price object
func (s *priceService) applyPriceUnitConversionToPrice(ctx context.Context, p *price.Price, req dto.CreatePriceRequest) error {
	if req.PriceUnitConfig == nil {
		return ierr.NewError("price_unit_config is required for CUSTOM price unit type").
			WithHint("Price unit config must be provided when using custom pricing units").
			Mark(ierr.ErrValidation)
	}

	// 1. Fetch the price unit by code
	priceUnit, err := s.PriceUnitRepo.GetByCode(ctx, req.PriceUnitConfig.PriceUnit)
	if err != nil {
		return err
	}

	// 2. Validate price unit is published
	if priceUnit.Status != types.StatusPublished {
		return ierr.NewError("price unit must be published").
			WithHint("The specified price unit is not published").
			WithReportableDetails(map[string]interface{}{
				"price_unit_code": req.PriceUnitConfig.PriceUnit,
				"status":          priceUnit.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	// 3. Validate base currency matches the price currency
	if priceUnit.BaseCurrency != p.Currency {
		return ierr.NewError("price unit base currency must match price currency").
			WithHint("The price unit's base currency must match the price's currency").
			WithReportableDetails(map[string]interface{}{
				"price_unit_base_currency": priceUnit.BaseCurrency,
				"price_currency":           p.Currency,
			}).
			Mark(ierr.ErrValidation)
	}

	// 4. Convert amount if provided (for FLAT_FEE and PACKAGE billing models)
	if p.PriceUnitAmount != nil {
		// Convert price unit amount to fiat currency
		fiatAmount, err := priceunit.ConvertToFiatCurrencyAmount(
			ctx,
			*p.PriceUnitAmount,
			priceUnit.ConversionRate,
			priceUnit.Precision,
		)
		if err != nil {
			return err
		}

		// Update the price amount with converted value
		p.Amount = fiatAmount
	}

	// 5. Convert tiers if provided (for TIERED billing model)
	if len(p.PriceUnitTiers) > 0 {
		convertedTiers := make([]price.PriceTier, len(p.PriceUnitTiers))

		for i, tier := range p.PriceUnitTiers {
			// Convert unit amount
			convertedUnitAmount, err := priceunit.ConvertToFiatCurrencyAmount(
				ctx,
				tier.UnitAmount,
				priceUnit.ConversionRate,
				priceUnit.Precision,
			)
			if err != nil {
				return err
			}

			convertedTier := price.PriceTier{
				UpTo:       tier.UpTo,
				UnitAmount: convertedUnitAmount,
			}

			// Convert flat amount if provided
			if tier.FlatAmount != nil {
				convertedFlatAmount, err := priceunit.ConvertToFiatCurrencyAmount(
					ctx,
					*tier.FlatAmount,
					priceUnit.ConversionRate,
					priceUnit.Precision,
				)
				if err != nil {
					return err
				}
				convertedTier.FlatAmount = &convertedFlatAmount
			}

			convertedTiers[i] = convertedTier
		}

		// Update the price tiers with converted values
		p.Tiers = price.JSONBTiers(convertedTiers)
	}

	// 6. Set price unit metadata
	p.PriceUnitID = priceUnit.ID
	p.ConversionRate = &priceUnit.ConversionRate

	// 7. Update display amounts after conversion
	p.DisplayAmount = p.GetDisplayAmount()
	if p.PriceUnitAmount != nil {
		p.DisplayPriceUnitAmount = p.GetDisplayPriceUnitAmount(priceUnit.Symbol)
	}

	s.Logger.Infow("applied price unit conversion to price object",
		"price_unit_id", priceUnit.ID,
		"price_unit_code", priceUnit.Code,
		"conversion_rate", priceUnit.ConversionRate.String(),
		"base_currency", priceUnit.BaseCurrency,
		"converted_amount", p.Amount.String(),
		"has_tiers", len(p.Tiers) > 0,
	)

	return nil
}

// syncPriceToQuickBooksIfEnabled syncs a price to QuickBooks via Temporal workflow if the integration is enabled.
// This is a non-blocking operation that offloads the sync to Temporal, preventing blocking and duplicate entity mappings.
// All QuickBooks syncs go through Temporal for reliability, retries, and proper async handling.
func (s *priceService) syncPriceToQuickBooksIfEnabled(ctx context.Context, priceID, planID string) {
	// Get global Temporal service
	temporalSvc := temporalService.GetGlobalTemporalService()
	if temporalSvc == nil {
		s.Logger.Debugw("Temporal service not available, skipping QuickBooks sync",
			"price_id", priceID)
		return
	}

	// Check if QuickBooks integration is available
	if s.IntegrationFactory == nil {
		return
	}

	// Quick check: does QuickBooks connection exist?
	_, err := s.IntegrationFactory.GetQuickBooksIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			// Connection not configured - skip silently
			s.Logger.Debugw("QB TEMPORAL SYNC - connection not found, skipping",
				"price_id", priceID)
			return
		}
		// Actual error - log but don't fail
		s.Logger.Errorw("error checking QuickBooks connection",
			"price_id", priceID,
			"plan_id", planID,
			"error", err)
		return
	}

	// Trigger Temporal workflow for QuickBooks price sync
	workflowInput := map[string]interface{}{
		"price_id": priceID,
		"plan_id":  planID,
	}

	workflowRun, err := temporalSvc.ExecuteWorkflow(
		ctx,
		types.TemporalQuickBooksPriceSyncWorkflow,
		workflowInput,
	)
	if err != nil {
		s.Logger.Errorw("failed to start QuickBooks sync workflow",
			"price_id", priceID,
			"plan_id", planID,
			"error", err)
		return
	}

	s.Logger.Debugw("QuickBooks sync workflow started",
		"price_id", priceID,
		"plan_id", planID,
		"workflow_id", workflowRun.GetID(),
		"run_id", workflowRun.GetRunID())
}
