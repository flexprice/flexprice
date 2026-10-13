package stripe

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stripe/stripe-go/v82"
)

// priceSyncItem is a single FlexPrice Price to ensure has a linked Stripe Product.
type priceSyncItem struct {
	PriceID     string
	DisplayName string
}

// stripePriceSyncService ensures FlexPrice Prices have a linked Stripe Product for invoice line items.
type stripePriceSyncService struct {
	client                       *Client
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	priceRepo                    price.Repository
	logger                       *logger.Logger
}

// NewStripePriceSyncService creates a new price-sync service.
func NewStripePriceSyncService(
	client *Client,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	priceRepo price.Repository,
	logger *logger.Logger,
) *stripePriceSyncService {
	return &stripePriceSyncService{
		client:                       client,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		priceRepo:                    priceRepo,
		logger:                       logger,
	}
}

// EnsureBulkProductsSynced returns itemPriceID -> stripeProductID. Products are keyed on the
// root price so every subscription override of a plan price shares one Stripe catalog item
// instead of minting a duplicate.
func (s *stripePriceSyncService) EnsureBulkProductsSynced(ctx context.Context, items []priceSyncItem) (map[string]string, error) {
	if len(items) == 0 {
		return map[string]string{}, nil
	}

	itemToRootPriceIdMap, err := s.resolveRootPriceIDs(ctx, items)
	if err != nil {
		return nil, err
	}

	rootPriceIDs := lo.Uniq(lo.Map(items, func(item priceSyncItem, _ int) string { return itemToRootPriceIdMap[item.PriceID] }))

	existing, err := s.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		EntityIDs:     rootPriceIDs,
		EntityType:    types.IntegrationEntityTypePrice,
		ProviderTypes: []string{"stripe"},
		QueryFilter:   types.NewNoLimitQueryFilter(),
	})
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to fetch existing Stripe product mappings").Mark(ierr.ErrDatabase)
	}

	rootProductMap := lo.SliceToMap(existing, func(m *entityintegrationmapping.EntityIntegrationMapping) (string, string) {
		return m.EntityID, m.ProviderEntityID
	})

	result := make(map[string]string, len(items))
	for _, item := range items {
		if productID := rootProductMap[itemToRootPriceIdMap[item.PriceID]]; productID != "" {
			result[item.PriceID] = productID
		}
	}

	unmappedItems := lo.Filter(items, func(item priceSyncItem, _ int) bool { return result[item.PriceID] == "" })
	if len(unmappedItems) == 0 {
		return result, nil
	}

	// Lazy: only reached when something is unmapped, so the fully-mapped test path needs no Stripe client.
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// Dedupe by root so two overrides of one unmapped parent create a single product.
	unmappedRoots := lo.Uniq(lo.Map(unmappedItems, func(item priceSyncItem, _ int) string { return itemToRootPriceIdMap[item.PriceID] }))
	displayByRoot := make(map[string]string, len(unmappedRoots))
	for _, item := range unmappedItems {
		rootPriceID := itemToRootPriceIdMap[item.PriceID]
		if displayByRoot[rootPriceID] == "" && item.DisplayName != "" {
			displayByRoot[rootPriceID] = item.DisplayName
		}
	}

	for _, rootID := range unmappedRoots {
		name := displayByRoot[rootID]
		if name == "" {
			name = rootID
		}

		product, err := stripeClient.V1Products.Create(ctx, &stripe.ProductCreateParams{
			Name: stripe.String(name),
			Metadata: map[string]string{
				"flexprice_price_id": rootID,
				"environment_id":     types.GetEnvironmentID(ctx),
				"sync_source":        "flexprice",
			},
		})
		if err != nil {
			s.logger.Error(ctx, "failed to create Stripe product for price", "error", err, "price_id", rootID)
			return nil, ierr.NewError("failed to create Stripe product").
				WithHint("Unable to create Stripe product for price. If using a restricted Stripe API key, ensure it has the 'Products: Write' permission.").
				WithReportableDetails(map[string]interface{}{"price_id": rootID, "error": err.Error()}).
				Mark(ierr.ErrSystem)
		}

		mapping := &entityintegrationmapping.EntityIntegrationMapping{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
			EntityID:         rootID,
			EntityType:       types.IntegrationEntityTypePrice,
			ProviderType:     "stripe",
			ProviderEntityID: product.ID,
			Metadata: map[string]interface{}{
				"sync_timestamp": time.Now().Unix(),
				"sync_source":    "flexprice",
			},
			EnvironmentID: types.GetEnvironmentID(ctx),
			BaseModel:     types.GetDefaultBaseModel(ctx),
		}

		productID := product.ID
		if err := s.entityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
			if !ierr.IsAlreadyExists(err) {
				return nil, ierr.WithError(err).WithHint("Failed to persist Stripe product mapping").Mark(ierr.ErrDatabase)
			}

			// Lost a create race against a concurrent sync of the same price: the other
			// mapping already committed, so adopt its product instead of failing — the
			// Product we just created above is orphaned (unreferenced, harmless) rather
			// than retried into a duplicate.
			winner, findErr := s.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
				EntityIDs:     []string{rootID},
				EntityType:    types.IntegrationEntityTypePrice,
				ProviderTypes: []string{"stripe"},
				QueryFilter:   types.NewNoLimitQueryFilter(),
			})
			if findErr != nil || len(winner) == 0 {
				return nil, ierr.WithError(err).WithHint("Failed to persist Stripe product mapping and could not recover the concurrent winner").Mark(ierr.ErrDatabase)
			}

			s.logger.Info(ctx, "lost product mapping race, adopting concurrently created mapping",
				"price_id", rootID, "orphaned_stripe_product_id", product.ID, "winning_stripe_product_id", winner[0].ProviderEntityID)
			productID = winner[0].ProviderEntityID
		} else {
			s.logger.Info(ctx, "created Stripe product for price", "price_id", rootID, "stripe_product_id", product.ID)
		}

		rootProductMap[rootID] = productID
		for _, item := range unmappedItems {
			if itemToRootPriceIdMap[item.PriceID] == rootID {
				result[item.PriceID] = productID
			}
		}
	}

	return result, nil
}

// An override whose parent row is gone keeps its own ID so it still gets a product.
func (s *stripePriceSyncService) resolveRootPriceIDs(ctx context.Context, items []priceSyncItem) (map[string]string, error) {
	itemToRootPriceIdMap := lo.SliceToMap(items, func(item priceSyncItem) (string, string) {
		return item.PriceID, item.PriceID
	})
	if s.priceRepo == nil {
		return itemToRootPriceIdMap, nil
	}

	prices, err := s.listPricesByID(ctx, lo.Keys(itemToRootPriceIdMap))
	if err != nil {
		return nil, err
	}

	exists := lo.SliceToMap(prices, func(p *price.Price) (string, bool) { return p.ID, true })
	parentPriceIdByPriceId := make(map[string]string, len(prices))
	for _, p := range prices {
		if root := p.GetRootPriceID(); root != p.ID {
			parentPriceIdByPriceId[p.ID] = root
		}
	}
	if len(parentPriceIdByPriceId) == 0 {
		return itemToRootPriceIdMap, nil
	}

	unknownParentPriceIds := lo.Filter(lo.Uniq(lo.Values(parentPriceIdByPriceId)), func(id string, _ int) bool { return !exists[id] })
	if len(unknownParentPriceIds) > 0 {
		unknownParentPrices, err := s.listPricesByID(ctx, unknownParentPriceIds)
		if err != nil {
			return nil, err
		}
		for _, p := range unknownParentPrices {
			exists[p.ID] = true
		}
	}

	for itemID, parentID := range parentPriceIdByPriceId {
		if exists[parentID] {
			itemToRootPriceIdMap[itemID] = parentID
		}
	}
	return itemToRootPriceIdMap, nil
}

// An ended plan price must keep owning the Product its overrides point at, hence expired prices.
func (s *stripePriceSyncService) listPricesByID(ctx context.Context, ids []string) ([]*price.Price, error) {
	prices, err := s.priceRepo.ListAll(ctx, types.NewNoLimitPriceFilter().WithPriceIDs(ids).WithAllowExpiredPrices(true))
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to load prices for Stripe product sync").Mark(ierr.ErrDatabase)
	}

	return prices, nil
}
