package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type PriceService interface {
	CreatePrice(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error)
	CreateBulkPrice(ctx context.Context, req dto.CreateBulkPriceRequest) (*dto.CreateBulkPriceResponse, error)
	GetPrice(ctx context.Context, id string) (*dto.PriceResponse, error)
	GetPricesByPlanID(ctx context.Context, planID string) (*dto.ListPricesResponse, error)
	GetPricesBySubscriptionID(ctx context.Context, subscriptionID string) (*dto.ListPricesResponse, error)
	GetPricesByAddonID(ctx context.Context, addonID string) (*dto.ListPricesResponse, error)
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

	// Handle entity type validation
	if req.EntityType != "" {
		if err := req.EntityType.Validate(); err != nil {
			return nil, err
		}

		if req.EntityID == "" {
			return nil, ierr.NewError("entity_id is required when entity_type is provided").
				WithHint("Please provide an entity id").
				Mark(ierr.ErrValidation)
		}

		// Validate that the entity exists based on entity type
		if !req.SkipEntityValidation {
			if err := s.validateEntityExists(ctx, req.EntityType, req.EntityID); err != nil {
				return nil, err
			}
		}
	} else {
		// Legacy support for plan_id
		if req.PlanID == "" {
			return nil, ierr.NewError("either entity_type/entity_id or plan_id is required").
				WithHint("Please provide entity_type and entity_id, or plan_id for backward compatibility").
				Mark(ierr.ErrValidation)
		}
		// Set entity type and ID from plan_id for backward compatibility
		req.EntityType = types.PRICE_ENTITY_TYPE_PLAN
		req.EntityID = req.PlanID
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

	if err := s.PriceRepo.Create(ctx, price); err != nil {
		return nil, err
	}

	response := &dto.PriceResponse{Price: price}

	// TODO: !REMOVE after migration
	if price.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
		response.PlanID = price.EntityID
	}

	return response, nil
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

		// Separate prices that need price unit config handling from regular prices
		var regularPrices []*price.Price
		var priceUnitConfigPrices []dto.CreatePriceRequest

		for _, priceReq := range req.Items {
			if priceReq.PriceUnitConfig != nil {
				priceUnitConfigPrices = append(priceUnitConfigPrices, priceReq)
			} else {
				// Handle regular prices
				price, err := priceReq.ToPrice(txCtx)
				if err != nil {
					return ierr.WithError(err).
						WithHint("Failed to create price").
						Mark(ierr.ErrValidation)
				}
				regularPrices = append(regularPrices, price)
			}
		}

		// Create regular prices in bulk if any exist
		if len(regularPrices) > 0 {
			if err := s.PriceRepo.CreateBulk(txCtx, regularPrices); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to create prices in bulk").
					Mark(ierr.ErrDatabase)
			}

			// Add successful regular prices to response
			for _, p := range regularPrices {
				priceResp := &dto.PriceResponse{Price: p}
				if p.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
					priceResp.PlanID = p.EntityID
				}
				response.Items = append(response.Items, priceResp)
			}
		}

		// Handle price unit config prices individually (they need special processing)
		for _, priceReq := range priceUnitConfigPrices {
			priceResp, err := s.createPriceWithUnitConfig(txCtx, priceReq)
			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to create price with unit config").
					Mark(ierr.ErrValidation)
			}
			response.Items = append(response.Items, priceResp)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return response, nil
}

// createPriceWithUnitConfig- a private helper method to create a price with a price unit config
func (s *priceService) createPriceWithUnitConfig(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error) {

	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Parse price unit amount - this is the amount in the price unit currency
	priceUnitAmount := decimal.Zero
	if req.BillingModel != types.BILLING_MODEL_TIERED {
		if req.PriceUnitConfig.Amount == "" {
			return nil, ierr.NewError("price_unit_config.amount is required when billing model is not TIERED").
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
	}

	// Fetch the price unit by code, tenant, and environment
	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)
	priceUnit, err := s.PriceUnitRepo.GetByCode(ctx, req.PriceUnitConfig.PriceUnit, tenantID, envID, string(types.StatusPublished))
	if err != nil || priceUnit == nil {
		return nil, ierr.NewError("invalid or unpublished price unit").
			WithHint("Price unit must exist and be published").
			Mark(ierr.ErrValidation)
	}

	// Convert FROM price unit TO base currency
	baseAmount, err := s.PriceUnitRepo.ConvertToBaseCurrency(ctx, req.PriceUnitConfig.PriceUnit, tenantID, envID, priceUnitAmount)
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
			convertedUnitAmount, err := s.PriceUnitRepo.ConvertToBaseCurrency(ctx, req.PriceUnitConfig.PriceUnit, tenantID, envID, unitAmount)
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
				convertedFlatAmount, err := s.PriceUnitRepo.ConvertToBaseCurrency(ctx, req.PriceUnitConfig.PriceUnit, tenantID, envID, parsed)
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
		EntityType:         req.EntityType,
		EntityID:           req.EntityID,
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

	if err := s.PriceRepo.Create(ctx, p); err != nil {
		return nil, err
	}

	response := &dto.PriceResponse{Price: p}

	// TODO: !REMOVE after migration
	if p.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
		response.PlanID = p.EntityID
	}

	return response, nil
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

	price, err := s.PriceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	response := &dto.PriceResponse{Price: price}

	// Set entity information
	response.EntityType = price.EntityType
	response.EntityID = price.EntityID

	// TODO: !REMOVE after migration
	if price.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
		response.PlanID = price.EntityID
	}

	if price.MeterID != "" {
		meterService := NewMeterService(s.MeterRepo)
		meter, err := meterService.GetMeter(ctx, price.MeterID)
		if err != nil {
			s.Logger.Warnw("failed to fetch meter", "meter_id", price.MeterID, "error", err)
			return nil, err
		}
		response.Meter = dto.ToMeterResponse(meter)
	}

	if price.PriceUnitID != "" {
		priceUnit, err := s.PriceUnitRepo.GetByID(ctx, price.PriceUnitID)
		if err != nil {
			s.Logger.Warnw("failed to fetch price unit", "price_unit_id", price.PriceUnitID, "error", err)
			return nil, err
		}
		response.PricingUnit = &dto.PriceUnitResponse{PriceUnit: priceUnit}
	}

	return response, nil
}

func (s *priceService) GetPricesByPlanID(ctx context.Context, planID string) (*dto.ListPricesResponse, error) {
	if planID == "" {
		return nil, ierr.NewError("plan_id is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	priceFilter := types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{planID}).
		WithStatus(types.StatusPublished).
		WithEntityType(types.PRICE_ENTITY_TYPE_PLAN).
		WithExpand(string(types.ExpandMeters) + "," + string(types.ExpandPriceUnit))

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
		WithExpand(string(types.ExpandMeters) + "," + string(types.ExpandPriceUnit))

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
		WithExpand(string(types.ExpandMeters) + "," + string(types.ExpandPriceUnit))

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

	// If price units are requested to be expanded, fetch all price units in one query
	var priceUnitsByID map[string]*dto.PriceUnitResponse
	if filter.GetExpand().Has(types.ExpandPriceUnit) && len(prices) > 0 {
		// Collect unique price unit IDs
		priceUnitIDs := make(map[string]bool)
		for _, p := range prices {
			if p.PriceUnitID != "" {
				priceUnitIDs[p.PriceUnitID] = true
			}
		}

		priceUnitsByID = make(map[string]*dto.PriceUnitResponse)
		for priceUnitID := range priceUnitIDs {
			priceUnit, err := s.PriceUnitRepo.GetByID(ctx, priceUnitID)
			if err != nil {
				s.Logger.Warnw("failed to fetch price unit", "price_unit_id", priceUnitID, "error", err)
				continue
			}
			priceUnitsByID[priceUnitID] = &dto.PriceUnitResponse{PriceUnit: priceUnit}
		}

		s.Logger.Debugw("fetched price units for prices", "count", len(priceUnitsByID))
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

		// Add price unit if requested and available
		if filter.GetExpand().Has(types.ExpandPriceUnit) && p.PriceUnitID != "" {
			if pu, ok := priceUnitsByID[p.PriceUnitID]; ok {
				response.Items[i].PricingUnit = pu
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
	price, err := s.PriceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	price.Description = req.Description
	price.Metadata = req.Metadata
	price.LookupKey = req.LookupKey

	if err := s.PriceRepo.Update(ctx, price); err != nil {
		return nil, err
	}

	response := &dto.PriceResponse{Price: price}

	// TODO: !REMOVE after migration
	if price.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
		response.PlanID = price.EntityID
	}

	return response, nil
}

func (s *priceService) DeletePrice(ctx context.Context, id string, req dto.DeletePriceRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}

	price, err := s.PriceRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	if req.EndDate != nil {
		price.EndDate = req.EndDate
	} else {
		price.EndDate = lo.ToPtr(time.Now().UTC())
	}

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
