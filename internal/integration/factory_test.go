package integration_test

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
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

	return buildStorageTestFactoryWithRepo(connectionRepo, cfg, log, encSvc), encSvc
}

// buildStorageTestFactoryWithRepo is like buildStorageTestFactory but accepts any
// connection.Repository implementation (interface, not concrete in-memory store), so tests
// can inject a fake repository that returns arbitrary errors — e.g. to prove a non-NotFound
// repository failure (DB outage, timeout) is not silently reclassified as ErrNotFound.
func buildStorageTestFactoryWithRepo(connectionRepo connection.Repository, cfg *config.Configuration, log *logger.Logger, encSvc security.EncryptionService) *integration.Factory {
	return integration.NewFactory(
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
		nil, // cache.Locker — not needed for storage provider dispatch (only used by Razorpay payment integration)
	)
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

// seedS3ConnectionWithEmptyCredentials mirrors seedS3Connection but leaves the encrypted
// access-key/secret-key fields empty, simulating a corrupted or never-populated BYO
// credential — the case that must be rejected rather than silently falling back to the
// platform's ambient AWS credential chain.
func seedS3ConnectionWithEmptyCredentials(ctx context.Context, t *testing.T, store *testutil.InMemoryConnectionStore) *connection.Connection {
	t.Helper()

	conn := &connection.Connection{
		ID:           "conn_s3_empty_creds_test",
		Name:         "Test S3 Connection With Empty Credentials",
		ProviderType: types.SecretProviderS3,
		EncryptedSecretData: types.ConnectionMetadata{
			S3: &types.S3ConnectionMetadata{
				AWSAccessKeyID:     "",
				AWSSecretAccessKey: "",
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

func seedGCSConnectionWithEmptyCredentials(ctx context.Context, t *testing.T, store *testutil.InMemoryConnectionStore) *connection.Connection {
	t.Helper()

	conn := &connection.Connection{
		ID:           "conn_gcs_empty_creds_test",
		Name:         "Test GCS Connection With Empty Credentials",
		ProviderType: types.SecretProviderGCS,
		EncryptedSecretData: types.ConnectionMetadata{
			GCS: &types.GCSConnectionMetadata{
				ServiceAccountJSON: "",
			},
		},
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				Bucket: "test-bucket",
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

func TestFactory_GetStorageProvider_S3EmptyCredentials_ReturnsValidationError(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()
	factory, _ := buildStorageTestFactory(connRepo)

	conn := seedS3ConnectionWithEmptyCredentials(ctx, t, connRepo)

	got, err := factory.GetStorageProvider(ctx, conn.ID)
	require.Error(t, err)
	require.Nil(t, got)
	require.True(t, ierr.IsValidation(err), "expected validation error, got: %v", err)
}

func TestFactory_GetStorageProvider_GCSEmptyCredentials_ReturnsValidationError(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()
	factory, _ := buildStorageTestFactory(connRepo)

	conn := seedGCSConnectionWithEmptyCredentials(ctx, t, connRepo)

	got, err := factory.GetStorageProvider(ctx, conn.ID)
	require.Error(t, err)
	require.Nil(t, got)
	require.True(t, ierr.IsValidation(err), "expected validation error, got: %v", err)
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

// erroringConnectionRepo wraps an InMemoryConnectionStore but forces Get to fail with a
// non-NotFound error, simulating a database outage/timeout rather than a missing row.
type erroringConnectionRepo struct {
	connection.Repository
	getErr error
}

func (r *erroringConnectionRepo) Get(ctx context.Context, id string) (*connection.Connection, error) {
	return nil, r.getErr
}

func TestFactory_GetStorageProvider_RepositoryFailure_PreservesOriginalErrorKind(t *testing.T) {
	ctx := buildFactoryTestContext()

	dbErr := ierr.NewError("connection to database lost").
		WithHint("Transient database failure").
		Mark(ierr.ErrDatabase)

	repo := &erroringConnectionRepo{
		Repository: testutil.NewInMemoryConnectionStore(),
		getErr:     dbErr,
	}

	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}
	log := logger.NewNoopLogger()
	encSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)

	factory := buildStorageTestFactoryWithRepo(repo, cfg, log, encSvc)

	got, err := factory.GetStorageProvider(ctx, "conn_whatever")
	require.Error(t, err)
	require.Nil(t, got)

	// The critical assertion: a database failure must NOT be reclassified as ErrNotFound —
	// that would mask real outages as "connection doesn't exist" and break upstream
	// retry/error-handling logic.
	require.False(t, ierr.IsNotFound(err), "database failure was incorrectly reclassified as NotFound: %v", err)
	require.ErrorIs(t, err, dbErr)
}
