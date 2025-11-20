package chargebee

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/chargebee/chargebee-go/v3/enum"
	"github.com/chargebee/chargebee-go/v3/models/item"
	itemEnum "github.com/chargebee/chargebee-go/v3/models/item/enum"
	"github.com/chargebee/chargebee-go/v3/models/itemfamily"
	"github.com/chargebee/chargebee-go/v3/models/itemprice"
	itempriceEnum "github.com/chargebee/chargebee-go/v3/models/itemprice/enum"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// ChargebeeItemFamilyService defines the interface for Chargebee item family operations
type ChargebeeItemFamilyService interface {
	CreateItemFamily(ctx context.Context, req *ItemFamilyCreateRequest) (*ItemFamilyResponse, error)
	ListItemFamilies(ctx context.Context) ([]*ItemFamilyResponse, error)
	GetLatestItemFamily(ctx context.Context) (*ItemFamilyResponse, error)
}

// ChargebeeItemService defines the interface for Chargebee item operations
type ChargebeeItemService interface {
	CreateItem(ctx context.Context, req *ItemCreateRequest) (*ItemResponse, error)
	RetrieveItem(ctx context.Context, itemID string) (*ItemResponse, error)
}

// ChargebeeItemPriceService defines the interface for Chargebee item price operations
type ChargebeeItemPriceService interface {
	CreateItemPrice(ctx context.Context, req *ItemPriceCreateRequest) (*ItemPriceResponse, error)
	RetrieveItemPrice(ctx context.Context, itemPriceID string) (*ItemPriceResponse, error)
}

// ChargebeePlanSyncService defines the interface for Chargebee plan synchronization
type ChargebeePlanSyncService interface {
	SyncPlanToChargebee(ctx context.Context, plan *ent.Plan, prices []*ent.Price) error
}

// Service Structs

// ItemFamilyService handles Chargebee item family operations
type ItemFamilyService struct {
	client ChargebeeClient
	logger *logger.Logger
}

// ItemService handles Chargebee item operations
type ItemService struct {
	client ChargebeeClient
	logger *logger.Logger
}

// ItemPriceService handles Chargebee item price operations
type ItemPriceService struct {
	client ChargebeeClient
	logger *logger.Logger
}

// PlanSyncService handles synchronization of FlexPrice plans to Chargebee
type PlanSyncService struct {
	client                       ChargebeeClient
	itemFamilyService            ChargebeeItemFamilyService
	itemService                  ChargebeeItemService
	itemPriceService             ChargebeeItemPriceService
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	meterRepo                    meter.Repository
	featureRepo                  feature.Repository
	logger                       *logger.Logger
}

// =============================================================================
// Constructors
// =============================================================================

// NewItemFamilyService creates a new Chargebee item family service
func NewItemFamilyService(
	client ChargebeeClient,
	logger *logger.Logger,
) ChargebeeItemFamilyService {
	return &ItemFamilyService{
		client: client,
		logger: logger,
	}
}

// NewItemService creates a new Chargebee item service
func NewItemService(
	client ChargebeeClient,
	logger *logger.Logger,
) ChargebeeItemService {
	return &ItemService{
		client: client,
		logger: logger,
	}
}

// NewItemPriceService creates a new Chargebee item price service
func NewItemPriceService(
	client ChargebeeClient,
	logger *logger.Logger,
) ChargebeeItemPriceService {
	return &ItemPriceService{
		client: client,
		logger: logger,
	}
}

// NewPlanSyncService creates a new Chargebee plan sync service
func NewPlanSyncService(
	client ChargebeeClient,
	itemFamilyService ChargebeeItemFamilyService,
	itemService ChargebeeItemService,
	itemPriceService ChargebeeItemPriceService,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	meterRepo meter.Repository,
	featureRepo feature.Repository,
	logger *logger.Logger,
) ChargebeePlanSyncService {
	return &PlanSyncService{
		client:                       client,
		itemFamilyService:            itemFamilyService,
		itemService:                  itemService,
		itemPriceService:             itemPriceService,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		meterRepo:                    meterRepo,
		featureRepo:                  featureRepo,
		logger:                       logger,
	}
}

// =============================================================================
// Item Family Operations
// =============================================================================

// CreateItemFamily creates a new item family in Chargebee
func (s *ItemFamilyService) CreateItemFamily(ctx context.Context, req *ItemFamilyCreateRequest) (*ItemFamilyResponse, error) {
	s.logger.Infow("creating item family in Chargebee",
		"family_id", req.ID,
		"name", req.Name)

	// Prepare request params
	createParams := &itemfamily.CreateRequestParams{
		Id:   req.ID,
		Name: req.Name,
	}

	if req.Description != "" {
		createParams.Description = req.Description
	}

	// Create item family using client wrapper
	result, err := s.client.CreateItemFamily(ctx, createParams)
	if err != nil {
		s.logger.Errorw("failed to create item family in Chargebee",
			"family_id", req.ID,
			"error", err)
		return nil, ierr.NewError("failed to create item family in Chargebee").
			WithReportableDetails(map[string]interface{}{
				"error":     err.Error(),
				"family_id": req.ID,
			}).
			WithHint("Check Chargebee API credentials and item family data").
			Mark(ierr.ErrValidation)
	}

	itemFamily := result.ItemFamily

	s.logger.Infow("successfully created item family in Chargebee",
		"family_id", itemFamily.Id,
		"name", itemFamily.Name)

	// Convert to our DTO format
	familyResponse := &ItemFamilyResponse{
		ID:              itemFamily.Id,
		Name:            itemFamily.Name,
		Description:     itemFamily.Description,
		Status:          string(itemFamily.Status),
		ResourceVersion: itemFamily.ResourceVersion,
		UpdatedAt:       time.Unix(itemFamily.UpdatedAt, 0),
	}

	return familyResponse, nil
}

// ListItemFamilies retrieves all item families from Chargebee
func (s *ItemFamilyService) ListItemFamilies(ctx context.Context) ([]*ItemFamilyResponse, error) {
	s.logger.Infow("listing item families from Chargebee")

	// List all item families using client wrapper
	result, err := s.client.ListItemFamilies(ctx, &itemfamily.ListRequestParams{
		Limit: lo.ToPtr(int32(100)), // Get up to 100 families
	})

	if err != nil {
		s.logger.Errorw("failed to list item families from Chargebee",
			"error", err)
		return nil, ierr.NewError("failed to list item families from Chargebee").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			WithHint("Check Chargebee API credentials").
			Mark(ierr.ErrValidation)
	}

	// Convert to our DTO format
	families := make([]*ItemFamilyResponse, 0, len(result.List))
	for _, entry := range result.List {
		itemFamily := entry.ItemFamily
		families = append(families, &ItemFamilyResponse{
			ID:              itemFamily.Id,
			Name:            itemFamily.Name,
			Description:     itemFamily.Description,
			Status:          string(itemFamily.Status),
			ResourceVersion: itemFamily.ResourceVersion,
			UpdatedAt:       time.Unix(itemFamily.UpdatedAt, 0),
		})
	}

	s.logger.Infow("successfully listed item families from Chargebee",
		"count", len(families))

	return families, nil
}

// GetLatestItemFamily retrieves the most recently updated item family
func (s *ItemFamilyService) GetLatestItemFamily(ctx context.Context) (*ItemFamilyResponse, error) {
	families, err := s.ListItemFamilies(ctx)
	if err != nil {
		return nil, err
	}

	if len(families) == 0 {
		return nil, ierr.NewError("no item families found in Chargebee").
			WithHint("Please create an item family first").
			Mark(ierr.ErrNotFound)
	}

	// Find the latest family by UpdatedAt
	latest := families[0]
	for _, family := range families[1:] {
		if family.UpdatedAt.After(latest.UpdatedAt) {
			latest = family
		}
	}

	s.logger.Infow("found latest item family",
		"family_id", latest.ID,
		"name", latest.Name,
		"updated_at", latest.UpdatedAt)

	return latest, nil
}

// =============================================================================
// Item Operations
// =============================================================================

// CreateItem creates a new item in Chargebee
func (s *ItemService) CreateItem(ctx context.Context, req *ItemCreateRequest) (*ItemResponse, error) {
	s.logger.Infow("creating item in Chargebee",
		"item_id", req.ID,
		"name", req.Name,
		"type", req.Type,
		"item_family_id", req.ItemFamilyID)

	// Prepare request params
	createParams := &item.CreateRequestParams{
		Id:              req.ID,
		Name:            req.Name,
		Type:            itemEnum.Type(req.Type),
		ItemFamilyId:    req.ItemFamilyID,
		EnabledInPortal: lo.ToPtr(req.EnabledInPortal),
	}

	if req.Description != "" {
		createParams.Description = req.Description
	}

	if req.ExternalName != "" {
		createParams.ExternalName = req.ExternalName
	}

	// Create item using client wrapper
	result, err := s.client.CreateItem(ctx, createParams)
	if err != nil {
		s.logger.Errorw("failed to create item in Chargebee",
			"item_id", req.ID,
			"error", err)
		return nil, ierr.NewError("failed to create item in Chargebee").
			WithReportableDetails(map[string]interface{}{
				"error":   err.Error(),
				"item_id": req.ID,
			}).
			WithHint("Check Chargebee API credentials and item data").
			Mark(ierr.ErrValidation)
	}

	itemData := result.Item

	s.logger.Infow("successfully created item in Chargebee",
		"item_id", itemData.Id,
		"name", itemData.Name,
		"type", itemData.Type)

	// Convert to our DTO format
	itemResponse := &ItemResponse{
		ID:              itemData.Id,
		Name:            itemData.Name,
		Type:            string(itemData.Type),
		ItemFamilyID:    itemData.ItemFamilyId,
		Description:     itemData.Description,
		ExternalName:    itemData.ExternalName,
		Status:          string(itemData.Status),
		ResourceVersion: itemData.ResourceVersion,
		UpdatedAt:       time.Unix(itemData.UpdatedAt, 0),
	}

	return itemResponse, nil
}

// RetrieveItem retrieves an item from Chargebee
func (s *ItemService) RetrieveItem(ctx context.Context, itemID string) (*ItemResponse, error) {
	s.logger.Infow("retrieving item from Chargebee",
		"item_id", itemID)

	// Retrieve item using client wrapper
	result, err := s.client.RetrieveItem(ctx, itemID)
	if err != nil {
		s.logger.Errorw("failed to retrieve item from Chargebee",
			"item_id", itemID,
			"error", err)
		return nil, ierr.NewError("failed to retrieve item from Chargebee").
			WithReportableDetails(map[string]interface{}{
				"error":   err.Error(),
				"item_id": itemID,
			}).
			WithHint("Check if item exists in Chargebee").
			Mark(ierr.ErrNotFound)
	}

	itemData := result.Item

	s.logger.Infow("successfully retrieved item from Chargebee",
		"item_id", itemData.Id,
		"name", itemData.Name)

	// Convert to our DTO format
	itemResponse := &ItemResponse{
		ID:              itemData.Id,
		Name:            itemData.Name,
		Type:            string(itemData.Type),
		ItemFamilyID:    itemData.ItemFamilyId,
		Description:     itemData.Description,
		ExternalName:    itemData.ExternalName,
		Status:          string(itemData.Status),
		ResourceVersion: itemData.ResourceVersion,
		UpdatedAt:       time.Unix(itemData.UpdatedAt, 0),
	}

	return itemResponse, nil
}

// =============================================================================
// Item Price Operations
// =============================================================================

// CreateItemPrice creates a new item price in Chargebee
func (s *ItemPriceService) CreateItemPrice(ctx context.Context, req *ItemPriceCreateRequest) (*ItemPriceResponse, error) {
	s.logger.Infow("creating item price in Chargebee",
		"item_price_id", req.ID,
		"item_id", req.ItemID,
		"pricing_model", req.PricingModel,
		"price", req.Price,
		"currency", req.CurrencyCode,
		"has_tiers", len(req.Tiers) > 0)

	// Prepare request params
	createParams := &itemprice.CreateRequestParams{
		Id:           req.ID,
		ItemId:       req.ItemID,
		Name:         req.Name,
		PricingModel: enum.PricingModel(req.PricingModel),
		CurrencyCode: req.CurrencyCode,
	}

	// Only set Price for non-tiered models
	if len(req.Tiers) == 0 {
		createParams.Price = lo.ToPtr(req.Price)
	}

	if req.ExternalName != "" {
		createParams.ExternalName = req.ExternalName
	}

	if req.Description != "" {
		createParams.Description = req.Description
	}

	// Add tiers for tiered/volume pricing
	if len(req.Tiers) > 0 {
		createParams.Tiers = make([]*itemprice.CreateTierParams, len(req.Tiers))
		for i, tier := range req.Tiers {
			createParams.Tiers[i] = &itemprice.CreateTierParams{
				StartingUnit: lo.ToPtr(int32(tier.StartingUnit)),
				Price:        lo.ToPtr(int64(tier.Price)),
			}
			if tier.EndingUnit != nil {
				createParams.Tiers[i].EndingUnit = lo.ToPtr(int32(*tier.EndingUnit))
			}
		}
	}

	// Add period for package pricing
	if req.Period != nil {
		createParams.Period = lo.ToPtr(int32(*req.Period))
	}
	if req.PeriodUnit != "" {
		itemPriceEnum := itempriceEnum.PeriodUnit(req.PeriodUnit)
		createParams.PeriodUnit = itemPriceEnum
	}

	// Create item price using client wrapper
	result, err := s.client.CreateItemPrice(ctx, createParams)
	if err != nil {
		s.logger.Errorw("failed to create item price in Chargebee",
			"item_price_id", req.ID,
			"item_id", req.ItemID,
			"error", err)
		return nil, ierr.NewError("failed to create item price in Chargebee").
			WithReportableDetails(map[string]interface{}{
				"error":         err.Error(),
				"item_price_id": req.ID,
				"item_id":       req.ItemID,
			}).
			WithHint("Check Chargebee API credentials and item price data").
			Mark(ierr.ErrValidation)
	}

	itemPrice := result.ItemPrice

	s.logger.Infow("successfully created item price in Chargebee",
		"item_price_id", itemPrice.Id,
		"item_id", itemPrice.ItemId,
		"pricing_model", string(itemPrice.PricingModel),
		"price", itemPrice.Price,
		"currency", itemPrice.CurrencyCode)

	// Convert to our DTO format
	itemPriceResponse := &ItemPriceResponse{
		ID:              itemPrice.Id,
		ItemID:          itemPrice.ItemId,
		Name:            itemPrice.Name,
		ExternalName:    itemPrice.ExternalName,
		PricingModel:    string(itemPrice.PricingModel),
		Price:           itemPrice.Price,
		CurrencyCode:    itemPrice.CurrencyCode,
		Description:     itemPrice.Description,
		Status:          string(itemPrice.Status),
		ResourceVersion: itemPrice.ResourceVersion,
		UpdatedAt:       time.Unix(itemPrice.UpdatedAt, 0),
	}

	return itemPriceResponse, nil
}

// RetrieveItemPrice retrieves an item price from Chargebee
func (s *ItemPriceService) RetrieveItemPrice(ctx context.Context, itemPriceID string) (*ItemPriceResponse, error) {
	s.logger.Infow("retrieving item price from Chargebee",
		"item_price_id", itemPriceID)

	// Retrieve item price using client wrapper
	result, err := s.client.RetrieveItemPrice(ctx, itemPriceID)
	if err != nil {
		s.logger.Errorw("failed to retrieve item price from Chargebee",
			"item_price_id", itemPriceID,
			"error", err)
		return nil, ierr.NewError("failed to retrieve item price from Chargebee").
			WithReportableDetails(map[string]interface{}{
				"error":         err.Error(),
				"item_price_id": itemPriceID,
			}).
			WithHint("Check if item price exists in Chargebee").
			Mark(ierr.ErrNotFound)
	}

	itemPrice := result.ItemPrice

	s.logger.Infow("successfully retrieved item price from Chargebee",
		"item_price_id", itemPrice.Id,
		"item_id", itemPrice.ItemId)

	// Convert to our DTO format
	itemPriceResponse := &ItemPriceResponse{
		ID:              itemPrice.Id,
		ItemID:          itemPrice.ItemId,
		Name:            itemPrice.Name,
		ExternalName:    itemPrice.ExternalName,
		PricingModel:    string(itemPrice.PricingModel),
		Price:           itemPrice.Price,
		CurrencyCode:    itemPrice.CurrencyCode,
		Description:     itemPrice.Description,
		Status:          string(itemPrice.Status),
		ResourceVersion: itemPrice.ResourceVersion,
		UpdatedAt:       time.Unix(itemPrice.UpdatedAt, 0),
	}

	return itemPriceResponse, nil
}

// =============================================================================
// Plan Sync Operations
// =============================================================================

// convertAmountToSmallestUnit converts an amount to the smallest currency unit
// based on the currency's decimal places (e.g., USD: cents, JPY: yen)
func convertAmountToSmallestUnit(amount float64, currency string) int64 {
	precision := types.GetCurrencyPrecision(currency)
	multiplier := math.Pow(10, float64(precision))
	return int64(math.Round(amount * multiplier))
}

// mapPricingModel maps FlexPrice billing model to Chargebee pricing model
func mapPricingModel(price *ent.Price) string {
	switch types.BillingModel(price.BillingModel) {
	case types.BILLING_MODEL_PACKAGE:
		return "package"

	case types.BILLING_MODEL_TIERED:
		// Tiered with VOLUME mode → Chargebee "volume"
		// Tiered with SLAB mode → Chargebee "tiered"
		if price.TierMode != nil && *price.TierMode == string(types.BILLING_TIER_VOLUME) {
			return "volume"
		}
		return "tiered"

	case types.BILLING_MODEL_FLAT_FEE:
		// FLAT_FEE with USAGE type → "per_unit"
		// FLAT_FEE with FIXED type → "flat_fee"
		if price.Type == string(types.PRICE_TYPE_USAGE) {
			return "per_unit"
		}
		return "flat_fee"

	default:
		return "flat_fee"
	}
}

// convertTiersForChargebee converts FlexPrice tiers to Chargebee tier format
func convertTiersForChargebee(flexPriceTiers []*types.PriceTier, currency string) []ChargebeeTier {
	if len(flexPriceTiers) == 0 {
		return nil
	}

	chargebeeTiers := make([]ChargebeeTier, len(flexPriceTiers))
	startingUnit := int64(1) // Chargebee requires starting_unit to be at least 1

	for i, tier := range flexPriceTiers {
		// Convert unit amount to smallest currency unit
		priceInt := convertAmountToSmallestUnit(tier.UnitAmount.InexactFloat64(), currency)

		chargebeeTier := ChargebeeTier{
			StartingUnit: startingUnit,
			Price:        priceInt,
		}

		// Set ending unit (nil for last tier)
		if tier.UpTo != nil {
			endingUnit := int64(*tier.UpTo)
			chargebeeTier.EndingUnit = &endingUnit
			startingUnit = endingUnit + 1 // Next tier starts after current tier ends
		} else {
			chargebeeTier.EndingUnit = nil // Last tier has no ending unit
		}

		chargebeeTiers[i] = chargebeeTier
	}

	return chargebeeTiers
}

// getDisplayNameForPrice returns the display name for a price
// If price has a MeterID (feature price), returns feature name
// Otherwise returns plan name
func (s *PlanSyncService) getDisplayNameForPrice(ctx context.Context, price *ent.Price, planName string) string {
	// If price has no meter, it's a plan price - use plan name
	if price.MeterID == nil || *price.MeterID == "" {
		return planName
	}

	meterID := *price.MeterID

	// Try to get meter
	meter, err := s.meterRepo.GetMeter(ctx, meterID)
	if err != nil {
		s.logger.Debugw("failed to get meter for price, using plan name",
			"price_id", price.ID,
			"meter_id", meterID,
			"error", err)
		return planName
	}

	// Try to get feature from meter
	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.MeterIDs = []string{meterID}
	features, err := s.featureRepo.List(ctx, featureFilter)
	if err != nil || len(features) == 0 {
		s.logger.Debugw("failed to get feature for meter, using meter name",
			"price_id", price.ID,
			"meter_id", meterID,
			"error", err)
		// Fallback to meter name if feature not found
		if meter != nil && meter.Name != "" {
			return meter.Name
		}
		return planName
	}

	// Use feature name (first feature found)
	if len(features) > 0 && features[0].Name != "" {
		return features[0].Name
	}

	// Fallback to meter name
	if meter != nil && meter.Name != "" {
		return meter.Name
	}

	// Final fallback to plan name
	return planName
}

// SyncPlanToChargebee syncs a FlexPrice plan and its prices to Chargebee
// Creates a separate charge item for each price to avoid currency conflicts
func (s *PlanSyncService) SyncPlanToChargebee(ctx context.Context, plan *ent.Plan, prices []*ent.Price) error {
	s.logger.Infow("syncing plan to Chargebee",
		"plan_id", plan.ID,
		"plan_name", plan.Name,
		"prices_count", len(prices))

	// Step 1: Get or select the latest item family
	itemFamily, err := s.itemFamilyService.GetLatestItemFamily(ctx)
	if err != nil {
		s.logger.Errorw("failed to get item family from Chargebee",
			"plan_id", plan.ID,
			"error", err)
		return ierr.WithError(err).
			WithHint("Failed to get item family from Chargebee. Please create an item family first").
			Mark(ierr.ErrNotFound)
	}

	s.logger.Infow("using item family for plan",
		"plan_id", plan.ID,
		"item_family_id", itemFamily.ID,
		"item_family_name", itemFamily.Name)

	// Step 2: Create a separate charge item and item price for each price
	// This is required because Chargebee only allows one item price per currency per item
	successCount := 0
	for _, price := range prices {
		// Create unique item for this price
		// Format: charge_{uuid}
		uniqueUUID := types.GenerateUUID()
		itemID := fmt.Sprintf("charge_%s", uniqueUUID)

		// Get display name (feature name if price has meter, otherwise plan name)
		displayName := s.getDisplayNameForPrice(ctx, price, plan.Name)

		// Use display name as external name for better invoice display
		// Format: {display_name} - {currency} (e.g., "API Calls - USD" or "Pro Plan - USD")
		// This makes invoices more readable than showing price_id
		externalName := fmt.Sprintf("%s - %s", displayName, price.Currency)

		itemReq := &ItemCreateRequest{
			ID:              itemID,
			Name:            itemID,   // Name matches ID format (must be unique)
			Type:            "charge", // Charge type for one-time/recurring charges
			ItemFamilyID:    itemFamily.ID,
			Description:     plan.Description,
			ExternalName:    externalName, // Customer-facing name on invoices
			EnabledInPortal: true,
		}

		item, err := s.itemService.CreateItem(ctx, itemReq)
		if err != nil {
			s.logger.Errorw("failed to create item in Chargebee",
				"plan_id", plan.ID,
				"price_id", price.ID,
				"item_id", itemID,
				"error", err)
			// Continue with other prices even if one fails
			continue
		}

		s.logger.Infow("successfully created item in Chargebee",
			"plan_id", plan.ID,
			"price_id", price.ID,
			"item_id", item.ID)

		// Create item price for this price
		// Item price ID should just be the FlexPrice price ID
		itemPriceID := price.ID

		// Map FlexPrice pricing model to Chargebee pricing model
		pricingModel := mapPricingModel(price)

		// Use display name as external name for better invoice display
		// Same format as item external_name: {display_name} - {currency}
		itemPriceExternalName := fmt.Sprintf("%s - %s", displayName, price.Currency)

		// Build item price request
		itemPriceReq := &ItemPriceCreateRequest{
			ID:           itemPriceID,
			ItemID:       item.ID,
			Name:         itemPriceID,
			ExternalName: itemPriceExternalName, // Customer-facing name on invoices
			PricingModel: pricingModel,
			CurrencyCode: price.Currency,
			Description:  plan.Description,
		}

		// Set price or tiers based on billing model
		switch types.BillingModel(price.BillingModel) {
		case types.BILLING_MODEL_TIERED:
			// For tiered/volume pricing, send tiers array
			itemPriceReq.Tiers = convertTiersForChargebee(price.Tiers, price.Currency)
			s.logger.Infow("syncing tiered pricing to Chargebee",
				"price_id", price.ID,
				"tier_mode", price.TierMode,
				"tier_count", len(price.Tiers))

		case types.BILLING_MODEL_PACKAGE:
			// For package pricing, set price and optionally period
			itemPriceReq.Price = convertAmountToSmallestUnit(price.Amount.InexactFloat64(), price.Currency)
			if price.TransformQuantity.DivideBy > 0 {
				divideBy := price.TransformQuantity.DivideBy
				itemPriceReq.Period = &divideBy
			}

		case types.BILLING_MODEL_FLAT_FEE:
			// For flat fee, just set the price
			itemPriceReq.Price = convertAmountToSmallestUnit(price.Amount.InexactFloat64(), price.Currency)

		default:
			// Default to flat fee
			itemPriceReq.Price = convertAmountToSmallestUnit(price.Amount.InexactFloat64(), price.Currency)
		}

		itemPrice, err := s.itemPriceService.CreateItemPrice(ctx, itemPriceReq)
		if err != nil {
			s.logger.Errorw("failed to create item price in Chargebee",
				"plan_id", plan.ID,
				"price_id", price.ID,
				"item_price_id", itemPriceID,
				"item_id", item.ID,
				"error", err)
			// Continue with other prices even if one fails
			continue
		}

		s.logger.Infow("successfully created item price in Chargebee",
			"plan_id", plan.ID,
			"price_id", price.ID,
			"item_price_id", itemPrice.ID,
			"item_id", item.ID,
			"amount", itemPrice.Price,
			"currency", itemPrice.CurrencyCode)

		// Save entity mapping for price -> item_price
		// Only one entry per price: FlexPrice price ID -> Chargebee item price ID
		mapping := &entityintegrationmapping.EntityIntegrationMapping{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
			EntityType:       types.IntegrationEntityTypeItemPrice,
			EntityID:         price.ID,
			ProviderType:     string(types.SecretProviderChargebee),
			ProviderEntityID: itemPrice.ID,
			EnvironmentID:    price.EnvironmentID,
			BaseModel:        types.GetDefaultBaseModel(ctx),
			Metadata: map[string]interface{}{
				"chargebee_charge_item_id": item.ID,
			},
		}
		// Override tenant_id from price (price has TenantID in BaseModel)
		mapping.TenantID = price.TenantID

		err = s.entityIntegrationMappingRepo.Create(ctx, mapping)
		if err != nil {
			s.logger.Errorw("failed to save price entity mapping",
				"price_id", price.ID,
				"chargebee_item_price_id", itemPrice.ID,
				"chargebee_item_id", item.ID,
				"error", err)
			// Don't fail the entire operation, just log the error
		}

		successCount++
	}

	s.logger.Infow("successfully synced plan to Chargebee",
		"plan_id", plan.ID,
		"total_prices", len(prices),
		"successfully_synced", successCount)

	return nil
}
