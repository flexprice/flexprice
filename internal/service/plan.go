package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

type PlanService interface {
	CreatePlan(ctx context.Context, req dto.CreatePlanRequest) (*dto.CreatePlanResponse, error)
	GetPlan(ctx context.Context, id string) (*dto.PlanResponse, error)
	GetPlans(ctx context.Context, filter types.Filter) (*dto.ListPlansResponse, error)
	UpdatePlan(ctx context.Context, id string, req dto.UpdatePlanRequest) (*dto.PlanResponse, error)
	DeletePlan(ctx context.Context, id string) error
}

type planService struct {
	planRepo  plan.Repository
	priceRepo price.Repository
	meterRepo meter.Repository
	logger    *logger.Logger
}

func NewPlanService(planRepo plan.Repository, priceRepo price.Repository, meterRepo meter.Repository, logger *logger.Logger) PlanService {
	return &planService{planRepo: planRepo, priceRepo: priceRepo, meterRepo: meterRepo, logger: logger}
}

func (s *planService) CreatePlan(ctx context.Context, req dto.CreatePlanRequest) (*dto.CreatePlanResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	plan := req.ToPlan(ctx)
	if err := s.planRepo.Create(ctx, plan); err != nil {
		return nil, fmt.Errorf("failed to create plan: %w", err)
	}

	// TODO: Create prices in bulk
	for _, priceReq := range req.Prices {
		price, err := priceReq.ToPrice(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create price: %w", err)
		}
		price.PlanID = plan.ID
		if err := s.priceRepo.Create(ctx, price); err != nil {
			return nil, fmt.Errorf("failed to create price: %w", err)
		}
	}

	return &dto.CreatePlanResponse{Plan: plan}, nil
}

func (s *planService) GetPlan(ctx context.Context, id string) (*dto.PlanResponse, error) {
	plan, err := s.planRepo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get plan: %w", err)
	}
	priceService := NewPriceService(s.priceRepo, s.meterRepo, s.logger)
	pricesResponse, err := priceService.GetPricesByPlanID(ctx, plan.ID)
	if err != nil {
		s.logger.Errorw("failed to fetch prices for plan", "plan_id", plan.ID, "error", err)
		return nil, err
	}

	response := &dto.PlanResponse{Plan: plan}
	for _, p := range pricesResponse.Prices {
		if p.Price.PlanID == plan.ID {
			response.Prices = append(response.Prices, dto.PriceResponse{Price: p.Price})
		}
	}

	return response, nil
}

func (s *planService) GetPlans(ctx context.Context, filter types.Filter) (*dto.ListPlansResponse, error) {
	priceService := NewPriceService(s.priceRepo, s.meterRepo, s.logger)

	// Fetch plans
	s.logger.Debugw("fetching plans", "filter", filter)
	plans, err := s.planRepo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list plans: %w", err)
	}

	// Prepare response
	response := &dto.ListPlansResponse{
		Plans:  make([]*dto.PlanResponse, len(plans)),
		Total:  len(plans),
		Offset: filter.Offset,
		Limit:  filter.Limit,
	}

	// Create basic plan responses
	for i, plan := range plans {
		response.Plans[i] = &dto.PlanResponse{Plan: plan}
	}

	// If prices expansion is requested, fetch all prices in one query
	if filter.GetExpand().Has(types.ExpandPrices) && len(plans) > 0 {
		// Extract plan IDs
		planIDs := make([]string, len(plans))
		for i, plan := range plans {
			planIDs[i] = plan.ID
		}

		// Create price filter with same status as plan filter and propagate meter expansion
		priceFilter := types.NewUnlimitedPriceFilter().
			WithPlanIDs(planIDs).
			WithStatus(types.StatusPublished)

		// If meters should be expanded, propagate the expansion to prices
		if filter.GetExpand().Has(types.ExpandMeters) {
			priceFilter = priceFilter.WithExpand(string(types.ExpandMeters))
		}

		// Fetch all prices in one query
		s.logger.Debugw("fetching prices for plans",
			"plan_ids", planIDs,
			"expand", priceFilter.GetExpand())
		pricesResponse, err := priceService.GetPrices(ctx, priceFilter)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch prices: %w", err)
		}

		// Create a map for quick price lookup by plan ID
		pricesByPlanID := make(map[string][]dto.PriceResponse)
		for _, p := range pricesResponse.Prices {
			pricesByPlanID[p.Price.PlanID] = append(pricesByPlanID[p.Price.PlanID], p)
		}

		// Assign prices to respective plans
		for i, planResp := range response.Plans {
			if prices, ok := pricesByPlanID[planResp.ID]; ok {
				response.Plans[i].Prices = prices
			}
		}
	}

	return response, nil
}

// UpdatePlan updates a plan and its prices
// TODO: Make this atomic by using a transaction
func (s *planService) UpdatePlan(ctx context.Context, id string, req dto.UpdatePlanRequest) (*dto.PlanResponse, error) {
	planResponse, err := s.GetPlan(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get plan: %w", err)
	}

	plan := planResponse.Plan
	plan.Name = req.Name
	plan.Description = req.Description
	plan.LookupKey = req.LookupKey

	if err := s.planRepo.Update(ctx, plan); err != nil {
		return nil, fmt.Errorf("failed to update plan: %w", err)
	}

	reqPriceMap := make(map[string]*dto.UpdatePlanPriceRequest)
	for _, reqPrice := range req.Prices {
		if reqPrice.ID != "" {
			reqPriceMap[reqPrice.ID] = &reqPrice
		}
	}

	finalPrices := make(map[string]*price.Price)

	// So there can be three cases:
	// 1. The price is in the request and the same ID - update the price
	// 2. The price is in the request but a different ID - create the price
	// 3. The price is not in the request - delete the price
	for _, price := range planResponse.Prices {
		if _, ok := reqPriceMap[price.ID]; ok {
			// Add the price to the final prices
			finalPrices[price.ID] = price.Price

			// Update the price but only the fields that are allowed to be updated
			price.Description = reqPriceMap[price.ID].Description
			price.Metadata = reqPriceMap[price.ID].Metadata
			price.LookupKey = reqPriceMap[price.ID].LookupKey
			if err := s.priceRepo.Update(ctx, price.Price); err != nil {
				return nil, fmt.Errorf("failed to update price: %w", err)
			}
		} else {
			// if existing price is not in the request, delete it
			if err := s.priceRepo.Delete(ctx, price.ID); err != nil {
				return nil, fmt.Errorf("failed to delete price: %w", err)
			}
		}
	}

	// iterate over the request prices and create the ones that are not in the final prices
	for _, reqPrice := range req.Prices {
		s.logger.Infof("reqPrice: %+v", reqPrice)
		if _, ok := finalPrices[reqPrice.ID]; ok {
			continue
		}

		// Create the newly requested price
		newPrice, err := reqPrice.ToPrice(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create price: %w", err)
		}
		newPrice.PlanID = plan.ID
		if err := s.priceRepo.Create(ctx, newPrice); err != nil {
			return nil, fmt.Errorf("failed to create price: %w", err)
		}

		finalPrices[newPrice.ID] = newPrice
	}

	response := &dto.PlanResponse{
		Plan:   plan,
		Prices: make([]dto.PriceResponse, 0, len(finalPrices)),
	}

	for _, price := range finalPrices {
		response.Prices = append(response.Prices, dto.PriceResponse{Price: price})
	}

	return response, nil
}

func (s *planService) DeletePlan(ctx context.Context, id string) error {
	err := s.planRepo.Delete(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete plan: %w", err)
	}
	return nil
}
