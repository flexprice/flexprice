package integration_test

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// buildFactoryTestContext returns a context with the default tenant/env values seeded by testutil.
func buildFactoryTestContext() context.Context {
	return testutil.SetupContext()
}

// buildStorageTestFactory creates an integration.Factory backed entirely by in-memory stores,
// mirroring the fixture pattern used in
// internal/temporal/activities/paddle/subscription_sync_activities_test.go.
func buildStorageTestFactory(connectionRepo *testutil.InMemoryConnectionStore) (*integration.Factory, security.EncryptionService) {
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

	factory := integration.NewFactory(
		cfg,
		log,
		connectionRepo,
		testutil.NewInMemoryCustomerStore(),
		testutil.NewInMemorySubscriptionStore(),
		testutil.NewInMemoryPlanStore(),
		testutil.NewInMemoryInvoiceStore(),
		testutil.NewInMemoryPaymentStore(),
		nil, // paymentMethodRepo — not needed for storage provider dispatch
		testutil.NewInMemoryPriceStore(),
		testutil.NewInMemoryEntityIntegrationMappingStore(),
		testutil.NewInMemoryMeterStore(),
		testutil.NewInMemoryFeatureStore(),
		encSvc,
		nil, // TemporalService — not needed for storage provider dispatch
	)

	return factory, encSvc
}

func seedS3Connection(ctx context.Context, t *testing.T, store *testutil.InMemoryConnectionStore, encSvc security.EncryptionService) *connection.Connection {
	t.Helper()

	accessKey, err := encSvc.Encrypt("AKIAFAKEACCESSKEY")
	require.NoError(t, err)
	secretKey, err := encSvc.Encrypt("fake-secret-access-key")
	require.NoError(t, err)

	conn := &connection.Connection{
		ID:           "conn_s3_test",
		Name:         "Test S3 Connection",
		ProviderType: types.SecretProviderS3,
		EncryptedSecretData: types.ConnectionMetadata{
			S3: &types.S3ConnectionMetadata{
				AWSAccessKeyID:     accessKey,
				AWSSecretAccessKey: secretKey,
			},
		},
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				Bucket: "test-bucket",
				Region: "us-east-1",
			},
		},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel: types.BaseModel{
			TenantID: types.GetTenantID(ctx),
			Status:   types.StatusPublished,
		},
	}
	require.NoError(t, store.Create(ctx, conn))
	return conn
}

func TestFactory_GetStorageProvider_S3Connection_ReturnsStorageInterface(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()
	factory, encSvc := buildStorageTestFactory(connRepo)

	conn := seedS3Connection(ctx, t, connRepo, encSvc)

	got, err := factory.GetStorageProvider(ctx, conn.ID)
	require.NoError(t, err)
	require.NotNil(t, got)

	var _ storage.Storage = got // compile-time assertion the return type satisfies storage.Storage
	require.Equal(t, storage.ProviderS3, got.Provider())
}

func TestFactory_GetStorageProvider_MissingConnectionID_ReturnsValidationError(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()
	factory, _ := buildStorageTestFactory(connRepo)

	got, err := factory.GetStorageProvider(ctx, "")
	require.Error(t, err)
	require.Nil(t, got)
}

func TestFactory_GetStorageProvider_UnknownConnection_ReturnsNotFoundError(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()
	factory, _ := buildStorageTestFactory(connRepo)

	got, err := factory.GetStorageProvider(ctx, "conn_does_not_exist")
	require.Error(t, err)
	require.Nil(t, got)
}

func TestFactory_GetStorageProvider_UnsupportedProviderType_ReturnsValidationError(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()
	factory, _ := buildStorageTestFactory(connRepo)

	conn := &connection.Connection{
		ID:            "conn_stripe_test",
		Name:          "Test Stripe Connection",
		ProviderType:  types.SecretProviderStripe,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel: types.BaseModel{
			TenantID: types.GetTenantID(ctx),
			Status:   types.StatusPublished,
		},
	}
	require.NoError(t, connRepo.Create(ctx, conn))

	got, err := factory.GetStorageProvider(ctx, conn.ID)
	require.Error(t, err)
	require.Nil(t, got)
}
