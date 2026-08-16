package stripe

// Already-mapped path only: creating a product needs a real *Client, out of unit-test
// reach (same limitation as EnsureCustomerSyncedToStripe — see customer_sync_test.go).
// Reuses that file's syncTestMappingRepo and testContext() (same package).

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/require"
)

func TestEnsureBulkProductsSynced_AllAlreadyMapped(t *testing.T) {
	mappingRepo := &syncTestMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "price_1", ProviderEntityID: "prod_1"},
			{EntityID: "price_2", ProviderEntityID: "prod_2"},
		},
	}
	svc := NewStripePriceSyncService(nil, mappingRepo, logger.NewNoopLogger())

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
	svc := NewStripePriceSyncService(nil, mappingRepo, logger.NewNoopLogger())

	result, err := svc.EnsureBulkProductsSynced(testContext(), nil)

	require.NoError(t, err)
	require.Empty(t, result)
	require.Equal(t, 0, mappingRepo.listCalls)
}
