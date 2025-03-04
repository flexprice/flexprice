package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type PlanService interface {
	CreatePlan(ctx context.Context, req dto.CreatePlanRequest) (*dto.CreatePlanResponse, error)
	GetPlan(ctx context.Context, id string) (*dto.PlanResponse, error)
	GetPlans(ctx context.Context, filter *types.PlanFilter) (*dto.ListPlansResponse, error)
	UpdatePlan(ctx context.Context, id string, req dto.UpdatePlanRequest) (*dto.PlanResponse, error)
	DeletePlan(ctx context.Context, id string) error
}

type planService struct {
	planRepo        plan.Repository
	priceRepo       price.Repository
	meterRepo       meter.Repository
	entitlementRepo entitlement.Repository
	featureRepo     feature.Repository
	client          postgres.IClient
	logger          *logger.Logger
}

func NewPlanService(
	client postgres.IClient,
	planRepo plan.Repository,
	priceRepo price.Repository,
	meterRepo meter.Repository,
	entitlementRepo entitlement.Repository,
	featureRepo feature.Repository,
	logger *logger.Logger,
) PlanService {
	return &planService{
		client:          client,
		planRepo:        planRepo,
		priceRepo:       priceRepo,
		meterRepo:       meterRepo,
		entitlementRepo: entitlementRepo,
		featureRepo:     featureRepo,
		logger:          logger,
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
			return err // Repository already returns properly formatted errors
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
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve plan").
			WithReportableDetails(map[string]interface{}{
				"plan_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Get prices for the plan
	prices, err := s.priceRepo.ListAll(ctx, types.NewNoLimitPriceFilter().WithPlanIDs([]string{plan.ID}))
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve plan prices").
			WithReportableDetails(map[string]interface{}{
				"plan_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Get entitlements for the plan
	entitlements, err := s.entitlementRepo.ListAll(ctx, types.NewNoLimitEntitlementFilter().WithPlanIDs([]string{plan.ID}))
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve plan entitlements").
			WithReportableDetails(map[string]interface{}{
				"plan_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Get features for the entitlements
	featureIDs := make([]string, 0, len(entitlements))
	for _, e := range entitlements {
		featureIDs = append(featureIDs, e.FeatureID)
	}

	var features []*feature.Feature
	if len(featureIDs) > 0 {
		features, err = s.featureRepo.ListAll(ctx, types.NewNoLimitFeatureFilter().WithIDs(featureIDs))
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to retrieve features").
				WithReportableDetails(map[string]interface{}{
					"plan_id": id,
				}).
				Mark(ierr.ErrDatabase)
		}
	}

	// Get meters for the prices
	meterIDs := make([]string, 0, len(prices))
	for _, p := range prices {
		if p.MeterID != "" {
			meterIDs = append(meterIDs, p.MeterID)
		}
	}

	var meters []*meter.Meter
	if len(meterIDs) > 0 {
		meters, err = s.meterRepo.ListAll(ctx, types.NewNoLimitMeterFilter().WithIDs(meterIDs))
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to retrieve meters").
				WithReportableDetails(map[string]interface{}{
					"plan_id": id,
				}).
				Mark(ierr.ErrDatabase)
		}
	}

	// Build response
	response := &dto.PlanResponse{
		Plan: plan,
	}

	// Add prices to response
	response.Prices = make([]*dto.PriceResponse, len(prices))
	for i, p := range prices {
		priceResp := &dto.PriceResponse{
			Price: p,
		}

		// Add meter to price if it exists
		for _, m := range meters {
			if m.ID == p.MeterID {
				priceResp.Meter = m
				break
			}
		}

		response.Prices[i] = priceResp
	}

	// Add entitlements to response
	response.Entitlements = make([]*dto.EntitlementResponse, len(entitlements))
	for i, e := range entitlements {
		entResp := &dto.EntitlementResponse{
			Entitlement: e,
		}

		// Add feature to entitlement if it exists
		for _, f := range features {
			if f.ID == e.FeatureID {
				entResp.Feature = f
				break
			}
		}

		response.Entitlements[i] = entResp
	}

	return response, nil
}

func (s *planService) GetPlans(ctx context.Context, filter *types.PlanFilter) (*dto.ListPlansResponse, error) {
	if filter == nil {
		filter = types.NewDefaultPlanFilter()
	}

	// Get plans
	plans, err := s.planRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve plans").
			Mark(ierr.ErrDatabase)
	}

	// Get count
	count, err := s.planRepo.Count(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to count plans").
			Mark(ierr.ErrDatabase)
	}

	// Build response
	response := &dto.ListPlansResponse{
		Items: make([]*dto.PlanResponse, len(plans)),
		Pagination: types.PaginationResponse{
			Total:  count,
			Limit:  filter.GetLimit(),
			Offset: filter.GetOffset(),
		},
	}

	// If expand is requested, get related data
	if filter.GetExpand().Has(types.ExpandPrices) || filter.GetExpand().Has(types.ExpandEntitlements) {
		// Get all plan IDs
		planIDs := make([]string, len(plans))
		for i, p := range plans {
			planIDs[i] = p.ID
		}

		// Get prices for all plans
		var prices []*price.Price
		if filter.GetExpand().Has(types.ExpandPrices) {
			prices, err = s.priceRepo.ListAll(ctx, types.NewNoLimitPriceFilter().WithPlanIDs(planIDs))
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to retrieve prices").
					Mark(ierr.ErrDatabase)
			}
		}

		// Get entitlements for all plans
		var entitlements []*entitlement.Entitlement
		if filter.GetExpand().Has(types.ExpandEntitlements) {
			entitlements, err = s.entitlementRepo.ListAll(ctx, types.NewNoLimitEntitlementFilter().WithPlanIDs(planIDs))
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to retrieve entitlements").
					Mark(ierr.ErrDatabase)
			}
		}

		// Get features for all entitlements
		var features []*feature.Feature
		if filter.GetExpand().Has(types.ExpandEntitlements) && len(entitlements) > 0 {
			featureIDs := make([]string, 0, len(entitlements))
			for _, e := range entitlements {
				featureIDs = append(featureIDs, e.FeatureID)
			}

			features, err = s.featureRepo.ListAll(ctx, types.NewNoLimitFeatureFilter().WithIDs(featureIDs))
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to retrieve features").
					Mark(ierr.ErrDatabase)
			}
		}

		// Get meters for all prices
		var meters []*meter.Meter
		if filter.GetExpand().Has(types.ExpandPrices) && len(prices) > 0 {
			meterIDs := make([]string, 0, len(prices))
			for _, p := range prices {
				if p.MeterID != "" {
					meterIDs = append(meterIDs, p.MeterID)
				}
			}

			if len(meterIDs) > 0 {
				meters, err = s.meterRepo.ListAll(ctx, types.NewNoLimitMeterFilter().WithIDs(meterIDs))
				if err != nil {
					return nil, ierr.WithError(err).
						WithHint("Failed to retrieve meters").
						Mark(ierr.ErrDatabase)
				}
			}
		}

		// Build plan responses with expanded data
		for i, plan := range plans {
			planResp := &dto.PlanResponse{
				Plan: plan,
			}

			// Add prices to plan
			if filter.GetExpand().Has(types.ExpandPrices) {
				planPrices := make([]*price.Price, 0)
				for _, p := range prices {
					if p.PlanID == plan.ID {
						planPrices = append(planPrices, p)
					}
				}

				planResp.Prices = make([]*dto.PriceResponse, len(planPrices))
				for j, p := range planPrices {
					priceResp := &dto.PriceResponse{
						Price: p,
					}

					// Add meter to price
					for _, m := range meters {
						if m.ID == p.MeterID {
							priceResp.Meter = m
							break
						}
					}

					planResp.Prices[j] = priceResp
				}
			}

			// Add entitlements to plan
			if filter.GetExpand().Has(types.ExpandEntitlements) {
				planEntitlements := make([]*entitlement.Entitlement, 0)
				for _, e := range entitlements {
					if e.PlanID == plan.ID {
						planEntitlements = append(planEntitlements, e)
					}
				}

				planResp.Entitlements = make([]*dto.EntitlementResponse, len(planEntitlements))
				for j, e := range planEntitlements {
					entResp := &dto.EntitlementResponse{
						Entitlement: e,
					}

					// Add feature to entitlement
					for _, f := range features {
						if f.ID == e.FeatureID {
							entResp.Feature = f
							break
						}
					}

					planResp.Entitlements[j] = entResp
				}
			}

			response.Items[i] = planResp
		}
	} else {
		// Simple response without expanded data
		for i, plan := range plans {
			response.Items[i] = &dto.PlanResponse{
				Plan: plan,
			}
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
	if req.InvoiceCadence != nil {
		plan.InvoiceCadence = *req.InvoiceCadence
	}
	if req.TrialPeriod != nil {
		plan.TrialPeriod = *req.TrialPeriod
	}

	// Start a transaction for updating plan, prices, and entitlements
	err = s.client.WithTx(ctx, func(ctx context.Context) error {
		// 1. Update the plan
		if err := s.planRepo.Update(ctx, plan); err != nil {
			return fmt.Errorf("failed to update plan: %w", err)
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
						return fmt.Errorf("failed to update price: %w", err)
					}
				} else {
					// Delete price not in request
					pricesToDelete = append(pricesToDelete, price.ID)
				}
			}

			// Delete prices in bulk
			if len(pricesToDelete) > 0 {
				if err := s.priceRepo.DeleteBulk(ctx, pricesToDelete); err != nil {
					return fmt.Errorf("failed to delete prices: %w", err)
				}
			}

			// Create new prices
			newPrices := make([]*price.Price, 0)
			for _, reqPrice := range req.Prices {
				if reqPrice.ID == "" {
					newPrice, err := reqPrice.ToPrice(ctx)
					if err != nil {
						return fmt.Errorf("failed to create price: %w", err)
					}
					newPrice.PlanID = plan.ID
					newPrices = append(newPrices, newPrice)
				}
			}

			if len(newPrices) > 0 {
				if err := s.priceRepo.CreateBulk(ctx, newPrices); err != nil {
					return fmt.Errorf("failed to create prices: %w", err)
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
						return fmt.Errorf("failed to update entitlement: %w", err)
					}
				} else {
					// Delete entitlement not in request
					entsToDelete = append(entsToDelete, ent.ID)
				}
			}

			// Delete entitlements in bulk
			if len(entsToDelete) > 0 {
				if err := s.entitlementRepo.DeleteBulk(ctx, entsToDelete); err != nil {
					return fmt.Errorf("failed to delete entitlements: %w", err)
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
					return fmt.Errorf("failed to create entitlements: %w", err)
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
	err := s.planRepo.Delete(ctx, id)
	if err != nil {
		return err
	}
	return nil
}
