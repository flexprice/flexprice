package stripe

// Already-mapped path only: creating a product needs a real *Client, out of unit-test
// reach (same limitation as EnsureCustomerSyncedToStripe — see customer_sync_test.go).
// Reuses that file's syncTestMappingRepo and testContext() (same package).

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type syncTestPriceRepo struct {
	price.Repository
	prices []*price.Price
}

func (r *syncTestPriceRepo) ListAll(_ context.Context, filter *types.PriceFilter) ([]*price.Price, error) {
	if filter == nil || len(filter.PriceIDs) == 0 {
		return append([]*price.Price(nil), r.prices...), nil
	}
	want := make(map[string]struct{}, len(filter.PriceIDs))
	for _, id := range filter.PriceIDs {
		want[id] = struct{}{}
	}
	out := make([]*price.Price, 0, len(filter.PriceIDs))
	for _, p := range r.prices {
		if _, ok := want[p.ID]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func TestEnsureBulkProductsSynced_AllAlreadyMapped(t *testing.T) {
	mappingRepo := &syncTestMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "price_1", ProviderEntityID: "prod_1"},
			{EntityID: "price_2", ProviderEntityID: "prod_2"},
		},
	}
	svc := NewStripePriceSyncService(nil, mappingRepo, nil, logger.NewNoopLogger())

	result, err := svc.EnsureBulkProductsSynced(testContext(), []priceSyncItem{
		{PriceID: "price_1", DisplayName: "Seat fee"},
		{PriceID: "price_2", DisplayName: "API calls"},
	})

	require.NoError(t, err)
	require.Equal(t, map[string]string{"price_1": "prod_1", "price_2": "prod_2"}, result)
	require.Equal(t, 1, mappingRepo.listCalls)
}

func TestEnsureBulkProductsSynced_EmptyInput(t *testing.T) {
	mappingRepo := &syncTestMappingRepo{}
	svc := NewStripePriceSyncService(nil, mappingRepo, nil, logger.NewNoopLogger())

	result, err := svc.EnsureBulkProductsSynced(testContext(), nil)

	require.NoError(t, err)
	require.Empty(t, result)
	require.Equal(t, 0, mappingRepo.listCalls)
}

func TestEnsureBulkProductsSynced_OverrideReusesParentProduct(t *testing.T) {
	mappingRepo := &syncTestMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "price_plan", ProviderEntityID: "prod_plan"},
		},
	}
	priceRepo := &syncTestPriceRepo{
		prices: []*price.Price{
			{ID: "price_override", ParentPriceID: "price_plan"},
			{ID: "price_plan"},
		},
	}
	svc := NewStripePriceSyncService(nil, mappingRepo, priceRepo, logger.NewNoopLogger())

	result, err := svc.EnsureBulkProductsSynced(testContext(), []priceSyncItem{
		{PriceID: "price_override", DisplayName: "Attestra Academy Annual"},
	})

	require.NoError(t, err)
	require.Equal(t, map[string]string{"price_override": "prod_plan"}, result)
}

func TestEnsureBulkProductsSynced_TwoOverridesShareParentProduct(t *testing.T) {
	mappingRepo := &syncTestMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "price_plan", ProviderEntityID: "prod_plan"},
		},
	}
	priceRepo := &syncTestPriceRepo{
		prices: []*price.Price{
			{ID: "price_override_a", ParentPriceID: "price_plan"},
			{ID: "price_override_b", ParentPriceID: "price_plan"},
			{ID: "price_plan"},
		},
	}
	svc := NewStripePriceSyncService(nil, mappingRepo, priceRepo, logger.NewNoopLogger())

	result, err := svc.EnsureBulkProductsSynced(testContext(), []priceSyncItem{
		{PriceID: "price_override_a", DisplayName: "Attestra Academy Annual"},
		{PriceID: "price_override_b", DisplayName: "Attestra Academy Annual"},
	})

	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"price_override_a": "prod_plan",
		"price_override_b": "prod_plan",
	}, result)
}

func TestEnsureBulkProductsSynced_PrefersParentOverLegacyOverrideMapping(t *testing.T) {
	mappingRepo := &syncTestMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "price_plan", ProviderEntityID: "prod_plan"},
			{EntityID: "price_override", ProviderEntityID: "prod_orphan"},
		},
	}
	priceRepo := &syncTestPriceRepo{
		prices: []*price.Price{
			{ID: "price_override", ParentPriceID: "price_plan"},
			{ID: "price_plan"},
		},
	}
	svc := NewStripePriceSyncService(nil, mappingRepo, priceRepo, logger.NewNoopLogger())

	result, err := svc.EnsureBulkProductsSynced(testContext(), []priceSyncItem{
		{PriceID: "price_override", DisplayName: "Attestra Academy Annual"},
	})

	require.NoError(t, err)
	require.Equal(t, map[string]string{"price_override": "prod_plan"}, result)
}

func TestEnsureBulkProductsSynced_MissingParentFallsBackToOwnMapping(t *testing.T) {
	mappingRepo := &syncTestMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "price_override", ProviderEntityID: "prod_override"},
		},
	}
	priceRepo := &syncTestPriceRepo{
		prices: []*price.Price{
			{ID: "price_override", ParentPriceID: "price_gone"},
		},
	}
	svc := NewStripePriceSyncService(nil, mappingRepo, priceRepo, logger.NewNoopLogger())

	result, err := svc.EnsureBulkProductsSynced(testContext(), []priceSyncItem{
		{PriceID: "price_override", DisplayName: "Attestra Academy Annual"},
	})

	require.NoError(t, err)
	require.Equal(t, map[string]string{"price_override": "prod_override"}, result)
}
