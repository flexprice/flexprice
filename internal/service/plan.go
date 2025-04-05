package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type PlanService interface {
	CreatePlan(ctx context.Context, req dto.CreatePlanRequest) (*dto.CreatePlanResponse, error)
	GetPlan(ctx context.Context, id string) (*dto.PlanResponse, error)
	GetPlans(ctx context.Context, filter *types.PlanFilter) (*dto.ListPlansResponse, error)
	UpdatePlan(ctx context.Context, id string, req dto.UpdatePlanRequest) (*dto.PlanResponse, error)
	DeletePlan(ctx context.Context, id string) error
}

type planService struct {
	planRepo         plan.Repository
	priceRepo        price.Repository
	subscriptionRepo subscription.Repository
	meterRepo        meter.Repository
	entitlementRepo  entitlement.Repository
	featureRepo      feature.Repository
	client           postgres.IClient
	logger           *logger.Logger
}

func NewPlanService(
	client postgres.IClient,
	planRepo plan.Repository,
	priceRepo price.Repository,
	subscriptionRepo subscription.Repository,
	meterRepo meter.Repository,
	entitlementRepo entitlement.Repository,
	featureRepo feature.Repository,
	logger *logger.Logger,
) PlanService {
	return &planService{
		client:           client,
		planRepo:         planRepo,
		priceRepo:        priceRepo,
		subscriptionRepo: subscriptionRepo,
		meterRepo:        meterRepo,
		entitlementRepo:  entitlementRepo,
		featureRepo:      featureRepo,
		logger:           logger,
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
	err := s.client.WithTx(ctx, func(ctx context.Context) error {
		// 1. Create the plan
		if err := s.planRepo.Create(ctx, plan); err != nil {
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
				price.PlanID = plan.ID
				prices[i] = price
			}

			// Create prices in bulk
			if err := s.priceRepo.CreateBulk(ctx, prices); err != nil {
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
			if _, err := s.entitlementRepo.CreateBulk(ctx, entitlements); err != nil {
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

	plan, err := s.planRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	priceService := NewPriceService(s.priceRepo, s.meterRepo, s.logger)
	entitlementService := NewEntitlementService(s.entitlementRepo, s.planRepo, s.featureRepo, s.meterRepo, s.logger)

	pricesResponse, err := priceService.GetPricesByPlanID(ctx, plan.ID)
	if err != nil {
		s.logger.Errorw("failed to fetch prices for plan", "plan_id", plan.ID, "error", err)
		return nil, err
	}

	entitlements, err := entitlementService.GetPlanEntitlements(ctx, plan.ID)
	if err != nil {
		s.logger.Errorw("failed to fetch entitlements for plan", "plan_id", plan.ID, "error", err)
		return nil, err
	}

	response := &dto.PlanResponse{
		Plan:         plan,
		Prices:       pricesResponse.Items,
		Entitlements: entitlements.Items,
	}
	return response, nil
}

func (s *planService) GetPlans(ctx context.Context, filter *types.PlanFilter) (*dto.ListPlansResponse, error) {
	if filter == nil {
		filter = types.NewPlanFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	// Fetch plans
	plans, err := s.planRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve plans").
			Mark(ierr.ErrDatabase)
	}

	// Get count
	count, err := s.planRepo.Count(ctx, filter)
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

	priceService := NewPriceService(s.priceRepo, s.meterRepo, s.logger)
	entitlementService := NewEntitlementService(s.entitlementRepo, s.planRepo, s.featureRepo, s.meterRepo, s.logger)

	// If prices or entitlements expansion is requested, fetch them in bulk
	// Fetch prices if requested
	if filter.GetExpand().Has(types.ExpandPrices) {
		priceFilter := types.NewNoLimitPriceFilter().
			WithPlanIDs(planIDs).
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
			pricesByPlanID[p.PlanID] = append(pricesByPlanID[p.PlanID], p)
		}
	}

	// Fetch entitlements if requested
	if filter.GetExpand().Has(types.ExpandEntitlements) {
		entFilter := types.NewNoLimitEntitlementFilter().
			WithPlanIDs(planIDs).
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
			entitlementsByPlanID[e.PlanID] = append(entitlementsByPlanID[e.PlanID], e)
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

	// Start a transaction for updating plan, prices, and entitlements
	err = s.client.WithTx(ctx, func(ctx context.Context) error {
		// 1. Update the plan
		if err := s.planRepo.Update(ctx, plan); err != nil {
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
					if err := s.priceRepo.Update(ctx, price.Price); err != nil {
						return err
					}
				} else {
					// Delete price not in request
					pricesToDelete = append(pricesToDelete, price.ID)
				}
			}

			// Delete prices in bulk
			if len(pricesToDelete) > 0 {
				if err := s.priceRepo.DeleteBulk(ctx, pricesToDelete); err != nil {
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
					newPrice.PlanID = plan.ID
					newPrices = append(newPrices, newPrice)
				}
			}

			if len(newPrices) > 0 {
				if err := s.priceRepo.CreateBulk(ctx, newPrices); err != nil {
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
					if _, err := s.entitlementRepo.Update(ctx, ent.Entitlement); err != nil {
						return err
					}
				} else {
					// Delete entitlement not in request
					entsToDelete = append(entsToDelete, ent.ID)
				}
			}

			// Delete entitlements in bulk
			if len(entsToDelete) > 0 {
				if err := s.entitlementRepo.DeleteBulk(ctx, entsToDelete); err != nil {
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
				if _, err := s.entitlementRepo.CreateBulk(ctx, newEntitlements); err != nil {
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

	entitlements, err := s.entitlementRepo.List(ctx, &types.EntitlementFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		PlanIDs:     []string{id},
	})

	if err != nil {
		return err
	}

	subscriptions, err := s.subscriptionRepo.List(ctx, &types.SubscriptionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		PlanID:      id,
	})

	if err != nil {
		return err
	}

	if len(entitlements) > 0 {
		return ierr.NewError("plan is still associated with entitlements or subscriptions").
			WithHint("This plan has associated entitlements. Please remove the entitlements before deleting the plan").
			WithReportableDetails(map[string]interface{}{
				"plan_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	if len(subscriptions) > 0 {
		return ierr.NewError("plan is still associated with subscriptions").
			WithHint("This plan has active subscriptions associated with it. Please delete the subscriptions before removing the plan.").
			WithReportableDetails(map[string]interface{}{
				"plan_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	err = s.planRepo.Delete(ctx, id)
	if err != nil {
		return err
	}
	return nil
}
