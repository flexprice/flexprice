package paddle_test

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	paddleactivities "github.com/flexprice/flexprice/internal/temporal/activities/paddle"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

// buildTestContext returns a context with the default tenant/env values seeded by testutil.
func buildActivityTestContext() context.Context {
	return testutil.SetupContext()
}

func buildTestActivityLogger() *logger.Logger {
	return logger.NewNoopLogger()
}

// buildActivityFactory creates an integration.Factory backed entirely by in-memory stores.
func buildActivityFactory(
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
	log := buildTestActivityLogger()

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
		invoiceRepo,
		testutil.NewInMemoryPaymentStore(),
		testutil.NewInMemoryPriceStore(),
		mappingRepo,
		nil, // PaymentMethodRepo — not needed in activity unit tests
		testutil.NewInMemoryMeterStore(),
		testutil.NewInMemoryFeatureStore(),
		encSvc,
		nil, // TemporalService — not needed in activity unit tests
	)
}

// seedPaddleConnection seeds the connection store with a published Paddle connection.
func seedPaddleConnection(ctx context.Context, t *testing.T, store *testutil.InMemoryConnectionStore) {
	t.Helper()
	conn := &connection.Connection{
		ID:            "conn_paddle_test",
		ProviderType:  types.SecretProviderPaddle,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel: types.BaseModel{
			TenantID: types.GetTenantID(ctx),
			Status:   types.StatusPublished,
		},
	}
	require.NoError(t, store.Create(ctx, conn))
}

// seedSubscriptionMapping seeds an entity_integration_mapping for a subscription→Paddle mapping.
func seedSubscriptionSyncMapping(
	ctx context.Context,
	t *testing.T,
	store entityintegrationmapping.Repository,
	subscriptionID, paddleSubID string,
) {
	t.Helper()
	m := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         subscriptionID,
		EntityType:       types.IntegrationEntityTypeSubscription,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: paddleSubID,
		Metadata:         map[string]interface{}{"synced_via": "test"},
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, store.Create(ctx, m))
}

// seedTestInvoice seeds an invoice in the invoice store.
func seedTestInvoice(ctx context.Context, t *testing.T, store invoice.Repository, inv *invoice.Invoice) {
	t.Helper()
	require.NoError(t, store.Create(ctx, inv))
}

// --- Tests ---

// TestSyncSubscriptionToPaddle_NoPaddleConnection verifies that when GetPaddleIntegration returns
// ErrNotFound (no connection seeded), the activity returns a NonRetryableApplicationError.
func TestSyncSubscriptionToPaddle_NoPaddleConnection(t *testing.T) {
	ctx := buildActivityTestContext()

	// Do NOT seed a Paddle connection — connection store is empty.
	connectionStore := testutil.NewInMemoryConnectionStore()
	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	invoiceStore := testutil.NewInMemoryInvoiceStore()
	subStore := testutil.NewInMemorySubscriptionStore()

	factory := buildActivityFactory(connectionStore, mappingStore, invoiceStore, subStore)
	act := paddleactivities.NewSubscriptionSyncActivities(factory, buildTestActivityLogger())

	input := models.PaddleSubscriptionSyncWorkflowInput{
		SubscriptionID: "sub_no_conn",
		CustomerID:     "cust_no_conn",
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
	}

	err := act.SyncSubscriptionToPaddle(ctx, input)
	require.Error(t, err)

	// Must be a NonRetryableApplicationError with type "ConnectionNotFound".
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.True(t, appErr.NonRetryable(), "error must be non-retryable")
	assert.Equal(t, "ConnectionNotFound", appErr.Type())
}

// TestSyncSubscriptionToPaddle_ValidationError verifies that an invalid input (missing subscription_id)
// returns a NonRetryableApplicationError before even touching the factory.
func TestSyncSubscriptionToPaddle_ValidationError(t *testing.T) {
	ctx := buildActivityTestContext()

	factory := buildActivityFactory(
		testutil.NewInMemoryConnectionStore(),
		testutil.NewInMemoryEntityIntegrationMappingStore(),
		testutil.NewInMemoryInvoiceStore(),
		testutil.NewInMemorySubscriptionStore(),
	)
	act := paddleactivities.NewSubscriptionSyncActivities(factory, buildTestActivityLogger())

	// Missing subscription_id.
	input := models.PaddleSubscriptionSyncWorkflowInput{
		SubscriptionID: "", // invalid
		CustomerID:     "cust_001",
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
	}

	err := act.SyncSubscriptionToPaddle(ctx, input)
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.True(t, appErr.NonRetryable(), "validation error must be non-retryable")
	assert.Equal(t, "ValidationError", appErr.Type())
}

// TestCheckSubscriptionSyncStatus_NoPaddleConnection verifies that when there is no Paddle
// connection, the status returned is "activated" (no sync needed).
func TestCheckSubscriptionSyncStatus_NoPaddleConnection(t *testing.T) {
	ctx := buildActivityTestContext()

	// Do NOT seed a Paddle connection.
	connectionStore := testutil.NewInMemoryConnectionStore()
	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	invoiceStore := testutil.NewInMemoryInvoiceStore()
	subStore := testutil.NewInMemorySubscriptionStore()

	factory := buildActivityFactory(connectionStore, mappingStore, invoiceStore, subStore)
	act := paddleactivities.NewSubscriptionSyncActivities(factory, buildTestActivityLogger())

	input := models.PaddleInvoiceSyncWorkflowInput{
		InvoiceID:     "inv_no_conn",
		CustomerID:    "cust_001",
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
	}

	result, err := act.CheckSubscriptionSyncStatus(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "activated", result.Status, "no Paddle connection should be treated as activated")
}

// TestCheckSubscriptionSyncStatus_NoMapping verifies that when Paddle connection exists but
// no subscription mapping is present, the status is "not_synced".
func TestCheckSubscriptionSyncStatus_NoMapping(t *testing.T) {
	ctx := buildActivityTestContext()

	connectionStore := testutil.NewInMemoryConnectionStore()
	seedPaddleConnection(ctx, t, connectionStore)

	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	// No mapping seeded for the subscription.

	invoiceStore := testutil.NewInMemoryInvoiceStore()
	subStore := testutil.NewInMemorySubscriptionStore()

	// Seed an invoice that has a subscription_id.
	const (
		invoiceID      = "inv_no_mapping"
		subscriptionID = "sub_no_mapping"
	)
	subIDPtr := subscriptionID
	inv := &invoice.Invoice{
		ID:             invoiceID,
		CustomerID:     "cust_001",
		SubscriptionID: &subIDPtr,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	seedTestInvoice(ctx, t, invoiceStore, inv)

	factory := buildActivityFactory(connectionStore, mappingStore, invoiceStore, subStore)
	act := paddleactivities.NewSubscriptionSyncActivities(factory, buildTestActivityLogger())

	// Provide subscriptionID directly to skip invoice lookup.
	input := models.PaddleInvoiceSyncWorkflowInput{
		InvoiceID:      invoiceID,
		CustomerID:     "cust_001",
		SubscriptionID: subscriptionID,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
	}

	result, err := act.CheckSubscriptionSyncStatus(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "not_synced", result.Status, "no mapping should return not_synced")
	assert.Equal(t, subscriptionID, result.SubscriptionID, "resolved subscription_id should match")
	assert.Equal(t, "cust_001", result.CustomerID, "resolved customer_id should match")
}

// TestCheckSubscriptionSyncStatus_MappingExists verifies that when a Paddle mapping exists for
// the subscription, the status returned is "activated".
func TestCheckSubscriptionSyncStatus_MappingExists(t *testing.T) {
	ctx := buildActivityTestContext()

	connectionStore := testutil.NewInMemoryConnectionStore()
	seedPaddleConnection(ctx, t, connectionStore)

	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	const (
		subscriptionID = "sub_with_mapping"
		paddleSubID    = "sub_paddle_existing"
	)
	// Seed an existing mapping for the subscription.
	seedSubscriptionSyncMapping(ctx, t, mappingStore, subscriptionID, paddleSubID)

	invoiceStore := testutil.NewInMemoryInvoiceStore()
	subStore := testutil.NewInMemorySubscriptionStore()

	factory := buildActivityFactory(connectionStore, mappingStore, invoiceStore, subStore)
	act := paddleactivities.NewSubscriptionSyncActivities(factory, buildTestActivityLogger())

	input := models.PaddleInvoiceSyncWorkflowInput{
		InvoiceID:      "inv_mapping_exists",
		CustomerID:     "cust_001",
		SubscriptionID: subscriptionID,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
	}

	result, err := act.CheckSubscriptionSyncStatus(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "activated", result.Status, "existing mapping should return activated")
	assert.Equal(t, subscriptionID, result.SubscriptionID, "resolved subscription_id should match")
}

// TestCheckSubscriptionSyncStatus_NoSubscriptionID_FallbackToInvoice verifies that when
// no subscription_id is in the input, the activity fetches it from the invoice.
func TestCheckSubscriptionSyncStatus_NoSubscriptionID_FallbackToInvoice(t *testing.T) {
	ctx := buildActivityTestContext()

	connectionStore := testutil.NewInMemoryConnectionStore()
	seedPaddleConnection(ctx, t, connectionStore)

	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	const (
		subscriptionID = "sub_invoice_fallback"
		paddleSubID    = "sub_paddle_fallback"
		invoiceID      = "inv_fallback_test"
	)
	// Seed both a mapping and an invoice with subscription_id set.
	seedSubscriptionSyncMapping(ctx, t, mappingStore, subscriptionID, paddleSubID)

	invoiceStore := testutil.NewInMemoryInvoiceStore()
	subIDPtr := subscriptionID
	inv := &invoice.Invoice{
		ID:             invoiceID,
		CustomerID:     "cust_001",
		SubscriptionID: &subIDPtr,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	seedTestInvoice(ctx, t, invoiceStore, inv)

	subStore := testutil.NewInMemorySubscriptionStore()

	factory := buildActivityFactory(connectionStore, mappingStore, invoiceStore, subStore)
	act := paddleactivities.NewSubscriptionSyncActivities(factory, buildTestActivityLogger())

	// No SubscriptionID in input — activity must fall back to invoice lookup.
	input := models.PaddleInvoiceSyncWorkflowInput{
		InvoiceID:     invoiceID,
		CustomerID:    "cust_001",
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		// SubscriptionID intentionally empty.
	}

	result, err := act.CheckSubscriptionSyncStatus(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "activated", result.Status, "invoice-resolved subscription with mapping should return activated")
	assert.Equal(t, subscriptionID, result.SubscriptionID, "resolved subscription_id should match")
	assert.Equal(t, "cust_001", result.CustomerID, "resolved customer_id should match")
}

// TestCheckSubscriptionSyncStatus_InvoiceWithNoSubscription verifies that when an invoice
// exists but has no subscription_id, the status returned is "activated" (no sub sync needed).
func TestCheckSubscriptionSyncStatus_InvoiceWithNoSubscription(t *testing.T) {
	ctx := buildActivityTestContext()

	connectionStore := testutil.NewInMemoryConnectionStore()
	seedPaddleConnection(ctx, t, connectionStore)

	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	const invoiceID = "inv_no_subscription"

	// Seed an invoice with no subscription_id (SubscriptionID is nil).
	invoiceStore := testutil.NewInMemoryInvoiceStore()
	inv := &invoice.Invoice{
		ID:             invoiceID,
		CustomerID:     "cust_001",
		SubscriptionID: nil, // No subscription linked
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	seedTestInvoice(ctx, t, invoiceStore, inv)

	subStore := testutil.NewInMemorySubscriptionStore()

	factory := buildActivityFactory(connectionStore, mappingStore, invoiceStore, subStore)
	act := paddleactivities.NewSubscriptionSyncActivities(factory, buildTestActivityLogger())

	// No SubscriptionID in input — activity will look up invoice and find no subscription_id.
	input := models.PaddleInvoiceSyncWorkflowInput{
		InvoiceID:     invoiceID,
		CustomerID:    "cust_001",
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		// SubscriptionID intentionally empty.
	}

	result, err := act.CheckSubscriptionSyncStatus(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "activated", result.Status, "invoice with no subscription_id should return activated")
}
