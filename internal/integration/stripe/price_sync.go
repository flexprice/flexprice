package stripe

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
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
	logger                       *logger.Logger
}

// NewStripePriceSyncService creates a new price-sync service.
func NewStripePriceSyncService(
	client *Client,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) *stripePriceSyncService {
	return &stripePriceSyncService{
		client:                       client,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// EnsureBulkProductsSynced ensures a Stripe Product exists per price and returns priceID -> stripeProductID.
func (s *stripePriceSyncService) EnsureBulkProductsSynced(ctx context.Context, items []priceSyncItem) (map[string]string, error) {
	if len(items) == 0 {
		return map[string]string{}, nil
	}

	priceIDs := lo.Map(items, func(item priceSyncItem, _ int) string { return item.PriceID })

	existing, err := s.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		EntityIDs:     priceIDs,
		EntityType:    types.IntegrationEntityTypePrice,
		ProviderTypes: []string{"stripe"},
		QueryFilter:   types.NewNoLimitQueryFilter(),
	})
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to fetch existing Stripe product mappings").Mark(ierr.ErrDatabase)
	}

	result := lo.SliceToMap(existing, func(m *entityintegrationmapping.EntityIntegrationMapping) (string, string) {
		return m.EntityID, m.ProviderEntityID
	})

	unmapped := lo.Filter(items, func(item priceSyncItem, _ int) bool { return result[item.PriceID] == "" })
	if len(unmapped) == 0 {
		return result, nil
	}

	// Lazy: only reached when something is unmapped, so the fully-mapped test path needs no Stripe client.
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	for _, item := range unmapped {
		name := item.DisplayName
		if name == "" {
			name = item.PriceID
		}

		product, err := stripeClient.V1Products.Create(ctx, &stripe.ProductCreateParams{
			Name: stripe.String(name),
			Metadata: map[string]string{
				"flexprice_price_id": item.PriceID,
				"environment_id":     types.GetEnvironmentID(ctx),
				"sync_source":        "flexprice",
			},
		})
		if err != nil {
			s.logger.Error(ctx, "failed to create Stripe product for price", "error", err, "price_id", item.PriceID)
			return nil, ierr.NewError("failed to create Stripe product").
				WithHint("Unable to create Stripe product for price. If using a restricted Stripe API key, ensure it has the 'Products: Write' permission.").
				WithReportableDetails(map[string]interface{}{"price_id": item.PriceID, "error": err.Error()}).
				Mark(ierr.ErrSystem)
		}

		mapping := &entityintegrationmapping.EntityIntegrationMapping{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
			EntityID:         item.PriceID,
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
		if err := s.entityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
			if !ierr.IsAlreadyExists(err) {
				return nil, ierr.WithError(err).WithHint("Failed to persist Stripe product mapping").Mark(ierr.ErrDatabase)
			}

			// Lost a create race against a concurrent sync of the same price: the other
			// mapping already committed, so adopt its product instead of failing — the
			// Product we just created above is orphaned (unreferenced, harmless) rather
			// than retried into a duplicate.
			winner, findErr := s.entityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
				EntityIDs:     []string{item.PriceID},
				EntityType:    types.IntegrationEntityTypePrice,
				ProviderTypes: []string{"stripe"},
				QueryFilter:   types.NewNoLimitQueryFilter(),
			})
			if findErr != nil || len(winner) == 0 {
				return nil, ierr.WithError(err).WithHint("Failed to persist Stripe product mapping and could not recover the concurrent winner").Mark(ierr.ErrDatabase)
			}

			s.logger.Info(ctx, "lost product mapping race, adopting concurrently created mapping",
				"price_id", item.PriceID, "orphaned_stripe_product_id", product.ID, "winning_stripe_product_id", winner[0].ProviderEntityID)
			result[item.PriceID] = winner[0].ProviderEntityID
			continue
		}

		result[item.PriceID] = product.ID
		s.logger.Info(ctx, "created Stripe product for price", "price_id", item.PriceID, "stripe_product_id", product.ID)
	}

	return result, nil
}
