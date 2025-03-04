package service

import (
	"context"
	"sort"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type PriceService interface {
	CreatePrice(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error)
	GetPrice(ctx context.Context, id string) (*dto.PriceResponse, error)
	GetPricesByPlanID(ctx context.Context, planID string) (*dto.ListPricesResponse, error)
	GetPrices(ctx context.Context, filter *types.PriceFilter) (*dto.ListPricesResponse, error)
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
		return nil, ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation)
	}

	if req.PlanID == "" {
		return nil, ierr.NewError("plan_id is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	price, err := req.ToPrice(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse price data").
			Mark(ierr.ErrValidation)
	}

	if err := s.repo.Create(ctx, price); err != nil {
		return nil, err // Repository errors are already properly formatted
	}

	return &dto.PriceResponse{Price: price}, nil
}

func (s *priceService) GetPrice(ctx context.Context, id string) (*dto.PriceResponse, error) {
	if id == "" {
		return nil, ierr.NewError("price_id is required").
			WithHint("Price ID is required").
			Mark(ierr.ErrValidation)
	}

	price, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err // Repository errors are already properly formatted
	}

	return &dto.PriceResponse{Price: price}, nil
}

func (s *priceService) GetPricesByPlanID(ctx context.Context, planID string) (*dto.ListPricesResponse, error) {
	if planID == "" {
		return nil, ierr.NewError("plan_id is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	// Use unlimited filter to fetch all prices
	priceFilter := types.NewNoLimitPriceFilter().
		WithPlanIDs([]string{planID}).
		WithStatus(types.StatusPublished).
		WithExpand(string(types.ExpandMeters))

	return s.GetPrices(ctx, priceFilter)
}

func (s *priceService) GetPrices(ctx context.Context, filter *types.PriceFilter) (*dto.ListPricesResponse, error) {
	if filter == nil {
		filter = types.NewDefaultPriceFilter()
	}

	// Validate expand fields
	if err := filter.GetExpand().Validate(types.PriceExpandConfig); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid expand fields").
			Mark(ierr.ErrValidation)
	}

	// Get prices
	prices, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve prices").
			Mark(ierr.ErrDatabase)
	}

	// Get count
	count, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to count prices").
			Mark(ierr.ErrDatabase)
	}

	// Build response
	response := &dto.ListPricesResponse{
		Items: make([]*dto.PriceResponse, len(prices)),
		Pagination: types.PaginationResponse{
			Total:  count,
			Limit:  filter.GetLimit(),
			Offset: filter.GetOffset(),
		},
	}

	// If no prices found, return empty response
	if len(prices) == 0 {
		return response, nil
	}

	// Expand meters if requested
	if filter.GetExpand().Has(types.ExpandMeters) {
		// Collect meter IDs
		meterIDs := make([]string, 0)
		for _, p := range prices {
			if p.MeterID != "" {
				meterIDs = append(meterIDs, p.MeterID)
			}
		}

		// Get meters if needed
		var meters []*meter.Meter
		if len(meterIDs) > 0 {
			meterFilter := types.NewNoLimitMeterFilter()
			meterFilter.IDs = meterIDs

			meters, err = s.meterRepo.ListAll(ctx, meterFilter)
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to retrieve meters").
					Mark(ierr.ErrDatabase)
			}
		}

		// Create meter map for quick lookup
		meterMap := make(map[string]*meter.Meter)
		for _, m := range meters {
			meterMap[m.ID] = m
		}

		// Build response with meters
		for i, p := range prices {
			priceResp := &dto.PriceResponse{
				Price: p,
			}

			// Add meter if available
			if p.MeterID != "" {
				if m, ok := meterMap[p.MeterID]; ok {
					priceResp.Meter = m
				}
			}

			response.Items[i] = priceResp
		}
	} else {
		// Simple response without meters
		for i, p := range prices {
			response.Items[i] = &dto.PriceResponse{
				Price: p,
			}
		}
	}

	return response, nil
}

func (s *priceService) UpdatePrice(ctx context.Context, id string, req dto.UpdatePriceRequest) (*dto.PriceResponse, error) {
	if id == "" {
		return nil, ierr.NewError("price ID is required").
			WithHint("Please provide a valid price ID").
			Mark(ierr.ErrValidation)
	}

	// Validate request
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid price data").
			Mark(ierr.ErrValidation)
	}

	// Get existing price
	existingPrice, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve price").
			WithReportableDetails(map[string]interface{}{
				"price_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Apply updates
	if req.Description != nil {
		existingPrice.Description = *req.Description
	}

	if req.Metadata != nil {
		existingPrice.Metadata = *req.Metadata
	}

	// Update price
	if err := s.repo.Update(ctx, existingPrice); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to update price").
			WithReportableDetails(map[string]interface{}{
				"price_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	return &dto.PriceResponse{Price: existingPrice}, nil
}

func (s *priceService) DeletePrice(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("price ID is required").
			WithHint("Please provide a valid price ID").
			Mark(ierr.ErrValidation)
	}

	// Check if price exists
	_, err := s.repo.Get(ctx, id)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to retrieve price").
			WithReportableDetails(map[string]interface{}{
				"price_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Delete price
	if err := s.repo.Delete(ctx, id); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete price").
			WithReportableDetails(map[string]interface{}{
				"price_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (s *priceService) CalculateCost(ctx context.Context, price *price.Price, quantity decimal.Decimal) decimal.Decimal {
	if price == nil {
		s.logger.Error("Cannot calculate cost for nil price")
		return decimal.Zero
	}

	// For flat fee, just return the amount
	if price.BillingModel == types.BillingModelFlatFee {
		return price.Amount
	}

	// For per unit pricing, multiply by quantity
	if price.BillingModel == types.BillingModelPerUnit {
		return price.Amount.Mul(quantity)
	}

	// For tiered pricing, calculate based on tiers
	if price.BillingModel == types.BillingModelTiered {
		// Apply quantity transformation if needed
		transformedQuantity := quantity
		if price.TransformQuantity.DivideBy > 0 {
			transformedQuantity = quantity.Div(decimal.NewFromInt(int64(price.TransformQuantity.DivideBy)))
			
			// Apply rounding if specified
			if price.TransformQuantity.Round == "up" {
				transformedQuantity = transformedQuantity.Ceil()
			} else if price.TransformQuantity.Round == "down" {
				transformedQuantity = transformedQuantity.Floor()
			}
		}

		// Calculate cost based on tier mode
		if price.TierMode == types.BillingTierGraduated {
			return s.calculateGraduatedTierCost(price, transformedQuantity)
		} else if price.TierMode == types.BillingTierVolume {
			return s.calculateVolumeTierCost(price, transformedQuantity)
		}
	}

	// For package pricing, calculate based on package size
	if price.BillingModel == types.BillingModelPackage {
		// Apply quantity transformation
		transformedQuantity := quantity
		if price.TransformQuantity.DivideBy > 0 {
			transformedQuantity = quantity.Div(decimal.NewFromInt(int64(price.TransformQuantity.DivideBy)))
			
			// Apply rounding if specified
			if price.TransformQuantity.Round == "up" {
				transformedQuantity = transformedQuantity.Ceil()
			} else if price.TransformQuantity.Round == "down" {
				transformedQuantity = transformedQuantity.Floor()
			} else {
				// Default to ceiling for package pricing
				transformedQuantity = transformedQuantity.Ceil()
			}
		}

		// Multiply package count by price
		return price.Amount.Mul(transformedQuantity)
	}

	// Default fallback
	s.logger.Warn("Unsupported billing model, returning zero", "billing_model", price.BillingModel)
	return decimal.Zero
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
