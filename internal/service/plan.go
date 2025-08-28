package service

import (
	"context"
	"time"

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

type SyncPlanPricesResponse struct {
	Message                string `json:"message"`
	PlanID                 string `json:"plan_id"`
	PlanName               string `json:"plan_name"`
	SynchronizationSummary struct {
		SubscriptionsProcessed int `json:"subscriptions_processed"`
		PricesAdded            int `json:"prices_added"`
		PricesRemoved          int `json:"prices_removed"`
		PricesSkipped          int `json:"prices_skipped"`
	} `json:"synchronization_summary"`
}

type PlanService interface {
	CreatePlan(ctx context.Context, req dto.CreatePlanRequest) (*dto.CreatePlanResponse, error)
	GetPlan(ctx context.Context, id string) (*dto.PlanResponse, error)
	GetPlans(ctx context.Context, filter *types.PlanFilter) (*dto.ListPlansResponse, error)
	UpdatePlan(ctx context.Context, id string, req dto.UpdatePlanRequest) (*dto.PlanResponse, error)
	DeletePlan(ctx context.Context, id string) error
	SyncPlanPrices(ctx context.Context, id string) (*SyncPlanPricesResponse, error)
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

// SyncPlanPrices synchronizes plan prices across all active subscriptions
// This method ensures that all subscriptions using a plan have the correct line items
// based on the current plan's active prices, while preserving subscription-specific overrides.
func (s *planService) SyncPlanPrices(ctx context.Context, id string) (*SyncPlanPricesResponse, error) {

	if id == "" {
		return nil, ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	s.Logger.Infow("Starting plan price synchronization", "plan_id", id)

	// Get the plan to be synced
	p, err := s.PlanRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if p.Status != types.StatusPublished {
		return nil, ierr.NewError("plan is not active").
			WithHint("Plan must be active to sync prices").
			WithReportableDetails(map[string]interface{}{
				"plan_id": id,
				"status":  p.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get all plan-scoped prices (including expired ones for proper termination)
	planPriceFilter := types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{id}).
		WithEntityType(types.PRICE_ENTITY_TYPE_PLAN).
		WithAllowExpiredPrices(true)

	planPrices, err := s.PriceRepo.List(ctx, planPriceFilter)
	if err != nil {
		return nil, err
	}

	// Separate active and expired prices
	var activePlanPrices, expiredPlanPrices []*price.Price
	planPriceMap := make(map[string]*price.Price)

	for _, price := range planPrices {
		planPriceMap[price.ID] = price
		if price.IsActive() {
			activePlanPrices = append(activePlanPrices, price)
		} else {
			expiredPlanPrices = append(expiredPlanPrices, price)
		}
	}

	if len(planPrices) == 0 {
		return &SyncPlanPricesResponse{
			Message:  "No prices found for this plan",
			PlanID:   id,
			PlanName: p.Name,
			SynchronizationSummary: struct {
				SubscriptionsProcessed int `json:"subscriptions_processed"`
				PricesAdded            int `json:"prices_added"`
				PricesRemoved          int `json:"prices_removed"`
				PricesSkipped          int `json:"prices_skipped"`
			}{
				SubscriptionsProcessed: 0,
				PricesAdded:            0,
				PricesRemoved:          0,
				PricesSkipped:          0,
			},
		}, nil
	}

	s.Logger.Infow("Found plan prices",
		"plan_id", id,
		"active_price_count", len(activePlanPrices),
		"expired_price_count", len(expiredPlanPrices))

	// Get all active subscriptions for this plan
	subscriptionFilter := types.NewNoLimitSubscriptionFilter()
	subscriptionFilter.PlanID = id
	subscriptionFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}

	subs, err := s.SubRepo.ListAll(ctx, subscriptionFilter)
	if err != nil {
		return nil, err
	}

	s.Logger.Infow("Found active subscriptions using plan",
		"plan_id", id,
		"subscription_count", len(subs))

	totalAdded := 0
	totalRemoved := 0
	totalSkipped := 0
	var syncErrors []error

	// Process subscriptions with better error tracking
	for _, sub := range subs {
		// Process this subscription with transaction safety
		err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
			added, removed, skipped, err := s.syncSubscriptionLineItems(txCtx, sub, activePlanPrices, expiredPlanPrices, p)
			if err != nil {
				return err
			}

			totalAdded += added
			totalRemoved += removed
			totalSkipped += skipped
			return nil
		})

		if err != nil {
			s.Logger.Errorw("Failed to sync subscription line items",
				"subscription_id", sub.ID,
				"error", err)
			syncErrors = append(syncErrors, err)
			continue
		}
	}

	if len(syncErrors) > 0 {
		s.Logger.Errorw("Some subscriptions failed to sync",
			"plan_id", id,
			"failed_count", len(syncErrors),
			"total_subscriptions", len(subs))
	}

	response := &SyncPlanPricesResponse{
		Message:  "Plan prices synchronized successfully",
		PlanID:   id,
		PlanName: p.Name,
		SynchronizationSummary: struct {
			SubscriptionsProcessed int `json:"subscriptions_processed"`
			PricesAdded            int `json:"prices_added"`
			PricesRemoved          int `json:"prices_removed"`
			PricesSkipped          int `json:"prices_skipped"`
		}{
			SubscriptionsProcessed: len(subs),
			PricesAdded:            totalAdded,
			PricesRemoved:          totalRemoved,
			PricesSkipped:          totalSkipped,
		},
	}

	s.Logger.Infow("Plan sync completed",
		"plan_id", id,
		"total_added", totalAdded,
		"total_removed", totalRemoved,
		"total_skipped", totalSkipped)

	return response, nil
}

func (s *planService) syncSubscriptionLineItems(
	ctx context.Context,
	sub *subscription.Subscription,
	activePrices []*price.Price,
	expiredPrices []*price.Price,
	plan *plan.Plan,
) (added, removed, skipped int, err error) {

	// STEP 1: Get existing line items for this subscription (only active ones)
	lineItems, err := s.SubscriptionLineItemRepo.ListBySubscription(ctx, sub)
	if err != nil {
		return 0, 0, 0, err
	}

	// STEP 2: Get subscription override prices
	overridePrices, err := s.getSubscriptionOverridePrices(ctx, sub.ID)
	if err != nil {
		return 0, 0, 0, err
	}

	// STEP 3: Build maps for efficient lookups
	existingLineItems := make(map[string]*subscription.SubscriptionLineItem)
	parentPriceToLineItem := make(map[string]*subscription.SubscriptionLineItem)

	// Process line items - only consider plan-scoped ones
	for _, item := range lineItems {
		if item.Status == types.StatusPublished &&
			item.EntityType == types.SubscriptionLineItemEntitiyTypePlan &&
			item.EntityID == plan.ID {
			existingLineItems[item.PriceID] = item
		}
	}

	// Process override prices to map parent price relationships
	for _, overridePrice := range overridePrices {
		for _, item := range lineItems {
			if item.PriceID == overridePrice.ID && item.Status == types.StatusPublished {
				parentPriceToLineItem[overridePrice.ParentPriceID] = item
			}
		}
	}

	// STEP 4: Process active prices - identify what to add
	toAdd := make([]*price.Price, 0)
	for _, price := range activePrices {
		if !s.isPriceEligibleForSubscription(price, sub) {
			skipped++
			continue
		}

		// Skip if line item already exists
		if existingLineItems[price.ID] != nil {
			skipped++
			continue
		}

		// Check if subscription has an override for this price
		if parentPriceToLineItem[price.ID] != nil {
			skipped++
			continue
		}

		// No line item and no override, add to list
		toAdd = append(toAdd, price)
	}

	// STEP 5: Process expired prices - identify what to terminate
	toTerminate := make([]*subscription.SubscriptionLineItem, 0)
	expiredPricesMap := make(map[string]*price.Price)

	for _, price := range expiredPrices {
		expiredPricesMap[price.ID] = price
		// Check if there's an active line item for this expired price
		if lineItem, exists := existingLineItems[price.ID]; exists {
			toTerminate = append(toTerminate, lineItem)
		}
	}

	// STEP 6: Execute changes
	removed = s.terminateExpiredLineItems(ctx, toTerminate, expiredPricesMap, sub.ID)
	added = s.createNewLineItems(ctx, toAdd, sub, plan)

	s.Logger.Infow("Subscription sync completed",
		"subscription_id", sub.ID,
		"added", added,
		"removed", removed,
		"skipped", skipped,
	)

	return added, removed, skipped, nil
}

// getSubscriptionOverridePrices fetches override prices for a subscription
func (s *planService) getSubscriptionOverridePrices(ctx context.Context, subscriptionID string) ([]*price.Price, error) {
	overridePriceFilter := types.NewNoLimitPriceFilter()
	overridePriceFilter = overridePriceFilter.WithEntityIDs([]string{subscriptionID})
	overridePriceFilter = overridePriceFilter.WithEntityType(types.PRICE_ENTITY_TYPE_SUBSCRIPTION)

	return s.PriceRepo.List(ctx, overridePriceFilter)
}

// terminateExpiredLineItems terminates the specified line items
func (s *planService) terminateExpiredLineItems(ctx context.Context, toTerminate []*subscription.SubscriptionLineItem, expiredPricesMap map[string]*price.Price, subscriptionID string) int {
	removed := 0

	for _, item := range toTerminate {
		endDate := time.Now().UTC()

		// Use price end date if available from the price map
		if price, exists := expiredPricesMap[item.PriceID]; exists && price.EndDate != nil {
			endDate = *price.EndDate
		}

		deleteReq := &dto.DeleteSubscriptionLineItemRequest{
			EndDate: lo.ToPtr(endDate),
		}

		subscriptionService := NewSubscriptionService(s.ServiceParams)
		err := subscriptionService.DeleteSubscriptionLineItem(ctx, item.ID, deleteReq)
		if err != nil {
			s.Logger.Errorw("Failed to terminate line item",
				"subscription_id", subscriptionID,
				"line_item_id", item.ID,
				"error", err)
			continue
		}

		removed++
		s.Logger.Infow("Terminated line item",
			"subscription_id", subscriptionID,
			"line_item_id", item.ID,
			"price_id", item.PriceID,
			"end_date", endDate)
	}

	return removed
}

// createNewLineItems creates new line items for the specified prices
func (s *planService) createNewLineItems(
	ctx context.Context,
	toAdd []*price.Price,
	sub *subscription.Subscription,
	plan *plan.Plan,
) int {

	added := 0

	// Create subscription service to reuse existing line item creation logic
	subscriptionService := NewSubscriptionService(s.ServiceParams)

	for _, price := range toAdd {
		// Use the existing CreateSubscriptionLineItem method from subscription service
		createReq := dto.CreateSubscriptionLineItemRequest{
			PriceID:     price.ID,
			Quantity:    lo.ToPtr(price.GetDefaultQuantity()),
			DisplayName: plan.Name, // Use plan name as default display name
			StartDate:   lo.ToPtr(sub.StartDate),
			Metadata:    map[string]string{"added_by": "plan_sync_api"},
		}

		_, err := subscriptionService.CreateSubscriptionLineItem(ctx, sub.ID, createReq)
		if err != nil {
			s.Logger.Errorw("Failed to create line item",
				"subscription_id", sub.ID,
				"price_id", price.ID,
				"error", err)
			continue
		}

		added++
		s.Logger.Infow("Added line item",
			"subscription_id", sub.ID,
			"price_id", price.ID)
	}

	return added
}

// isPriceEligibleForSubscription provides enhanced eligibility checking beyond basic currency/billing period matching
// This method considers subscription-specific overrides and entity type constraints
func (s *planService) isPriceEligibleForSubscription(price *price.Price, sub *subscription.Subscription) bool {
	// Basic eligibility check
	if !price.IsEligibleForSubscription(sub.Currency, sub.BillingPeriod, sub.BillingPeriodCount) {
		return false
	}

	// Additional eligibility rules:

	// 1. Plan-scoped prices should only apply to subscriptions using that plan
	if price.IsPlanScoped() && price.EntityID != sub.PlanID {
		return false
	}

	// 2. Subscription-scoped prices (overrides) should only apply to the specific subscription
	if price.IsSubscriptionScoped() && price.EntityID != sub.ID {
		return false
	}

	// 3. Check if price is within its active date range
	if !price.IsActive() {
		return false
	}

	return true
}
