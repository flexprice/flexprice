package zoho

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// ItemSyncInput holds per-price data needed to create a Zoho item.
type ItemSyncInput struct {
	PriceID string
	Name    string
	Rate    decimal.Decimal
}

// ZohoItemSyncService manages syncing FlexPrice prices as items in Zoho Books.
type ZohoItemSyncService interface {
	// EnsureItemsMapped bulk-queries existing mappings for all given price IDs,
	// creates missing items in Zoho, and returns a map of priceID → zohoItemID.
	// taxRes is resolved once by the caller and applied to every newly created item.
	EnsureItemsMapped(ctx context.Context, inputs []ItemSyncInput, environmentID string, tenantID string, taxRes *ItemTaxResolution) (map[string]string, error)
}

type ItemSyncServiceParams struct {
	Client      ZohoClient
	MappingRepo entityintegrationmapping.Repository
	Logger      *logger.Logger
}

type ItemSyncService struct {
	ItemSyncServiceParams
}

func NewItemSyncService(params ItemSyncServiceParams) ZohoItemSyncService {
	return &ItemSyncService{ItemSyncServiceParams: params}
}

func (s *ItemSyncService) EnsureItemsMapped(ctx context.Context, inputs []ItemSyncInput, environmentID string, tenantID string, taxRes *ItemTaxResolution) (map[string]string, error) {
	if len(inputs) == 0 {
		return map[string]string{}, nil
	}

	// Collect all price IDs for bulk query
	priceIDs := make([]string, len(inputs))
	for i, in := range inputs {
		priceIDs[i] = in.PriceID
	}

	// Single bulk query for all price IDs
	filter := types.NewEntityIntegrationMappingFilter()
	filter.EntityType = types.IntegrationEntityTypePrice
	filter.EntityIDs = priceIDs
	filter.ProviderTypes = []string{string(types.SecretProviderZohoBooks)}

	mappings, err := s.MappingRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to bulk query Zoho item mappings").
			Mark(ierr.ErrDatabase)
	}

	// Build result map from existing mappings
	result := make(map[string]string, len(inputs))
	for _, m := range mappings {
		result[m.EntityID] = m.ProviderEntityID
	}

	// Create items in Zoho for any price IDs not yet mapped
	for _, in := range inputs {
		if _, exists := result[in.PriceID]; exists {
			continue
		}

		zohoItemID, errCreate := s.createAndSaveItem(ctx, in, environmentID, tenantID, taxRes)
		if errCreate != nil {
			s.Logger.Errorw("failed to create Zoho item, line item will fall back to name+rate",
				"price_id", in.PriceID,
				"item_name", in.Name,
				"error", errCreate)
			return nil, errCreate
		}
		result[in.PriceID] = zohoItemID
	}

	return result, nil
}

func (s *ItemSyncService) createAndSaveItem(ctx context.Context, in ItemSyncInput, environmentID string, tenantID string, taxRes *ItemTaxResolution) (string, error) {
	createReq := &ItemCreateRequest{
		Name:        in.Name,
		Rate:        in.Rate.InexactFloat64(),
		Description: in.PriceID,
		ProductType: "service",
		SKU:         in.PriceID,
	}

	if taxRes != nil {
		if taxRes.IsTaxable {
			t := true
			createReq.TaxID = taxRes.TaxID
			createReq.IsTaxable = &t
		} else {
			f := false
			createReq.IsTaxable = &f
			createReq.TaxExemptionID = taxRes.TaxExemptionID
		}
	}

	itemResp, err := s.Client.CreateItem(ctx, createReq)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to create item in Zoho Books").
			WithReportableDetails(map[string]interface{}{
				"price_id":  in.PriceID,
				"item_name": in.Name,
			}).
			Mark(ierr.ErrInternal)
	}

	s.Logger.Infow("created item in Zoho Books",
		"price_id", in.PriceID,
		"zoho_item_id", itemResp.ItemID,
		"item_name", itemResp.Name)

	baseModel := types.GetDefaultBaseModel(ctx)
	baseModel.TenantID = tenantID

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityType:       types.IntegrationEntityTypePrice,
		EntityID:         in.PriceID,
		ProviderType:     string(types.SecretProviderZohoBooks),
		ProviderEntityID: itemResp.ItemID,
		EnvironmentID:    environmentID,
		BaseModel:        baseModel,
		Metadata: map[string]interface{}{
			"synced_at":      time.Now().UTC().Format(time.RFC3339),
			"zoho_item_name": itemResp.Name,
		},
	}

	if err := s.MappingRepo.Create(ctx, mapping); err != nil {
		return "", ierr.WithError(err).
			WithHint("Item created in Zoho but mapping failed to save").
			WithReportableDetails(map[string]interface{}{
				"price_id":     in.PriceID,
				"zoho_item_id": itemResp.ItemID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return itemResp.ItemID, nil
}
