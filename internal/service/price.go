package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type PriceService interface {
	CreatePrice(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error)
	GetPrice(ctx context.Context, id string) (*dto.PriceResponse, error)
	GetPricesByPlanID(ctx context.Context, planID string) (*dto.ListPricesResponse, error)
	GetPrices(ctx context.Context, filter types.PriceFilter) (*dto.ListPricesResponse, error)
	UpdatePrice(ctx context.Context, id string, req dto.UpdatePriceRequest) (*dto.PriceResponse, error)
	DeletePrice(ctx context.Context, id string) error
	CalculateCost(ctx context.Context, price *price.Price, quantity decimal.Decimal) decimal.Decimal
}

type priceService struct {
	repo      price.Repository
	meterRepo meter.Repository
	logger    *logger.Logger
}

func NewPriceService(repo price.Repository, meterRepo meter.Repository, logger *logger.Logger) PriceService {
	return &priceService{repo: repo, logger: logger, meterRepo: meterRepo}
}

func (s *priceService) CreatePrice(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	if req.PlanID == "" {
		return nil, fmt.Errorf("plan_id is required")
	}

	price, err := req.ToPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to parse price: %w", err)
	}

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

func (s *priceService) GetPricesByPlanID(ctx context.Context, planID string) (*dto.ListPricesResponse, error) {
	// Use unlimited filter to fetch all prices
	priceFilter := types.NewUnlimitedPriceFilter().
		WithPlanIDs([]string{planID}).
		WithStatus(types.StatusPublished).
		WithExpand(string(types.ExpandMeters))

	return s.GetPrices(ctx, priceFilter)
}

func (s *priceService) GetPrices(ctx context.Context, filter types.PriceFilter) (*dto.ListPricesResponse, error) {
	// Validate expand fields
	if err := filter.GetExpand().Validate(types.PriceExpandConfig); err != nil {
		return nil, fmt.Errorf("invalid expand fields: %w", err)
	}

	// Fetch prices
	prices, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list prices: %w", err)
	}

	response := &dto.ListPricesResponse{
		Prices: make([]dto.PriceResponse, len(prices)),
	}

	// If meters are requested to be expanded, fetch all meters in one query
	var metersByID map[string]*meter.Meter
	if filter.GetExpand().Has(types.ExpandMeters) && len(prices) > 0 {
		// Fetch all meters in one query
		meters, err := s.meterRepo.GetAllMeters(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch meters: %w", err)
		}

		// Create a map for quick meter lookup
		metersByID = make(map[string]*meter.Meter, len(meters))
		for _, m := range meters {
			metersByID[m.ID] = m
		}

		s.logger.Debugw("fetched meters for prices", "count", len(meters))
	}

	// Build response with expanded fields
	for i, p := range prices {
		response.Prices[i] = dto.PriceResponse{Price: p}

		// Add meter if requested and available
		if filter.GetExpand().Has(types.ExpandMeters) && p.MeterID != "" {
			if m, ok := metersByID[p.MeterID]; ok {
				response.Prices[i].Meter = dto.ToMeterResponse(m)
			}
		}
	}

	response.Total = len(prices)
	response.Offset = filter.GetOffset()
	response.Limit = filter.GetLimit()

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
// returns the cost in main currency units (e.g., 1.00 = $1.00)
func (s *priceService) CalculateCost(ctx context.Context, price *price.Price, quantity decimal.Decimal) decimal.Decimal {
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

		transformedQuantity := quantity.Div(decimal.NewFromInt(int64(price.TransformQuantity.DivideBy)))

		if price.TransformQuantity.Round == types.ROUND_UP {
			transformedQuantity = transformedQuantity.Ceil()
		} else if price.TransformQuantity.Round == types.ROUND_DOWN {
			transformedQuantity = transformedQuantity.Floor()
		}

		cost = price.CalculateAmount(transformedQuantity)

	case types.BILLING_MODEL_TIERED:
		cost = s.calculateTieredCost(ctx, price, quantity)
	}

	finalCost := cost.Round(types.GetCurrencyPrecision(price.Currency))
	return finalCost
}

// calculateTieredCost calculates cost for tiered pricing
func (s *priceService) calculateTieredCost(ctx context.Context, price *price.Price, quantity decimal.Decimal) decimal.Decimal {
	cost := decimal.Zero
	if len(price.Tiers) == 0 {
		s.logger.WithContext(ctx).Errorf("no tiers found for price %s", price.ID)
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
		for i, tier := range price.Tiers {
			if tier.UpTo == nil {
				selectedTierIndex = i
				break
			}
			if quantity.LessThan(decimal.NewFromUint64(*tier.UpTo)) {
				selectedTierIndex = i
				break
			}
		}

		selectedTier := price.Tiers[selectedTierIndex]

		// Calculate tier cost with proper rounding and handling of flat amount
		tierCost := selectedTier.CalculateTierAmount(quantity, price.Currency)

		s.logger.WithContext(ctx).Debugf(
			"volume tier total cost for quantity %s: %s price: %s tier : %+v",
			quantity.String(),
			tierCost.String(),
			price.ID,
			selectedTier,
		)

		cost = cost.Add(tierCost)

	case types.BILLING_TIER_SLAB:
		remainingQuantity := quantity
		for _, tier := range price.Tiers {
			var tierQuantity = remainingQuantity
			if tier.UpTo != nil {
				upTo := decimal.NewFromUint64(*tier.UpTo)
				if remainingQuantity.GreaterThan(upTo) {
					tierQuantity = upTo
				}
			}

			// Calculate tier cost with proper rounding and handling of flat amount
			tierCost := tier.CalculateTierAmount(tierQuantity, price.Currency)
			cost = cost.Add(tierCost)
			remainingQuantity = remainingQuantity.Sub(tierQuantity)

			s.logger.WithContext(ctx).Debugf(
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
		s.logger.WithContext(ctx).Errorf("invalid tier mode: %s", price.TierMode)
		return decimal.Zero
	}

	return cost
}
