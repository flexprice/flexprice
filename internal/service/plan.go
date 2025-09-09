package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type PlanService interface {
	CreatePlan(ctx context.Context, req dto.CreatePlanRequest) (*dto.CreatePlanResponse, error)
	GetPlan(ctx context.Context, id string) (*dto.PlanResponse, error)
	GetPlans(ctx context.Context, filter *types.PlanFilter) (*dto.ListPlansResponse, error)
	UpdatePlan(ctx context.Context, id string, req dto.UpdatePlanRequest) (*dto.PlanResponse, error)
	DeletePlan(ctx context.Context, id string) error
	SyncPlanPrices(ctx context.Context, id string) (*dto.SyncPlanPricesResponse, error)
}

type planService struct {
	ServiceParams
}

func NewPlanService(
	params ServiceParams,
) PlanService {
	return &planService{
		ServiceParams: params,
	}
}

func (s *planService) CreatePlan(ctx context.Context, req dto.CreatePlanRequest) (*dto.CreatePlanResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid plan data provided").
			Mark(ierr.ErrValidation)
	}

	plan := req.ToPlan(ctx)

	// Start a transaction to create plan, prices, and entitlements
	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		// 1. Create the plan
		if err := s.PlanRepo.Create(ctx, plan); err != nil {
			return err
		}

		// 2. Create prices in bulk if present
		if len(req.Prices) > 0 {
			prices := make([]*price.Price, len(req.Prices))
			for i, planPriceReq := range req.Prices {
				var price *price.Price
				var err error

				// Skip if the price request is nil
				if planPriceReq.CreatePriceRequest == nil {
					return ierr.NewError("price request cannot be nil").
						WithHint("Please provide valid price configuration").
						Mark(ierr.ErrValidation)
				}

				// If price unit config is provided, use price unit handling logic
				if planPriceReq.PriceUnitConfig != nil {
					// Create a price service instance for price unit handling
					priceService := NewPriceService(s.ServiceParams)
					priceResp, err := priceService.CreatePrice(ctx, *planPriceReq.CreatePriceRequest)
					if err != nil {
						return ierr.WithError(err).
							WithHint("Failed to create price with unit config").
							Mark(ierr.ErrValidation)
					}
					price = priceResp.Price
				} else {
					// For regular prices without unit config, use ToPrice
					price, err = planPriceReq.CreatePriceRequest.ToPrice(ctx)
					if err != nil {
						return ierr.WithError(err).
							WithHint("Failed to create price").
							Mark(ierr.ErrValidation)
					}
				}

				price.EntityType = types.PRICE_ENTITY_TYPE_PLAN
				price.EntityID = plan.ID
				prices[i] = price
			}

			// Create prices in bulk
			if err := s.PriceRepo.CreateBulk(ctx, prices); err != nil {
				return err
			}
		}

		// 3. Create entitlements in bulk if present
		// TODO: add feature validations - maybe by cerating a bulk create method
		// in the entitlement service that can own this for create and updates
		if len(req.Entitlements) > 0 {
			entitlements := make([]*entitlement.Entitlement, len(req.Entitlements))
			for i, entReq := range req.Entitlements {
				ent := entReq.ToEntitlement(ctx, plan.ID)
				entitlements[i] = ent
			}

			// Create entitlements in bulk
			if _, err := s.EntitlementRepo.CreateBulk(ctx, entitlements); err != nil {
				return err
			}
		}

		// 4. Create credit grants in bulk if present
		if len(req.CreditGrants) > 0 {

			creditGrants := make([]*creditgrant.CreditGrant, len(req.CreditGrants))
			for i, creditGrantReq := range req.CreditGrants {
				creditGrant := creditGrantReq.ToCreditGrant(ctx)
				creditGrant.PlanID = &plan.ID
				creditGrant.Scope = types.CreditGrantScopePlan
				// Clear subscription_id for plan-scoped credit grants
				creditGrant.SubscriptionID = nil
				creditGrants[i] = creditGrant
			}

			// validate credit grants
			for _, creditGrant := range creditGrants {
				if err := creditGrant.Validate(); err != nil {
					return ierr.WithError(err).
						WithHint("Invalid credit grant data provided").
						WithReportableDetails(map[string]any{
							"credit_grant": creditGrant,
						}).
						Mark(ierr.ErrValidation)
				}
			}

			// Create credit grants in bulk
			if _, err := s.CreditGrantRepo.CreateBulk(ctx, creditGrants); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	response := &dto.CreatePlanResponse{Plan: plan}

	return response, nil
}

func (s *planService) GetPlan(ctx context.Context, id string) (*dto.PlanResponse, error) {
	if id == "" {
		return nil, ierr.NewError("plan ID is required").
			WithHint("Please provide a valid plan ID").
			Mark(ierr.ErrValidation)
	}

	plan, err := s.PlanRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	priceService := NewPriceService(s.ServiceParams)
	entitlementService := NewEntitlementService(s.ServiceParams)

	pricesResponse, err := priceService.GetPricesByPlanID(ctx, plan.ID)
	if err != nil {
		s.Logger.Errorw("failed to fetch prices for plan", "plan_id", plan.ID, "error", err)
		return nil, err
	}

	entitlements, err := entitlementService.GetPlanEntitlements(ctx, plan.ID)
	if err != nil {
		s.Logger.Errorw("failed to fetch entitlements for plan", "plan_id", plan.ID, "error", err)
		return nil, err
	}

	creditGrants, err := NewCreditGrantService(s.ServiceParams).GetCreditGrantsByPlan(ctx, plan.ID)
	if err != nil {
		s.Logger.Errorw("failed to fetch credit grants for plan", "plan_id", plan.ID, "error", err)
		return nil, err
	}

	response := &dto.PlanResponse{
		Plan:         plan,
		Prices:       pricesResponse.Items,
		Entitlements: entitlements.Items,
		CreditGrants: creditGrants.Items,
	}
	return response, nil
}

func (s *planService) GetPlans(ctx context.Context, filter *types.PlanFilter) (*dto.ListPlansResponse, error) {
	if filter == nil {
		filter = types.NewPlanFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	// Fetch plans
	plans, err := s.PlanRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve plans").
			Mark(ierr.ErrDatabase)
	}

	// Get count
	count, err := s.PlanRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Build response
	response := &dto.ListPlansResponse{
		Items: make([]*dto.PlanResponse, len(plans)),
		Pagination: types.NewPaginationResponse(
			count,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}

	if len(plans) == 0 {
		return response, nil
	}

	for i, plan := range plans {
		response.Items[i] = &dto.PlanResponse{Plan: plan}
	}

	// Expand entitlements and prices if requested
	planIDs := lo.Map(plans, func(plan *plan.Plan, _ int) string {
		return plan.ID
	})

	// Create maps for storing expanded data
	pricesByPlanID := make(map[string][]*dto.PriceResponse)
	entitlementsByPlanID := make(map[string][]*dto.EntitlementResponse)
	creditGrantsByPlanID := make(map[string][]*dto.CreditGrantResponse)

	priceService := NewPriceService(s.ServiceParams)
	entitlementService := NewEntitlementService(s.ServiceParams)

	// If prices or entitlements expansion is requested, fetch them in bulk
	// Fetch prices if requested
	if filter.GetExpand().Has(types.ExpandPrices) {
		priceFilter := types.NewNoLimitPriceFilter().
			WithEntityIDs(planIDs).
			WithStatus(types.StatusPublished).
			WithEntityType(types.PRICE_ENTITY_TYPE_PLAN)

		// If meters should be expanded, propagate the expansion to prices
		if filter.GetExpand().Has(types.ExpandMeters) {
			priceFilter = priceFilter.WithExpand(string(types.ExpandMeters))
		}

		prices, err := priceService.GetPrices(ctx, priceFilter)
		if err != nil {
			return nil, err
		}

		for _, p := range prices.Items {
			// TODO: !REMOVE after migration
			if p.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
				p.PlanID = p.EntityID
			}
			pricesByPlanID[p.EntityID] = append(pricesByPlanID[p.EntityID], p)
		}
	}

	// Fetch entitlements if requested
	if filter.GetExpand().Has(types.ExpandEntitlements) {
		entFilter := types.NewNoLimitEntitlementFilter().
			WithEntityIDs(planIDs).
			WithStatus(types.StatusPublished)

		// If features should be expanded, propagate the expansion to entitlements
		if filter.GetExpand().Has(types.ExpandFeatures) {
			entFilter = entFilter.WithExpand(string(types.ExpandFeatures))
		}

		// Apply the exact same sort order as plans
		if filter.Sort != nil {
			entFilter.Sort = append(entFilter.Sort, filter.Sort...)
		}

		entitlements, err := entitlementService.ListEntitlements(ctx, entFilter)
		if err != nil {
			return nil, err
		}

		for _, e := range entitlements.Items {
			entitlementsByPlanID[e.Entitlement.EntityID] = append(entitlementsByPlanID[e.Entitlement.EntityID], e)
		}
	}

	// Fetch credit grants if requested
	if filter.GetExpand().Has(types.ExpandCreditGrant) {

		for _, planID := range planIDs {
			creditGrants, err := s.CreditGrantRepo.GetByPlan(ctx, planID)
			if err != nil {
				return nil, err
			}

			for _, cg := range creditGrants {
				creditGrantsByPlanID[lo.FromPtr(cg.PlanID)] = append(creditGrantsByPlanID[lo.FromPtr(cg.PlanID)], &dto.CreditGrantResponse{CreditGrant: cg})
			}
		}
	}

	// Build response with expanded fields
	for i, plan := range plans {

		// Add prices if available
		if prices, ok := pricesByPlanID[plan.ID]; ok {
			response.Items[i].Prices = prices
		}

		// Add entitlements if available
		if entitlements, ok := entitlementsByPlanID[plan.ID]; ok {
			response.Items[i].Entitlements = entitlements
		}

		// Add credit grants if available
		if creditGrants, ok := creditGrantsByPlanID[plan.ID]; ok {
			response.Items[i].CreditGrants = creditGrants
		}
	}

	return response, nil
}

func (s *planService) UpdatePlan(ctx context.Context, id string, req dto.UpdatePlanRequest) (*dto.PlanResponse, error) {
	if id == "" {
		return nil, ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get the existing plan
	planResponse, err := s.GetPlan(ctx, id)
	if err != nil {
		return nil, err
	}

	plan := planResponse.Plan

	// Update plan fields if provided
	if req.Name != nil {
		plan.Name = *req.Name
	}
	if req.Description != nil {
		plan.Description = *req.Description
	}
	if req.LookupKey != nil {
		plan.LookupKey = *req.LookupKey
	}
	if req.Metadata != nil {
		plan.Metadata = req.Metadata
	}
	if req.DisplayOrder != nil {
		plan.DisplayOrder = req.DisplayOrder
	}

	// Start a transaction for updating plan, prices, and entitlements
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// 1. Update the plan
		if err := s.PlanRepo.Update(ctx, plan); err != nil {
			return err
		}

		// 2. Handle prices
		if len(req.Prices) > 0 {
			// Create maps for tracking
			reqPriceMap := make(map[string]dto.UpdatePlanPriceRequest)
			for _, reqPrice := range req.Prices {
				if reqPrice.ID != "" {
					reqPriceMap[reqPrice.ID] = reqPrice
				}
			}

			// Track prices to delete
			pricesToDelete := make([]string, 0)

			// Handle existing prices
			for _, price := range planResponse.Prices {
				if reqPrice, ok := reqPriceMap[price.ID]; ok {
					// Update existing price
					price.Description = reqPrice.Description
					price.Metadata = reqPrice.Metadata
					price.LookupKey = reqPrice.LookupKey
					if err := s.PriceRepo.Update(ctx, price.Price); err != nil {
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
			bulkCreatePrices := make([]*price.Price, 0) // Separate slice for bulk creation

			for _, reqPrice := range req.Prices {
				if reqPrice.ID == "" {
					var newPrice *price.Price
					var err error

					// If price unit config is provided, handle it through the price service
					if reqPrice.PriceUnitConfig != nil {
						// Set plan ID before creating price
						reqPrice.CreatePriceRequest.EntityID = plan.ID
						reqPrice.CreatePriceRequest.EntityType = types.PRICE_ENTITY_TYPE_PLAN

						priceService := NewPriceService(s.ServiceParams)
						priceResp, err := priceService.CreatePrice(ctx, *reqPrice.CreatePriceRequest)
						if err != nil {
							return ierr.WithError(err).
								WithHint("Failed to create price with unit config").
								Mark(ierr.ErrValidation)
						}
						newPrice = priceResp.Price
						// Add to newPrices but not to bulkCreatePrices since it's already created
						newPrices = append(newPrices, newPrice)
					} else {
						// For regular prices without unit config, use ToPrice
						// Ensure price unit type is set, default to FIAT if not provided
						if reqPrice.PriceUnitType == "" {
							reqPrice.PriceUnitType = types.PRICE_UNIT_TYPE_FIAT
						}
						newPrice, err = reqPrice.ToPrice(ctx)
						if err != nil {
							return ierr.WithError(err).
								WithHint("Failed to create price").
								Mark(ierr.ErrValidation)
						}
						newPrice.EntityType = types.PRICE_ENTITY_TYPE_PLAN
						newPrice.EntityID = plan.ID
						// Add to both slices since this needs bulk creation
						newPrices = append(newPrices, newPrice)
						bulkCreatePrices = append(bulkCreatePrices, newPrice)
					}
				}
			}

			// Only bulk create prices that weren't already created through the price service
			if len(bulkCreatePrices) > 0 {
				if err := s.PriceRepo.CreateBulk(ctx, bulkCreatePrices); err != nil {
					return err
				}
			}
		}

		// 3. Handle entitlements
		if len(req.Entitlements) > 0 {
			// Create maps for tracking
			reqEntMap := make(map[string]dto.UpdatePlanEntitlementRequest)
			for _, reqEnt := range req.Entitlements {
				if reqEnt.ID != "" {
					reqEntMap[reqEnt.ID] = reqEnt
				}
			}

			// Track entitlements to delete
			entsToDelete := make([]string, 0)

			// Handle existing entitlements
			for _, ent := range planResponse.Entitlements {
				if reqEnt, ok := reqEntMap[ent.ID]; ok {
					// Update existing entitlement
					ent.IsEnabled = reqEnt.IsEnabled
					ent.UsageLimit = reqEnt.UsageLimit
					ent.UsageResetPeriod = reqEnt.UsageResetPeriod
					ent.IsSoftLimit = reqEnt.IsSoftLimit
					ent.StaticValue = reqEnt.StaticValue
					if _, err := s.EntitlementRepo.Update(ctx, ent.Entitlement); err != nil {
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
					ent := reqEnt.ToEntitlement(ctx, plan.ID)
					newEntitlements = append(newEntitlements, ent)
				}
			}

			if len(newEntitlements) > 0 {
				if _, err := s.EntitlementRepo.CreateBulk(ctx, newEntitlements); err != nil {
					return err
				}
			}
		}

		// 4. Handle credit grants
		if len(req.CreditGrants) > 0 {
			// Create maps for tracking
			reqCreditGrantMap := make(map[string]dto.UpdatePlanCreditGrantRequest)
			for _, reqCreditGrant := range req.CreditGrants {
				if reqCreditGrant.ID != "" {
					reqCreditGrantMap[reqCreditGrant.ID] = reqCreditGrant
				}
			}

			// Track credit grants to delete
			creditGrantsToDelete := make([]string, 0)

			// Handle existing credit grants
			for _, cg := range planResponse.CreditGrants {
				if reqCreditGrant, ok := reqCreditGrantMap[cg.ID]; ok {
					// Update existing credit grant using the UpdateCreditGrant method
					if reqCreditGrant.CreateCreditGrantRequest != nil {
						updateReq := dto.UpdateCreditGrantRequest{
							Name:     lo.ToPtr(reqCreditGrant.Name),
							Metadata: lo.ToPtr(reqCreditGrant.Metadata),
						}
						updateReq.UpdateCreditGrant(cg.CreditGrant, ctx)
					}
					if _, err := s.CreditGrantRepo.Update(ctx, cg.CreditGrant); err != nil {
						return err
					}
				} else {
					// Delete credit grant not in request
					creditGrantsToDelete = append(creditGrantsToDelete, cg.ID)
				}
			}

			// Delete credit grants in bulk
			if len(creditGrantsToDelete) > 0 {
				if err := s.CreditGrantRepo.DeleteBulk(ctx, creditGrantsToDelete); err != nil {
					return err
				}
			}

			// Create new credit grants
			newCreditGrants := make([]*creditgrant.CreditGrant, 0)
			for _, reqCreditGrant := range req.CreditGrants {
				if reqCreditGrant.ID == "" {
					// Use the embedded CreateCreditGrantRequest
					createReq := *reqCreditGrant.CreateCreditGrantRequest
					createReq.Scope = types.CreditGrantScopePlan
					createReq.PlanID = &plan.ID

					newCreditGrant := createReq.ToCreditGrant(ctx)
					// Clear subscription_id for plan-scoped credit grants
					newCreditGrant.SubscriptionID = nil
					newCreditGrants = append(newCreditGrants, newCreditGrant)
				}
			}

			if len(newCreditGrants) > 0 {
				if _, err := s.CreditGrantRepo.CreateBulk(ctx, newCreditGrants); err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return s.GetPlan(ctx, id)
}

func (s *planService) DeletePlan(ctx context.Context, id string) error {

	if id == "" {
		return ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	// check if plan exists
	plan, err := s.PlanRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	subscriptionFilters := types.NewDefaultQueryFilter()
	subscriptionFilters.Status = lo.ToPtr(types.StatusPublished)
	subscriptionFilters.Limit = lo.ToPtr(1)
	subscriptions, err := s.SubRepo.List(ctx, &types.SubscriptionFilter{
		QueryFilter:             subscriptionFilters,
		PlanID:                  id,
		SubscriptionStatusNotIn: []types.SubscriptionStatus{types.SubscriptionStatusCancelled},
	})
	if err != nil {
		return err
	}

	if len(subscriptions) > 0 {
		return ierr.NewError("plan is still associated with subscriptions").
			WithHint("Please remove the active subscriptions before deleting this plan.").
			WithReportableDetails(map[string]interface{}{
				"plan_id": id,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	err = s.PlanRepo.Delete(ctx, plan)
	if err != nil {
		return err
	}
	return nil
}

func (s *planService) SyncPlanPrices(ctx context.Context, id string) (*dto.SyncPlanPricesResponse, error) {
	if id == "" {
		return nil, ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get the plan to be synced
	plan, err := s.PlanRepo.Get(ctx, id)
	if err != nil {

		return nil, err
	}

	s.Logger.Infow("Found plan", "plan_id", id, "plan_name", plan.Name)

	// Get all plan-scoped prices including expired ones
	priceService := NewPriceService(s.ServiceParams)
	priceFilter := types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{id}).
		WithEntityType(types.PRICE_ENTITY_TYPE_PLAN).
		WithStatus(types.StatusPublished).
		WithAllowExpiredPrices(true)

	pricesResponse, err := priceService.GetPrices(ctx, priceFilter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list prices for plan").
			Mark(ierr.ErrDatabase)
	}

	// Create price map for quick lookups
	priceMap := make(map[string]*price.Price)
	for _, priceResp := range pricesResponse.Items {
		priceMap[priceResp.ID] = priceResp.Price
	}

	// Set up filter for subscriptions
	subscriptionFilter := &types.SubscriptionFilter{}
	subscriptionFilter.PlanID = id
	subscriptionFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}

	// Get all active subscriptions for this plan
	subs, err := s.SubRepo.ListAll(ctx, subscriptionFilter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscriptions").
			Mark(ierr.ErrDatabase)
	}

	s.Logger.Infow("Found active subscriptions using plan", "plan_id", id, "subscription_count", len(subs))

	totalAdded := 0
	totalUpdated := 0
	totalSkipped := 0

	// Iterate through each subscription
	for _, sub := range subs {

		// Get line items for the subscription
		lineItems, err := s.SubscriptionLineItemRepo.ListBySubscription(ctx, sub)
		if err != nil {
			s.Logger.Infow("Failed to get line items for subscription", "subscription_id", sub.ID, "error", err)
			continue
		}

		// Get subscription-specific prices (overrides)
		subPriceFilter := types.NewNoLimitPriceFilter().
			WithEntityIDs([]string{sub.ID}).
			WithEntityType(types.PRICE_ENTITY_TYPE_SUBSCRIPTION)

		subPricesResponse, err := priceService.GetPrices(ctx, subPriceFilter)
		if err != nil {
			s.Logger.Infow("Failed to get subscription prices", "subscription_id", sub.ID, "error", err)
			continue
		}

		// Build override price relationship maps
		// overrideToParentMap: maps override price ID to its parent price ID
		// parentToOverrideMap: maps parent price ID to its override price ID
		overrideToParentMap := make(map[string]string) // overridePriceID -> parentPriceID
		parentToOverrideMap := make(map[string]string) // parentPriceID -> overridePriceID

		for _, priceResp := range subPricesResponse.Items {
			if priceResp.ParentPriceID != "" {
				overrideToParentMap[priceResp.ID] = priceResp.ParentPriceID
				parentToOverrideMap[priceResp.ParentPriceID] = priceResp.ID
			}
		}

		// Build line item lookup map
		// Maps both actual price IDs and parent price IDs (for overrides) to line items
		// Only plan line items are processed for plan sync - addon line items are preserved
		lineItemMap := make(map[string]*subscription.SubscriptionLineItem)
		for _, item := range lineItems {
			// Skip if line item is not active or is not a plan line item
			if item.EntityType != types.SubscriptionLineItemEntityTypePlan {
				continue
			}

			// Map the actual price ID of the line item
			lineItemMap[item.PriceID] = item

			// If this is an override price, also map it to the parent price
			// This allows us to check if a parent price already has a line item via override
			if parentPriceID, isOverride := overrideToParentMap[item.PriceID]; isOverride {
				lineItemMap[parentPriceID] = item
			}
		}

		addedCount := 0
		updatedCount := 0
		skippedCount := 0

		// SyncPlanPrices - Core Price Synchronization Logic
		//
		// This section handles the synchronization of prices between a plan and its subscriptions.
		// The process follows these rules:
		//
		// 1. Price Eligibility:
		//    - Each price must match the subscription's currency and billing period
		//    - Ineligible prices are skipped to maintain billing consistency
		//
		// 2. Line Item States:
		//    - Existing line items with expired prices (EndDate != nil) and not override prices -> Mark for end
		//    - Existing line items with active prices (EndDate == nil) -> Keep as is
		//    - Missing line items for active prices -> Create new (regardless of start date)
		//    - Missing line items for expired prices -> Skip
		//
		// 3. Price Lifecycle:
		//    - Active Price: EndDate is nil, can be attached to subscriptions (start date doesn't matter)
		//    - Expired Price: Has EndDate, existing line items will be ended (unless override)
		//
		// 4. Override Price Handling:
		//    - Override prices are never ended by plan sync (subscription-specific)
		//    - Parent prices with existing override line items are not processed
		//    - This maintains subscription-specific pricing while syncing plan prices
		//
		// The sync ensures subscriptions accurately reflect the current state of plan prices
		// while maintaining proper billing continuity and respecting price overrides.
		// Start dates are preserved as-is for proper billing timing.

		subscriptionService := NewSubscriptionService(s.ServiceParams)
		for priceID, planPrice := range priceMap {
			// Skip if price currency/billing period doesn't match subscription
			if !planPrice.IsEligibleForSubscription(sub.Currency, sub.BillingPeriod, sub.BillingPeriodCount) {
				s.Logger.Infow("Skipping incompatible price",
					"subscription_id", sub.ID,
					"price_id", priceID,
					"reason", "currency/billing_period mismatch")
				continue
			}

			lineItem, existingLineItem := lineItemMap[priceID]

			// Handle existing line items
			if existingLineItem {
				if planPrice.EndDate != nil {
					// Check if this price has an override - override prices should not be ended by plan sync
					if _, isOverride := parentToOverrideMap[priceID]; isOverride {
						s.Logger.Infow("Skipping override price line item",
							"subscription_id", sub.ID,
							"price_id", priceID,
							"reason", "override price - subscription specific")
						skippedCount++
						continue
					}

					// Price has expired - end the line item
					s.Logger.Infow("Ending line item for expired price",
						"subscription_id", sub.ID,
						"price_id", priceID,
						"end_date", planPrice.EndDate)

					deleteReq := dto.DeleteSubscriptionLineItemRequest{
						EndDate: planPrice.EndDate,
					}
					if _, err = subscriptionService.DeleteSubscriptionLineItem(ctx, lineItem.ID, deleteReq); err != nil {
						s.Logger.Errorw("Failed to end line item",
							"subscription_id", sub.ID,
							"line_item_id", lineItem.ID,
							"error", err)
						continue
					}
					updatedCount++
				} else {
					// Price is still active - no changes needed
					skippedCount++
				}
				continue
			}

			// Handle missing line items - create for any active price (no end date)
			if planPrice.EndDate == nil {
				// Create line item for active price (regardless of start date)
				s.Logger.Infow("Creating line item for price",
					"subscription_id", sub.ID,
					"price_id", priceID,
					"start_date", planPrice.StartDate)

				createReq := dto.CreateSubscriptionLineItemRequest{
					PriceID:   planPrice.ID,
					StartDate: planPrice.StartDate,
					Metadata: map[string]string{
						"added_by":     "plan_sync_api",
						"sync_version": "2.0",
					},
					Quantity: planPrice.GetDefaultQuantity(),
				}

				if _, err = subscriptionService.AddSubscriptionLineItem(ctx, sub.ID, createReq); err != nil {
					s.Logger.Errorw("Failed to create line item",
						"subscription_id", sub.ID,
						"price_id", priceID,
						"error", err)
					continue
				}
				addedCount++
			} else {
				// Price has expired (EndDate != nil) - skip creating line items
				s.Logger.Infow("Skipping expired price",
					"subscription_id", sub.ID,
					"price_id", priceID,
					"end_date", planPrice.EndDate,
					"reason", "price expired")
				skippedCount++
			}
		}

		s.Logger.Infow("Subscription processed",
			"subscription_id", sub.ID,
			"added_count", addedCount,
			"updated_count", updatedCount,
			"skipped_count", skippedCount)

		totalAdded += addedCount
		totalUpdated += updatedCount
		totalSkipped += skippedCount
	}

	// Update response with final statistics
	response := &dto.SyncPlanPricesResponse{
		Message:  "Plan prices synchronized successfully",
		PlanID:   id,
		PlanName: plan.Name,
		SynchronizationSummary: struct {
			SubscriptionsProcessed int `json:"subscriptions_processed"`
			PricesAdded            int `json:"prices_added"`
			PricesRemoved          int `json:"prices_removed"`
			PricesSkipped          int `json:"prices_skipped"`
		}{
			SubscriptionsProcessed: len(subs),
			PricesAdded:            totalAdded,
			PricesRemoved:          totalUpdated, // Using removed field for updates
			PricesSkipped:          totalSkipped,
		},
	}

	s.Logger.Infow("Plan sync completed",
		"total_added", totalAdded,
		"total_updated", totalUpdated,
		"total_skipped", totalSkipped)

	return response, nil
}
