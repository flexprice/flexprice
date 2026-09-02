package integration_test

import (
	"context"
	"os"
	"path/filepath"
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
	require.True(t, ierr.IsNotFound(err), "expected not-found error, got: %v", err)
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

// seedGCSCustomerConnection builds a customer (non-managed) GCS connection. GCS BYOB is
// out of scope: buildGCSStorage must refuse it regardless of what credentials it carries.
func seedGCSCustomerConnection(ctx context.Context, t *testing.T, store *testutil.InMemoryConnectionStore) *connection.Connection {
	t.Helper()

	conn := &connection.Connection{
		ID:                  "conn_gcs_customer_test",
		Name:                "Test GCS Customer Connection",
		ProviderType:        types.SecretProviderGCS,
		EncryptedSecretData: types.ConnectionMetadata{},
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

// seedFlexpriceManagedGCSConnection builds a Flexprice-managed GCS connection. It
// carries NO service account key on purpose: managed connections authenticate with
// the deployment's ambient Workload Identity.
func seedFlexpriceManagedGCSConnection(ctx context.Context, t *testing.T, store *testutil.InMemoryConnectionStore) *connection.Connection {
	t.Helper()

	conn := &connection.Connection{
		ID:                  "conn_gcs_managed_test",
		Name:                "Test Flexprice-Managed GCS Connection",
		ProviderType:        types.SecretProviderGCS,
		EncryptedSecretData: types.ConnectionMetadata{},
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				IsFlexpriceManaged: true,
				KeyPrefix:          "tenant_x/env_y",
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

// stubAmbientGCPCredentials points Application Default Credentials at a throwaway
// credentials file for the duration of the test. The managed GCS path
// deliberately resolves credentials ambiently, so without this the test would
// depend on whoever runs it happening to have real ADC configured — passing on a
// developer laptop with gcloud and failing in CI.
//
// Uses the authorized_user credential shape rather than a service account so no
// private-key-shaped material appears in the repository (secret scanners flag it,
// correctly, even when the key is non-functional). Constructing a storage client
// only parses these credentials; it issues no request, so the placeholder values
// are never exercised.
func stubAmbientGCPCredentials(t *testing.T) {
	t.Helper()

	const fakeADC = `{
  "type": "authorized_user",
  "client_id": "test-client-id.apps.googleusercontent.com",
  "client_secret": "not-a-real-secret",
  "refresh_token": "not-a-real-refresh-token"
}`

	path := filepath.Join(t.TempDir(), "adc.json")
	require.NoError(t, os.WriteFile(path, []byte(fakeADC), 0o600))
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path)
}

// A Flexprice-managed GCS connection must construct without any service account key,
// taking its bucket from configuration rather than from the connection row.
func TestFactory_GetStorageProvider_GCSFlexpriceManaged_UsesAmbientCredentials(t *testing.T) {
	stubAmbientGCPCredentials(t)

	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()

	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{EncryptionKey: "test-encryption-key-for-unit-tests-only"},
	}
	cfg.FlexpriceGCSExports.Bucket = "flexprice-managed-bucket"
	log := logger.NewNoopLogger()
	encSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)
	factory := buildStorageTestFactoryWithRepo(connRepo, cfg, log, encSvc)

	conn := seedFlexpriceManagedGCSConnection(ctx, t, connRepo)

	got, err := factory.GetStorageProvider(ctx, conn.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, storage.ProviderGCS, got.Provider())
	require.Equal(t, "gs://flexprice-managed-bucket/k", got.FileURL("k"),
		"managed connection must use the configured Flexprice bucket")
}

// GCS counterpart of
// TestFactory_GetStorageProvider_S3FlexpriceManaged_RowBucketWinsOverPlatformConfig.
// buildGCSStorage had the same defect and is fixed the same way.
func TestFactory_GetStorageProvider_GCSFlexpriceManaged_RowBucketWinsOverPlatformConfig(t *testing.T) {
	stubAmbientGCPCredentials(t)

	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()

	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{EncryptionKey: "test-encryption-key-for-unit-tests-only"},
	}
	cfg.FlexpriceGCSExports.Bucket = "platform-config-bucket"
	log := logger.NewNoopLogger()
	encSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)
	factory := buildStorageTestFactoryWithRepo(connRepo, cfg, log, encSvc)

	conn := seedFlexpriceManagedGCSConnection(ctx, t, connRepo)
	conn.SyncConfig.Storage.Bucket = "bucket-recorded-at-creation"
	require.NoError(t, connRepo.Update(ctx, conn))

	got, err := factory.GetStorageProvider(ctx, conn.ID)
	require.NoError(t, err)
	require.Equal(t, "gs://bucket-recorded-at-creation/k", got.FileURL("k"),
		"managed GCS export must use the bucket recorded on the connection, not current platform config")
}

// Without a configured bucket the managed path must fail loudly rather than
// constructing a client pointed at an empty bucket name.
func TestFactory_GetStorageProvider_GCSFlexpriceManagedNoBucket_ReturnsValidationError(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()
	factory, _ := buildStorageTestFactory(connRepo)

	conn := seedFlexpriceManagedGCSConnection(ctx, t, connRepo)

	got, err := factory.GetStorageProvider(ctx, conn.ID)
	require.Error(t, err)
	require.Nil(t, got)
	require.True(t, ierr.IsValidation(err), "expected validation error, got: %v", err)
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

// GCS BYOB (customer service-account-JSON connections) is out of scope: buildGCSStorage
// must refuse a non-managed GCS connection outright, regardless of credentials.
func TestFactory_GetStorageProvider_GCSCustomer_ReturnsValidationError(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()
	factory, _ := buildStorageTestFactory(connRepo)

	conn := seedGCSCustomerConnection(ctx, t, connRepo)

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

// seedFlexpriceManagedS3Connection builds a Flexprice-managed S3 connection. It
// carries NO credential snapshot on purpose: managed connections resolve
// credentials from platform config at runtime (see Factory.buildS3Storage).
func seedFlexpriceManagedS3Connection(ctx context.Context, t *testing.T, store *testutil.InMemoryConnectionStore) *connection.Connection {
	t.Helper()

	conn := &connection.Connection{
		ID:                  "conn_s3_managed_test",
		Name:                "Test Flexprice-Managed S3 Connection",
		ProviderType:        types.SecretProviderS3,
		EncryptedSecretData: types.ConnectionMetadata{},
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				IsFlexpriceManaged: true,
				KeyPrefix:          "tenant_x/env_y",
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

// seedFlexpriceManagedS3ConnectionWithLegacySnapshot mirrors
// seedFlexpriceManagedS3Connection but also carries a legacy credential
// snapshot in EncryptedSecretData.S3 — the shape connection.CreateConnection
// used to persist before the fix. The managed branch in buildS3Storage must run
// before the decrypt path and ignore this snapshot entirely, using platform
// config instead.
func seedFlexpriceManagedS3ConnectionWithLegacySnapshot(ctx context.Context, t *testing.T, store *testutil.InMemoryConnectionStore, encSvc security.EncryptionService) *connection.Connection {
	t.Helper()

	accessKey, err := encSvc.Encrypt("AKIALEGACYSNAPSHOT")
	require.NoError(t, err)
	secretKey, err := encSvc.Encrypt("legacy-secret")
	require.NoError(t, err)

	conn := &connection.Connection{
		ID:           "conn_s3_managed_legacy_test",
		Name:         "Test Flexprice-Managed S3 Connection With Legacy Snapshot",
		ProviderType: types.SecretProviderS3,
		EncryptedSecretData: types.ConnectionMetadata{
			S3: &types.S3ConnectionMetadata{
				AWSAccessKeyID:     accessKey,
				AWSSecretAccessKey: secretKey,
			},
		},
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				IsFlexpriceManaged: true,
				KeyPrefix:          "tenant_x/env_y",
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

// A Flexprice-managed S3 connection configured for static platform credentials
// must construct an s3backend using the PLATFORM config's bucket/region/keys,
// not anything from the connection row.
func TestFactory_GetStorageProvider_S3FlexpriceManaged_Static(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()

	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{EncryptionKey: "test-encryption-key-for-unit-tests-only"},
	}
	cfg.FlexpriceS3Exports.Bucket = "flexprice-managed-s3-bucket"
	cfg.FlexpriceS3Exports.Region = "ap-south-1"
	cfg.FlexpriceS3Exports.AWSAccessKeyID = "AKIAPLATFORMKEY"
	cfg.FlexpriceS3Exports.AWSSecretAccessKey = "platform-secret"

	log := logger.NewNoopLogger()
	encSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)
	factory := buildStorageTestFactoryWithRepo(connRepo, cfg, log, encSvc)

	conn := seedFlexpriceManagedS3Connection(ctx, t, connRepo)

	got, err := factory.GetStorageProvider(ctx, conn.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, storage.ProviderS3, got.Provider())
}

// A Flexprice-managed S3 connection with credential_source "ambient" and no
// static keys configured at all must still construct successfully — this is
// the entire point of the fix: ambient AWS credential chains (EKS IRSA / EKS Pod
// Identity / ECS task role / EC2 instance profile) supply nothing explicit here.
func TestFactory_GetStorageProvider_S3FlexpriceManaged_Ambient(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()

	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{EncryptionKey: "test-encryption-key-for-unit-tests-only"},
	}
	cfg.FlexpriceS3Exports.Bucket = "flexprice-managed-s3-bucket"
	cfg.FlexpriceS3Exports.Region = "ap-south-1"
	cfg.FlexpriceS3Exports.CredentialSource = config.CredentialSourceAmbient
	// No AWSAccessKeyID / AWSSecretAccessKey configured at all.

	log := logger.NewNoopLogger()
	encSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)
	factory := buildStorageTestFactoryWithRepo(connRepo, cfg, log, encSvc)

	conn := seedFlexpriceManagedS3Connection(ctx, t, connRepo)

	got, err := factory.GetStorageProvider(ctx, conn.ID)
	require.NoError(t, err, "ambient managed S3 must construct without any configured credentials")
	require.NotNil(t, got)
	require.Equal(t, storage.ProviderS3, got.Provider())
}

// A managed row that still carries a legacy credential snapshot (from before
// this fix) must succeed and use platform config, ignoring the snapshot.
func TestFactory_GetStorageProvider_S3FlexpriceManaged_IgnoresLegacyCredentialSnapshot(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()

	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{EncryptionKey: "test-encryption-key-for-unit-tests-only"},
	}
	cfg.FlexpriceS3Exports.Bucket = "flexprice-managed-s3-bucket"
	cfg.FlexpriceS3Exports.Region = "ap-south-1"
	cfg.FlexpriceS3Exports.CredentialSource = config.CredentialSourceAmbient

	log := logger.NewNoopLogger()
	encSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)
	factory := buildStorageTestFactoryWithRepo(connRepo, cfg, log, encSvc)

	conn := seedFlexpriceManagedS3ConnectionWithLegacySnapshot(ctx, t, connRepo, encSvc)

	got, err := factory.GetStorageProvider(ctx, conn.ID)
	require.NoError(t, err, "managed branch must ignore any legacy credential snapshot on the connection row")
	require.NotNil(t, got)
	require.Equal(t, storage.ProviderS3, got.Provider())
}

// Verified live against AWS staging on 2026-08-01: a managed connection created
// on the pre-branch image recorded bucket "…-a-ass1" in sync_config, and after
// deploying this branch with FLEXPRICE_FLEXPRICE_S3_EXPORTS_BUCKET pointing at
// "…-b-ass1", the very same connection — untouched, updated_at unchanged —
// exported to bucket B. Three other tenants' scheduled exports were redirected
// the same way. Ignoring the credential snapshot is intended; silently moving
// the DESTINATION is not, because prior exports live in the recorded bucket.
func TestFactory_GetStorageProvider_S3FlexpriceManaged_RowBucketWinsOverPlatformConfig(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()

	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{EncryptionKey: "test-encryption-key-for-unit-tests-only"},
	}
	cfg.FlexpriceS3Exports.Bucket = "platform-config-bucket"
	cfg.FlexpriceS3Exports.Region = "ap-south-1"
	cfg.FlexpriceS3Exports.CredentialSource = config.CredentialSourceAmbient

	log := logger.NewNoopLogger()
	encSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)
	factory := buildStorageTestFactoryWithRepo(connRepo, cfg, log, encSvc)

	conn := seedFlexpriceManagedS3Connection(ctx, t, connRepo)
	conn.SyncConfig.Storage.Bucket = "bucket-recorded-at-creation"
	conn.SyncConfig.Storage.Region = "ap-south-1"
	require.NoError(t, connRepo.Update(ctx, conn))

	got, err := factory.GetStorageProvider(ctx, conn.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Contains(t, got.FileURL("some/key.csv"), "bucket-recorded-at-creation",
		"managed export must write to the bucket recorded on the connection, not the current platform config")
	require.NotContains(t, got.FileURL("some/key.csv"), "platform-config-bucket")
}

// Rows created before the bucket was recorded carry an empty Bucket; those must
// still resolve, falling back to platform config.
func TestFactory_GetStorageProvider_S3FlexpriceManaged_EmptyRowBucketFallsBackToConfig(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()

	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{EncryptionKey: "test-encryption-key-for-unit-tests-only"},
	}
	cfg.FlexpriceS3Exports.Bucket = "platform-config-bucket"
	cfg.FlexpriceS3Exports.Region = "ap-south-1"
	cfg.FlexpriceS3Exports.CredentialSource = config.CredentialSourceAmbient

	log := logger.NewNoopLogger()
	encSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)
	factory := buildStorageTestFactoryWithRepo(connRepo, cfg, log, encSvc)

	// seedFlexpriceManagedS3Connection records no bucket.
	conn := seedFlexpriceManagedS3Connection(ctx, t, connRepo)

	got, err := factory.GetStorageProvider(ctx, conn.ID)
	require.NoError(t, err)
	require.Contains(t, got.FileURL("some/key.csv"), "platform-config-bucket",
		"a managed row with no recorded bucket must fall back to platform config")
}

// Regression guard: adding the managed ambient-credential path must not weaken the
// refusal to fall back to ambient credentials for customer BYO S3 connections, even
// when the platform's managed S3 config happens to be fully configured.
func TestFactory_GetStorageProvider_S3CustomerBYOEmptyCreds_StillRefusesAmbient(t *testing.T) {
	ctx := buildFactoryTestContext()
	connRepo := testutil.NewInMemoryConnectionStore()

	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{EncryptionKey: "test-encryption-key-for-unit-tests-only"},
	}
	cfg.FlexpriceS3Exports.Bucket = "flexprice-managed-s3-bucket"
	cfg.FlexpriceS3Exports.Region = "ap-south-1"
	cfg.FlexpriceS3Exports.CredentialSource = config.CredentialSourceAmbient

	log := logger.NewNoopLogger()
	encSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)
	factory := buildStorageTestFactoryWithRepo(connRepo, cfg, log, encSvc)

	conn := seedS3ConnectionWithEmptyCredentials(ctx, t, connRepo)

	got, err := factory.GetStorageProvider(ctx, conn.ID)
	require.Error(t, err)
	require.Nil(t, got)
	require.True(t, ierr.IsValidation(err), "expected validation error, got: %v", err)
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
