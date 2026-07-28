package zoho_test

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	zohoactivities "github.com/flexprice/flexprice/internal/temporal/activities/zoho"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

func buildZohoActivityTestContext() context.Context {
	return testutil.SetupContext()
}

func buildZohoActivityFactory(
	connectionRepo *testutil.InMemoryConnectionStore,
	mappingRepo entityintegrationmapping.Repository,
	invoiceRepo invoice.Repository,
	subscriptionRepo subscription.Repository,
) *integration.Factory {
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}
	log := logger.NewNoopLogger()

	encSvc, err := security.NewEncryptionService(cfg, log)
	if err != nil {
		panic("failed to create test encryption service: " + err.Error())
	}

	return integration.NewFactory(
		cfg,
		log,
		connectionRepo,
		testutil.NewInMemoryCustomerStore(),
		subscriptionRepo,
		testutil.NewInMemoryPlanStore(),
		invoiceRepo,
		testutil.NewInMemoryPaymentStore(),
		nil, // paymentMethodRepo — not needed in activity unit tests
		testutil.NewInMemoryPriceStore(),
		mappingRepo,
		testutil.NewInMemoryMeterStore(),
		testutil.NewInMemoryFeatureStore(),
		encSvc,
		nil, // TemporalService — not needed in activity unit tests
		testutil.NewInMemoryRedisLocker(nil),
	)
}

// TestMarkZohoBooksInvoicePaid_NoZohoConnection verifies that when GetZohoBooksIntegration
// returns ErrNotFound (no connection seeded), the activity returns a NonRetryableApplicationError.
func TestMarkZohoBooksInvoicePaid_NoZohoConnection(t *testing.T) {
	ctx := buildZohoActivityTestContext()

	connectionStore := testutil.NewInMemoryConnectionStore()
	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	invoiceStore := testutil.NewInMemoryInvoiceStore()
	subStore := testutil.NewInMemorySubscriptionStore()

	factory := buildZohoActivityFactory(connectionStore, mappingStore, invoiceStore, subStore)
	act := zohoactivities.NewInvoiceSyncActivities(factory, logger.NewNoopLogger())

	input := models.NewZohoBooksInvoiceMarkPaidWorkflowInput("inv_no_conn", types.GetTenantID(ctx), types.GetEnvironmentID(ctx))

	err := act.MarkZohoBooksInvoicePaid(ctx, input)
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.True(t, appErr.NonRetryable(), "error must be non-retryable")
	assert.Equal(t, "ConnectionNotFound", appErr.Type())
}
