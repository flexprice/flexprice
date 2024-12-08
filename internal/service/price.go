package service

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

type PriceService interface {
	CreatePrice(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error)
	GetPrice(ctx context.Context, id string) (*dto.PriceResponse, error)
	GetPrices(ctx context.Context, filter types.Filter) (*dto.ListPricesResponse, error)
	UpdatePrice(ctx context.Context, id string, req dto.UpdatePriceRequest) (*dto.PriceResponse, error)
	DeletePrice(ctx context.Context, id string) error
	CalculateCost(ctx context.Context, price *price.Price, usage *events.AggregationResult) uint64
}

type priceService struct {
	repo   price.Repository
	logger *logger.Logger
}

func NewPriceService(repo price.Repository, logger *logger.Logger) PriceService {
	return &priceService{repo: repo, logger: logger}
}

func (s *priceService) CreatePrice(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	if req.PlanID == "" {
		return nil, fmt.Errorf("plan_id is required")
	}

	price := req.ToPrice(ctx)

	if err := s.repo.Create(ctx, price); err != nil {
		return nil, fmt.Errorf("failed to create price: %w", err)
	}

	return &dto.PriceResponse{Price: price}, nil
}

func (s *priceService) GetPrice(ctx context.Context, id string) (*dto.PriceResponse, error) {
	price, err := s.repo.Get(ctx, id)
	if err != nil {

		return nil, fmt.Errorf("failed to get price: %w", err)
	}

	return &dto.PriceResponse{Price: price}, nil
}

func (s *priceService) GetPrices(ctx context.Context, filter types.Filter) (*dto.ListPricesResponse, error) {
	prices, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list prices: %w", err)
	}

	response := &dto.ListPricesResponse{
		Prices: make([]dto.PriceResponse, len(prices)),
	}

	for i, p := range prices {
		response.Prices[i] = dto.PriceResponse{Price: p}
	}

	response.Total = len(prices)
	response.Offset = filter.Offset
	response.Limit = filter.Limit

	return response, nil
}

func (s *priceService) UpdatePrice(ctx context.Context, id string, req dto.UpdatePriceRequest) (*dto.PriceResponse, error) {
	price, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get price: %w", err)
	}

	price.Description = req.Description
	price.Metadata = req.Metadata
	price.LookupKey = req.LookupKey

	if err := s.repo.Update(ctx, price); err != nil {
		return nil, fmt.Errorf("failed to update price: %w", err)
	}

	return &dto.PriceResponse{Price: price}, nil
}

func (s *priceService) DeletePrice(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete price: %w", err)
	}
	return nil
}

// CalculateCost calculates the cost for a given price and usage
// returns the cost in cents (e.g. 100 = $1.00)
// TODO : this is the first draft of the cost calculation, it needs to be tested and optimized
func (s *priceService) CalculateCost(ctx context.Context, price *price.Price, usage *events.AggregationResult) uint64 {
	var totalCost uint64
	var quantityInt uint64
	var quantity float64

	switch v := usage.Value.(type) {
	case float64:
		quantity = v
	case uint64:
		quantity = float64(v)
	}

	// round off quantity to the nearest integer and convert to int
	quantityInt = uint64(math.Round(quantity))

	var perUnitCost uint64
	switch price.BillingModel {
	case types.BILLING_MODEL_FLAT_FEE:
		perUnitCost = price.Amount
		totalCost += perUnitCost * quantityInt // multiply by quantity
	case types.BILLING_MODEL_PACKAGE:
		perUnitCost = price.Amount
		perUnitCost /= uint64(price.Transform.DivideBy) // convert to per unit
		totalCost += perUnitCost * quantityInt          // multiply by quantity
	case types.BILLING_MODEL_TIERED:
		switch price.TierMode {
		case types.BILLING_TIER_SLAB:
			remainingQuantity := quantityInt
			for _, tier := range price.Tiers {
				if remainingQuantity <= 0 {
					break
				}

				perUnitCost = uint64(tier.UnitAmount)
				maxQuantityForTier := uint64(tier.GetTierUpTo())

				validQuantityForTier := remainingQuantity
				if validQuantityForTier > maxQuantityForTier {
					validQuantityForTier = maxQuantityForTier
				}

				if validQuantityForTier <= 0 {
					break
				}

				totalCost += perUnitCost * validQuantityForTier // add the cost for this tier

				// subtract the tier quantity from the remaining quantity
				remainingQuantity -= maxQuantityForTier
			}
		case types.BILLING_TIER_VOLUME:
			// Sort price tiers by up_to value
			sort.Slice(price.Tiers, func(i, j int) bool {
				return price.Tiers[i].GetTierUpTo() < price.Tiers[j].GetTierUpTo()
			})

			// Find the tier that the quantity falls into
			for _, tier := range price.Tiers {
				perUnitCost = uint64(tier.UnitAmount)

				if quantityInt <= uint64(tier.GetTierUpTo()) {
					totalCost += quantityInt * perUnitCost // add the cost for this tier
					break
				}
			}
		}
	}

	return totalCost
}
