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
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
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
	SyncPlanToChargebee(ctx context.Context, plan *plan.Plan, prices []*price.Price) error
}

// Service Structs

// ItemFamilyServiceParams holds dependencies for ItemFamilyService
type ItemFamilyServiceParams struct {
	Client ChargebeeClient
	Logger *logger.Logger
}

// ItemFamilyService handles Chargebee item family operations
type ItemFamilyService struct {
	ItemFamilyServiceParams
}

// ItemServiceParams holds dependencies for ItemService
type ItemServiceParams struct {
	Client ChargebeeClient
	Logger *logger.Logger
}

// ItemService handles Chargebee item operations
type ItemService struct {
	ItemServiceParams
}

// ItemPriceServiceParams holds dependencies for ItemPriceService
type ItemPriceServiceParams struct {
	Client ChargebeeClient
	Logger *logger.Logger
}

// ItemPriceService handles Chargebee item price operations
type ItemPriceService struct {
	ItemPriceServiceParams
}

// PlanSyncServiceParams holds dependencies for PlanSyncService
type PlanSyncServiceParams struct {
	Client                       ChargebeeClient
	EntityIntegrationMappingRepo entityintegrationmapping.Repository
	MeterRepo                    meter.Repository
	FeatureRepo                  feature.Repository
	Logger                       *logger.Logger
}

// PlanSyncService handles synchronization of FlexPrice plans to Chargebee
type PlanSyncService struct {
	PlanSyncServiceParams
	itemFamilyService ChargebeeItemFamilyService
	itemService       ChargebeeItemService
	itemPriceService  ChargebeeItemPriceService
}

// =============================================================================
// Constructors
// =============================================================================

// NewItemFamilyService creates a new Chargebee item family service
func NewItemFamilyService(params ItemFamilyServiceParams) ChargebeeItemFamilyService {
	return &ItemFamilyService{
		ItemFamilyServiceParams: params,
	}
}

// NewItemService creates a new Chargebee item service
func NewItemService(params ItemServiceParams) ChargebeeItemService {
	return &ItemService{
		ItemServiceParams: params,
	}
}

// NewItemPriceService creates a new Chargebee item price service
func NewItemPriceService(params ItemPriceServiceParams) ChargebeeItemPriceService {
	return &ItemPriceService{
		ItemPriceServiceParams: params,
	}
}

// NewPlanSyncService creates a new Chargebee plan sync service
func NewPlanSyncService(params PlanSyncServiceParams) ChargebeePlanSyncService {
	// Initialize dependent services
	itemFamilyService := NewItemFamilyService(ItemFamilyServiceParams{
		Client: params.Client,
		Logger: params.Logger,
	})
	itemService := NewItemService(ItemServiceParams{
		Client: params.Client,
		Logger: params.Logger,
	})
	itemPriceService := NewItemPriceService(ItemPriceServiceParams{
		Client: params.Client,
		Logger: params.Logger,
	})

	return &PlanSyncService{
		PlanSyncServiceParams: params,
		itemFamilyService:     itemFamilyService,
		itemService:           itemService,
		itemPriceService:      itemPriceService,
	}
}

// =============================================================================
// Item Family Operations
// =============================================================================

// CreateItemFamily creates a new item family in Chargebee
func (s *ItemFamilyService) CreateItemFamily(ctx context.Context, req *ItemFamilyCreateRequest) (*ItemFamilyResponse, error) {
	s.Logger.Infow("creating item family in Chargebee",
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
	result, err := s.Client.CreateItemFamily(ctx, createParams)
	if err != nil {
		s.Logger.Errorw("failed to create item family in Chargebee",
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

	s.Logger.Infow("successfully created item family in Chargebee",
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
	s.Logger.Infow("listing item families from Chargebee")

	// List all item families using client wrapper
	result, err := s.Client.ListItemFamilies(ctx, &itemfamily.ListRequestParams{
		Limit: lo.ToPtr(int32(100)), // Get up to 100 families
	})

	if err != nil {
		s.Logger.Errorw("failed to list item families from Chargebee",
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

	s.Logger.Infow("successfully listed item families from Chargebee",
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

	s.Logger.Infow("found latest item family",
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
	s.Logger.Infow("creating item in Chargebee",
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
	result, err := s.Client.CreateItem(ctx, createParams)
	if err != nil {
		s.Logger.Errorw("failed to create item in Chargebee",
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

	s.Logger.Infow("successfully created item in Chargebee",
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
	s.Logger.Infow("retrieving item from Chargebee",
		"item_id", itemID)

	// Retrieve item using client wrapper
	result, err := s.Client.RetrieveItem(ctx, itemID)
	if err != nil {
		s.Logger.Errorw("failed to retrieve item from Chargebee",
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

	s.Logger.Infow("successfully retrieved item from Chargebee",
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
	s.Logger.Infow("creating item price in Chargebee",
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
	result, err := s.Client.CreateItemPrice(ctx, createParams)
	if err != nil {
		s.Logger.Errorw("failed to create item price in Chargebee",
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

	s.Logger.Infow("successfully created item price in Chargebee",
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
	s.Logger.Infow("retrieving item price from Chargebee",
		"item_price_id", itemPriceID)

	// Retrieve item price using client wrapper
	result, err := s.Client.RetrieveItemPrice(ctx, itemPriceID)
	if err != nil {
		s.Logger.Errorw("failed to retrieve item price from Chargebee",
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

	s.Logger.Infow("successfully retrieved item price from Chargebee",
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
func mapPricingModel(p *price.Price) string {
	switch p.BillingModel {
	case types.BILLING_MODEL_PACKAGE:
		return "package"

	case types.BILLING_MODEL_TIERED:
		// Tiered with VOLUME mode → Chargebee "volume"
		// Tiered with SLAB mode → Chargebee "tiered"
		if p.TierMode == types.BILLING_TIER_VOLUME {
			return "volume"
		}
		return "tiered"

	case types.BILLING_MODEL_FLAT_FEE:
		// FLAT_FEE with USAGE type → "per_unit"
		// FLAT_FEE with FIXED type → "flat_fee"
		if p.Type == types.PRICE_TYPE_USAGE {
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
func (s *PlanSyncService) getDisplayNameForPrice(ctx context.Context, p *price.Price, planName string) string {
	// If price has no meter, it's a plan price - use plan name
	if p.MeterID == "" {
		return planName
	}

	meterID := p.MeterID

	// Try to get meter
	meter, err := s.MeterRepo.GetMeter(ctx, meterID)
	if err != nil {
		s.Logger.Debugw("failed to get meter for price, using plan name",
			"price_id", p.ID,
			"meter_id", meterID,
			"error", err)
		return planName
	}

	// Try to get feature from meter
	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.MeterIDs = []string{meterID}
	features, err := s.FeatureRepo.List(ctx, featureFilter)
	if err != nil || len(features) == 0 {
		s.Logger.Debugw("failed to get feature for meter, using meter name",
			"price_id", p.ID,
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
func (s *PlanSyncService) SyncPlanToChargebee(ctx context.Context, plan *plan.Plan, prices []*price.Price) error {
	s.Logger.Infow("syncing plan to Chargebee",
		"plan_id", plan.ID,
		"plan_name", plan.Name,
		"prices_count", len(prices))

	// Step 1: Get or select the latest item family
	itemFamily, err := s.itemFamilyService.GetLatestItemFamily(ctx)
	if err != nil {
		s.Logger.Errorw("failed to get item family from Chargebee",
			"plan_id", plan.ID,
			"error", err)
		return ierr.WithError(err).
			WithHint("Failed to get item family from Chargebee. Please create an item family first").
			Mark(ierr.ErrNotFound)
	}

	s.Logger.Infow("using item family for plan",
		"plan_id", plan.ID,
		"item_family_id", itemFamily.ID,
		"item_family_name", itemFamily.Name)

	// Step 2: Create a separate charge item and item price for each price
	// This is required because Chargebee only allows one item price per currency per item
	successCount := 0
	for _, p := range prices {
		// Create unique item for this price
		// Format: charge_{uuid}
		uniqueUUID := types.GenerateUUID()
		itemID := fmt.Sprintf("charge_%s", uniqueUUID)

		// Get display name (feature name if price has meter, otherwise plan name)
		displayName := s.getDisplayNameForPrice(ctx, p, plan.Name)

		// Use display name as external name for better invoice display
		// Format: {display_name} - {currency} (e.g., "API Calls - USD" or "Pro Plan - USD")
		// This makes invoices more readable than showing price_id
		externalName := fmt.Sprintf("%s - %s", displayName, p.Currency)

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
			s.Logger.Errorw("failed to create item in Chargebee",
				"plan_id", plan.ID,
				"price_id", p.ID,
				"item_id", itemID,
				"error", err)
			// Continue with other prices even if one fails
			continue
		}

		s.Logger.Infow("successfully created item in Chargebee",
			"plan_id", plan.ID,
			"price_id", p.ID,
			"item_id", item.ID)

		// Create item price for this price
		// Item price ID should just be the FlexPrice price ID
		itemPriceID := p.ID

		// Map FlexPrice pricing model to Chargebee pricing model
		pricingModel := mapPricingModel(p)

		// Use display name as external name for better invoice display
		// Same format as item external_name: {display_name} - {currency}
		itemPriceExternalName := fmt.Sprintf("%s - %s", displayName, p.Currency)

		// Build item price request
		itemPriceReq := &ItemPriceCreateRequest{
			ID:           itemPriceID,
			ItemID:       item.ID,
			Name:         itemPriceID,
			ExternalName: itemPriceExternalName, // Customer-facing name on invoices
			PricingModel: pricingModel,
			CurrencyCode: p.Currency,
			Description:  plan.Description,
		}

		// Set price or tiers based on billing model
		switch p.BillingModel {
		case types.BILLING_MODEL_TIERED:
			// For tiered/volume pricing, send tiers array
			// Convert domain tiers to types.PriceTier slice
			tiers := make([]*types.PriceTier, len(p.Tiers))
			for i := range p.Tiers {
				tiers[i] = &types.PriceTier{
					UpTo:       p.Tiers[i].UpTo,
					UnitAmount: p.Tiers[i].UnitAmount,
					FlatAmount: p.Tiers[i].FlatAmount,
				}
			}
			itemPriceReq.Tiers = convertTiersForChargebee(tiers, p.Currency)
			s.Logger.Infow("syncing tiered pricing to Chargebee",
				"price_id", p.ID,
				"tier_mode", p.TierMode,
				"tier_count", len(p.Tiers))

		case types.BILLING_MODEL_PACKAGE:
			// For package pricing, set price and optionally period
			itemPriceReq.Price = convertAmountToSmallestUnit(p.Amount.InexactFloat64(), p.Currency)
			if p.TransformQuantity.DivideBy > 0 {
				divideBy := p.TransformQuantity.DivideBy
				itemPriceReq.Period = &divideBy
			}

		case types.BILLING_MODEL_FLAT_FEE:
			// For flat fee, just set the price
			itemPriceReq.Price = convertAmountToSmallestUnit(p.Amount.InexactFloat64(), p.Currency)

		default:
			// Default to flat fee
			itemPriceReq.Price = convertAmountToSmallestUnit(p.Amount.InexactFloat64(), p.Currency)
		}

		itemPrice, err := s.itemPriceService.CreateItemPrice(ctx, itemPriceReq)
		if err != nil {
			s.Logger.Errorw("failed to create item price in Chargebee",
				"plan_id", plan.ID,
				"price_id", p.ID,
				"item_price_id", itemPriceID,
				"item_id", item.ID,
				"error", err)
			// Continue with other prices even if one fails
			continue
		}

		s.Logger.Infow("successfully created item price in Chargebee",
			"plan_id", plan.ID,
			"price_id", p.ID,
			"item_price_id", itemPrice.ID,
			"item_id", item.ID,
			"amount", itemPrice.Price,
			"currency", itemPrice.CurrencyCode)

		// Save entity mapping for price -> item_price
		// Only one entry per price: FlexPrice price ID -> Chargebee item price ID
		mapping := &entityintegrationmapping.EntityIntegrationMapping{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
			EntityType:       types.IntegrationEntityTypeItemPrice,
			EntityID:         p.ID,
			ProviderType:     string(types.SecretProviderChargebee),
			ProviderEntityID: itemPrice.ID,
			EnvironmentID:    p.EnvironmentID,
			BaseModel:        types.GetDefaultBaseModel(ctx),
			Metadata: map[string]interface{}{
				"chargebee_charge_item_id": item.ID,
			},
		}
		// Override tenant_id from price (price has TenantID in BaseModel)
		mapping.TenantID = p.TenantID

		err = s.EntityIntegrationMappingRepo.Create(ctx, mapping)
		if err != nil {
			s.Logger.Errorw("failed to save price entity mapping",
				"price_id", p.ID,
				"chargebee_item_price_id", itemPrice.ID,
				"chargebee_item_id", item.ID,
				"error", err)
			// Don't fail the entire operation, just log the error
		}

		successCount++
	}

	s.Logger.Infow("successfully synced plan to Chargebee",
		"plan_id", plan.ID,
		"total_prices", len(prices),
		"successfully_synced", successCount)

	return nil
}
