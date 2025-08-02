package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/priceunit"
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

	// CalculateCostWithBreakup calculates the cost for a given price and quantity
	// and returns detailed information about the calculation
	CalculateCostWithBreakup(ctx context.Context, price *price.Price, quantity decimal.Decimal, round bool) dto.CostBreakup

	// CalculateCostSheetPrice calculates the cost for a given price and quantity
	// specifically for costsheet calculations
	CalculateCostSheetPrice(ctx context.Context, price *price.Price, quantity decimal.Decimal) decimal.Decimal
}

type priceService struct {
	repo          price.Repository
	meterRepo     meter.Repository
	logger        *logger.Logger
	priceUnitRepo priceunit.Repository
}

func NewPriceService(repo price.Repository, meterRepo meter.Repository, priceUnitRepo priceunit.Repository, logger *logger.Logger) PriceService {
	return &priceService{repo: repo, logger: logger, meterRepo: meterRepo, priceUnitRepo: priceUnitRepo}
}

func (s *priceService) CreatePrice(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if req.PlanID == "" {
		return nil, ierr.NewError("plan_id is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	// Handle price unit config case
	if req.PriceUnitConfig != nil {
		return s.createPriceWithUnitConfig(ctx, req)
	}

	// Handle regular price case
	price, err := req.ToPrice(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse price data").
			Mark(ierr.ErrValidation)
	}

	if err := s.repo.Create(ctx, price); err != nil {
		return nil, err
	}

	return &dto.PriceResponse{Price: price}, nil
}

// createPriceWithUnitConfig- a private helper method to create a price with a price unit config
func (s *priceService) createPriceWithUnitConfig(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error) {

	if err := req.Validate(); err != nil {
		return nil, err
	}

	if req.PlanID == "" {
		return nil, ierr.NewError("plan_id is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	// Parse price unit amount - this is the amount in the price unit currency
	priceUnitAmount := decimal.Zero
	if req.PriceUnitConfig.Amount == "" {
		return nil, ierr.NewError("price_unit_config.amount is required").
			WithHint("Amount in price unit currency is required for price unit config").
			Mark(ierr.ErrValidation)
	}

	var err error
	priceUnitAmount, err = decimal.NewFromString(req.PriceUnitConfig.Amount)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Price unit amount must be a valid decimal number").
			WithReportableDetails(map[string]interface{}{"amount": req.PriceUnitConfig.Amount}).
			Mark(ierr.ErrValidation)
	}

	// Fetch the price unit by code, tenant, and environment
	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)
	priceUnit, err := s.priceUnitRepo.GetByCode(ctx, req.PriceUnitConfig.PriceUnit, tenantID, envID, string(types.StatusPublished))
	if err != nil || priceUnit == nil {
		return nil, ierr.NewError("invalid or unpublished price unit").
			WithHint("Price unit must exist and be published").
			Mark(ierr.ErrValidation)
	}

	// Convert FROM price unit TO base currency
	baseAmount, err := s.priceUnitRepo.ConvertToBaseCurrency(ctx, req.PriceUnitConfig.PriceUnit, tenantID, envID, priceUnitAmount)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to convert price unit amount to base currency").
			Mark(ierr.ErrInternal)
	}

	// Round to the price unit's precision
	priceUnitAmount = priceUnitAmount.Round(int32(priceUnit.Precision))

	// Format display price unit amount
	displayPriceUnitAmount := formatDisplayPriceUnitAmount(priceUnitAmount, priceUnit.Precision, priceUnit.Symbol)

	// Build the price model
	metadata := make(map[string]string)
	if req.Metadata != nil {
		metadata = req.Metadata
	}

	var transformQuantity price.JSONBTransformQuantity
	if req.TransformQuantity != nil {
		transformQuantity = price.JSONBTransformQuantity(*req.TransformQuantity)
	}

	var tiers price.JSONBTiers
	var priceUnitTiers price.JSONBTiers
	if req.PriceUnitConfig != nil && req.PriceUnitConfig.PriceUnitTiers != nil {
		// Process price unit tiers - convert amounts from price unit to base currency
		priceTiers := make([]price.PriceTier, len(req.PriceUnitConfig.PriceUnitTiers))
		priceUnitTiers = make(price.JSONBTiers, len(req.PriceUnitConfig.PriceUnitTiers))
		for i, tier := range req.PriceUnitConfig.PriceUnitTiers {
			// Parse the tier unit amount (in price unit currency)
			unitAmount, err := decimal.NewFromString(tier.UnitAmount)
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Tier unit amount must be a valid decimal number").
					WithReportableDetails(map[string]interface{}{"unit_amount": tier.UnitAmount}).
					Mark(ierr.ErrValidation)
			}

			// Store original price unit tier
			priceUnitTiers[i] = price.PriceTier{
				UpTo:       tier.UpTo,
				UnitAmount: unitAmount,
			}

			// Convert tier unit amount from price unit to base currency
			convertedUnitAmount, err := s.priceUnitRepo.ConvertToBaseCurrency(ctx, req.PriceUnitConfig.PriceUnit, tenantID, envID, unitAmount)
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to convert tier unit amount to base currency").
					WithReportableDetails(map[string]interface{}{
						"tier_index":  i,
						"unit_amount": tier.UnitAmount,
						"price_unit":  req.PriceUnitConfig.PriceUnit,
					}).
					Mark(ierr.ErrInternal)
			}

			var flatAmount *decimal.Decimal
			var priceUnitFlatAmount *decimal.Decimal
			if tier.FlatAmount != nil {
				// Parse the tier flat amount (in price unit currency)
				parsed, err := decimal.NewFromString(*tier.FlatAmount)
				if err != nil {
					return nil, ierr.WithError(err).
						WithHint("Tier flat amount must be a valid decimal number").
						WithReportableDetails(map[string]interface{}{"flat_amount": tier.FlatAmount}).
						Mark(ierr.ErrValidation)
				}

				// Store original price unit flat amount
				priceUnitFlatAmount = &parsed
				priceUnitTiers[i].FlatAmount = priceUnitFlatAmount

				// Convert tier flat amount from price unit to base currency
				convertedFlatAmount, err := s.priceUnitRepo.ConvertToBaseCurrency(ctx, req.PriceUnitConfig.PriceUnit, tenantID, envID, parsed)
				if err != nil {
					return nil, ierr.WithError(err).
						WithHint("Failed to convert tier flat amount to base currency").
						WithReportableDetails(map[string]interface{}{
							"tier_index":  i,
							"flat_amount": tier.FlatAmount,
							"price_unit":  req.PriceUnitConfig.PriceUnit,
						}).
						Mark(ierr.ErrInternal)
				}
				flatAmount = &convertedFlatAmount
			}

			priceTiers[i] = price.PriceTier{
				UpTo:       tier.UpTo,
				UnitAmount: convertedUnitAmount, // Store converted amount
				FlatAmount: flatAmount,          // Store converted flat amount
			}
		}
		tiers = price.JSONBTiers(priceTiers)
	} else if req.Tiers != nil {
		// Process regular tiers (when not using price unit config)
		priceTiers := make([]price.PriceTier, len(req.Tiers))
		for i, tier := range req.Tiers {
			unitAmount, err := decimal.NewFromString(tier.UnitAmount)
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Unit amount must be a valid decimal number").
					WithReportableDetails(map[string]interface{}{"unit_amount": tier.UnitAmount}).
					Mark(ierr.ErrValidation)
			}
			var flatAmount *decimal.Decimal
			if tier.FlatAmount != nil {
				parsed, err := decimal.NewFromString(*tier.FlatAmount)
				if err != nil {
					return nil, ierr.WithError(err).
						WithHint("Flat amount must be a valid decimal number").
						WithReportableDetails(map[string]interface{}{"flat_amount": tier.FlatAmount}).
						Mark(ierr.ErrValidation)
				}
				flatAmount = &parsed
			}
			priceTiers[i] = price.PriceTier{
				UpTo:       tier.UpTo,
				UnitAmount: unitAmount,
				FlatAmount: flatAmount,
			}
		}
		tiers = price.JSONBTiers(priceTiers)
	}

	p := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             baseAmount,
		Currency:           req.Currency,
		PriceUnitType:      req.PriceUnitType,
		PlanID:             req.PlanID,
		Type:               req.Type,
		BillingPeriod:      req.BillingPeriod,
		BillingPeriodCount: req.BillingPeriodCount,
		BillingModel:       req.BillingModel,
		BillingCadence:     req.BillingCadence,
		InvoiceCadence:     req.InvoiceCadence,
		TrialPeriod:        req.TrialPeriod,
		MeterID:            req.MeterID,
		LookupKey:          req.LookupKey,
		Description:        req.Description,
		Metadata:           metadata,
		TierMode:           req.TierMode,
		Tiers:              tiers,
		PriceUnitTiers:     priceUnitTiers,
		TransformQuantity:  transformQuantity,
		EnvironmentID:      envID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
		// Price unit fields - set all from the fetched price unit
		PriceUnit:              priceUnit.Code,
		PriceUnitID:            priceUnit.ID,
		PriceUnitAmount:        priceUnitAmount,
		DisplayPriceUnitAmount: displayPriceUnitAmount,
		ConversionRate:         priceUnit.ConversionRate,
	}
	p.DisplayAmount = p.GetDisplayAmount()

	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}

	return &dto.PriceResponse{Price: p}, nil
}

// Helper to format display price unit amount
func formatDisplayPriceUnitAmount(amount decimal.Decimal, precision int, symbol string) string {
	// Round the amount to the specified precision
	roundedAmount := amount.Round(int32(precision))
	// Convert to float64 for proper formatting
	amountFloat := roundedAmount.InexactFloat64()
	return fmt.Sprintf("%s%.*f", symbol, precision, amountFloat)
}

func (s *priceService) GetPrice(ctx context.Context, id string) (*dto.PriceResponse, error) {
	if id == "" {
		return nil, ierr.NewError("price_id is required").
			WithHint("Price ID is required").
			Mark(ierr.ErrValidation)
	}

	price, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
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
	meterService := NewMeterService(s.meterRepo)

	// Validate expand fields
	if err := filter.GetExpand().Validate(types.PriceExpandConfig); err != nil {
		return nil, err
	}

	// Get prices
	prices, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	priceCount, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Build response
	response := &dto.ListPricesResponse{
		Items: make([]*dto.PriceResponse, len(prices)),
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

		s.logger.Debugw("fetched meters for prices", "count", len(metersResponse.Items))
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
	}

	response.Pagination = types.NewPaginationResponse(
		priceCount,
		filter.GetLimit(),
		filter.GetOffset(),
	)

	return response, nil
}

func (s *priceService) UpdatePrice(ctx context.Context, id string, req dto.UpdatePriceRequest) (*dto.PriceResponse, error) {
	price, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	price.Description = req.Description
	price.Metadata = req.Metadata
	price.LookupKey = req.LookupKey

	if err := s.repo.Update(ctx, price); err != nil {
		return nil, err
	}

	return &dto.PriceResponse{Price: price}, nil
}

func (s *priceService) DeletePrice(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	return nil
}

// CalculateCost calculates the cost for a given price and quantity
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

		// Calculate tier cost with full precision and handling of flat amount
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

			// Calculate tier cost with full precision and handling of flat amount
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
		s.logger.WithContext(ctx).Errorf("no tiers found for price %s", price.ID)
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

		s.logger.WithContext(ctx).Debugf(
			"volume tier total cost for quantity %s: %s price: %s tier : %+v",
			quantity.String(),
			result.FinalCost.String(),
			price.ID,
			selectedTier,
		)

	case types.BILLING_TIER_SLAB:
		remainingQuantity := quantity
		for i, tier := range price.Tiers {
			var tierQuantity = remainingQuantity
			if tier.UpTo != nil {
				upTo := decimal.NewFromUint64(*tier.UpTo)
				if remainingQuantity.GreaterThan(upTo) {
					tierQuantity = upTo
				}
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

		// Calculate effective unit cost (handle zero quantity case)
		if !quantity.IsZero() {
			result.EffectiveUnitCost = result.FinalCost.Div(quantity)
		} else {
			result.EffectiveUnitCost = decimal.Zero
		}
	default:
		s.logger.WithContext(ctx).Errorf("invalid tier mode: %s", price.TierMode)
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
