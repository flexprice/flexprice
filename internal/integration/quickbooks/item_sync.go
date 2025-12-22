package quickbooks

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// QuickBooksItemSyncService defines the interface for QuickBooks item synchronization
type QuickBooksItemSyncService interface {
	SyncPriceToQuickBooks(ctx context.Context, plan *plan.Plan, priceToSync *price.Price) error
}

// ItemSyncServiceParams holds dependencies for ItemSyncService
type ItemSyncServiceParams struct {
	Client                       QuickBooksClient
	EntityIntegrationMappingRepo entityintegrationmapping.Repository
	MeterRepo                    meter.Repository
	Logger                       *logger.Logger
}

// ItemSyncService handles synchronization of Flexprice plans/prices to QuickBooks items
type ItemSyncService struct {
	ItemSyncServiceParams
}

// NewItemSyncService creates a new QuickBooks item sync service
func NewItemSyncService(params ItemSyncServiceParams) QuickBooksItemSyncService {
	return &ItemSyncService{
		ItemSyncServiceParams: params,
	}
}

// SyncPriceToQuickBooks syncs a single Flexprice price to QuickBooks as a Service Item.
// This method is called when a price is created/updated.
// Item naming logic:
// - Usage charge (with meter): "{plan_name}-{meter_name}"
// - Recurring charge (without meter): "{plan_name}-Recurring"
func (s *ItemSyncService) SyncPriceToQuickBooks(ctx context.Context, plan *plan.Plan, priceToSync *price.Price) error {
	s.Logger.Infow("syncing price to QuickBooks",
		"price_id", priceToSync.ID,
		"plan_id", plan.ID,
		"plan_name", plan.Name)

	// Check if price is already synced
	existingItemID, err := s.getQuickBooksItemID(ctx, priceToSync.ID)
	if err == nil && existingItemID != "" {
		s.Logger.Infow("price already synced to QuickBooks",
			"price_id", priceToSync.ID,
			"quickbooks_item_id", existingItemID)
		return nil
	}

	if err != nil && !ierr.IsNotFound(err) {
		return err
	}

	// Get meter if price has meter ID - used for item naming
	var meterItem *meter.Meter
	if priceToSync.MeterID != "" {
		meterItem, err = s.MeterRepo.GetMeter(ctx, priceToSync.MeterID)
		if err != nil {
			s.Logger.Warnw("failed to find meter for price, continuing without meter name",
				"price_id", priceToSync.ID,
				"meter_id", priceToSync.MeterID,
				"error", err)
		}
	}

	// Build item name based on whether it's usage-based (with meter) or recurring (without meter)
	var itemName string
	if meterItem != nil && meterItem.Name != "" {
		// Usage-based price: {plan name}-{meter name}
		itemName = fmt.Sprintf("%s-%s", plan.Name, meterItem.Name)
	} else {
		// Recurring price: {plan name}-Recurring-{price_id}
		// Using price_id ensures each recurring price gets a truly unique item name
		itemName = fmt.Sprintf("%s-Recurring-%s", plan.Name, priceToSync.ID)
	}

	// Check if item already exists by name (avoid duplicates)
	existingItem, err := s.Client.QueryItemByName(ctx, itemName)
	if err == nil && existingItem != nil && existingItem.ID != "" {
		s.Logger.Infow("found existing item in QuickBooks by name",
			"item_name", itemName,
			"quickbooks_item_id", existingItem.ID)
		// Create mapping for existing item
		if err := s.createItemMapping(ctx, priceToSync.ID, existingItem.ID, existingItem.Name, plan.EnvironmentID, plan.TenantID); err != nil {
			s.Logger.Debugw("failed to create item mapping",
				"error", err,
				"price_id", priceToSync.ID)
		}
		return nil
	}

	// Get income account ID (configurable or default to "79")
	incomeAccountID := s.getIncomeAccountID(ctx)

	// Create new item in QuickBooks
	itemReq := &ItemCreateRequest{
		Name:        itemName,
		Type:        string(ItemTypeService),
		Description: priceToSync.ID, // Store price ID as description for reference
		Active:      true,
		IncomeAccountRef: &AccountRef{
			Value: incomeAccountID,
		},
	}

	// Set unit price based on price type:
	// - For usage-based prices: use Amount (per-unit rate)
	// - For recurring prices: use Amount (fixed recurring amount)
	// - For tiered prices: use first tier's unit amount
	var unitPrice decimal.Decimal

	s.Logger.Infow("determining unit price for QuickBooks item",
		"price_id", priceToSync.ID,
		"price_type", priceToSync.Type,
		"billing_model", priceToSync.BillingModel)

	if priceToSync.Type == types.PRICE_TYPE_USAGE {
		// Usage-based: use per-unit price (stored in Amount)
		unitPrice = priceToSync.Amount
	} else if priceToSync.BillingModel == types.BILLING_MODEL_TIERED && len(priceToSync.Tiers) > 0 {
		// Tiered pricing: use first tier's unit amount as default
		unitPrice = priceToSync.Tiers[0].UnitAmount
		s.Logger.Infow("using first tier unit amount for tiered price", "unit_price", unitPrice)
	} else {
		// Recurring/flat fee: use the fixed amount
		unitPrice = priceToSync.Amount
		s.Logger.Infow("using Amount for recurring/flat-fee price", "unit_price", unitPrice)
	}

	if !unitPrice.IsZero() {
		itemReq.UnitPrice = &unitPrice
		s.Logger.Infow("set UnitPrice on ItemCreateRequest", "unit_price", unitPrice)
	} else {
		s.Logger.Warnw("unit price is zero, not setting UnitPrice on ItemCreateRequest",
			"price_id", priceToSync.ID,
			"price_type", priceToSync.Type)
	}

	itemResp, err := s.Client.CreateItem(ctx, itemReq)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create item in QuickBooks").
			Mark(ierr.ErrInternal)
	}

	s.Logger.Infow("created item in QuickBooks",
		"price_id", priceToSync.ID,
		"quickbooks_item_id", itemResp.ID,
		"item_name", itemResp.Name,
		"income_account_id", incomeAccountID)

	// Create mapping
	if err := s.createItemMapping(ctx, priceToSync.ID, itemResp.ID, itemResp.Name, plan.EnvironmentID, plan.TenantID); err != nil {
		s.Logger.Errorw("failed to create item mapping",
			"error", err,
			"price_id", priceToSync.ID,
			"quickbooks_item_id", itemResp.ID)
		return ierr.WithError(err).
			WithHint("Item created in QuickBooks but mapping failed").
			Mark(ierr.ErrDatabase)
	}

	s.Logger.Infow("successfully synced price to QuickBooks",
		"price_id", priceToSync.ID,
		"quickbooks_item_id", itemResp.ID)

	return nil
}

// getQuickBooksItemID retrieves the QuickBooks item ID from entity mapping.
// Checks if price has already been synced to QuickBooks by looking up the mapping.
// Returns the QuickBooks item ID if mapping exists, or ErrNotFound if price hasn't been synced.
func (s *ItemSyncService) getQuickBooksItemID(ctx context.Context, priceID string) (string, error) {
	filter := types.NewEntityIntegrationMappingFilter()
	filter.EntityType = types.IntegrationEntityTypePrice
	filter.EntityID = priceID
	filter.ProviderTypes = []string{string(types.SecretProviderQuickBooks)}

	mappings, err := s.EntityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to query entity mapping for item").
			WithReportableDetails(map[string]interface{}{
				"price_id": priceID,
			}).
			Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		return "", ierr.NewError("item not synced to QuickBooks").
			WithHint("Item not synced to QuickBooks").
			WithReportableDetails(map[string]interface{}{
				"price_id": priceID,
			}).
			Mark(ierr.ErrNotFound)
	}

	return mappings[0].ProviderEntityID, nil
}

// createItemMapping creates an entity integration mapping for the item.
// Maps Flexprice price_id to QuickBooks item ID for use in invoice line items.
// Stores item name in metadata for reference and debugging.
func (s *ItemSyncService) createItemMapping(
	ctx context.Context,
	priceID string,
	quickBooksItemID string,
	itemName string,
	environmentID string,
	tenantID string,
) error {
	baseModel := types.GetDefaultBaseModel(ctx)
	baseModel.TenantID = tenantID

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityType:       types.IntegrationEntityTypePrice,
		EntityID:         priceID,
		ProviderType:     string(types.SecretProviderQuickBooks),
		ProviderEntityID: quickBooksItemID,
		EnvironmentID:    environmentID,
		BaseModel:        baseModel,
		Metadata: map[string]interface{}{
			"synced_at":            time.Now().UTC().Format(time.RFC3339),
			"quickbooks_item_name": itemName,
		},
	}

	return s.EntityIntegrationMappingRepo.Create(ctx, mapping)
}

// getIncomeAccountID retrieves the income account ID from connection metadata or returns default "79".
// This allows companies to configure their own income account ID while maintaining a sensible default.
// The default "79" is the standard Service Income account in QuickBooks Sandbox.
func (s *ItemSyncService) getIncomeAccountID(ctx context.Context) string {
	const defaultIncomeAccountID = "79"

	// Try to get connection to check for custom income account ID
	conn, err := s.Client.GetConnection(ctx)
	if err != nil {
		// If connection not found or error, use default
		s.Logger.Debugw("could not get QuickBooks connection, using default income account ID",
			"default_account_id", defaultIncomeAccountID,
			"error", err)
		return defaultIncomeAccountID
	}

	// Check if connection has custom income account ID in metadata
	if conn.EncryptedSecretData.QuickBooks != nil {
		if incomeAccountID := conn.EncryptedSecretData.QuickBooks.IncomeAccountID; incomeAccountID != "" {
			s.Logger.Debugw("using custom income account ID from connection",
				"income_account_id", incomeAccountID,
				"connection_id", conn.ID)
			return incomeAccountID
		}
	}

	// No custom account ID configured, use default
	s.Logger.Debugw("no custom income account ID configured, using default Service Revenue account",
		"default_account_id", defaultIncomeAccountID,
		"connection_id", conn.ID,
		"hint", "Set 'income_account_id' in connection metadata to use a different account")
	return defaultIncomeAccountID
}
