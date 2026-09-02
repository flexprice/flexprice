package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// newConnectionServiceWithRealFactoryForTest builds a connectionService wired with a real
// *integration.Factory (not nil), so the S3/GCS pre-persist validation path in
// validateStorageReachable actually runs instead of short-circuiting on a nil
// IntegrationFactory. Only connectionRepo, config and encryptionService are exercised by
// the storage-provider code path (GetStorageProviderForConnection / buildS3Storage /
// buildGCSStorage), so every other Factory dependency is safe to leave nil here.
func newConnectionServiceWithRealFactoryForTest(t *testing.T, cfg *config.Configuration) (ConnectionService, *testutil.InMemoryConnectionStore) {
	t.Helper()

	log := logger.NewNoopLogger()
	encSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)

	connRepo := testutil.NewInMemoryConnectionStore()

	factory := integration.NewFactory(
		cfg,
		log,
		connRepo,
		nil, // customerRepo
		nil, // subscriptionRepo
		nil, // planRepo
		nil, // invoiceRepo
		nil, // paymentRepo
		nil, // paymentMethodRepo
		nil, // priceRepo
		nil, // entityIntegrationMappingRepo
		nil, // meterRepo
		nil, // featureRepo
		encSvc,
		nil, // temporalSvc
		nil, // locker
	)

	params := ServiceParams{
		Logger:             log,
		Config:             cfg,
		ConnectionRepo:     connRepo,
		IntegrationFactory: factory,
	}

	return NewConnectionService(params, encSvc), connRepo
}

// newConnectionServiceForTest builds a connectionService with an in-memory connection
// repository, mirroring the minimal-ServiceParams fixture pattern used elsewhere in this
// package (see billing_commitment_test.go's newCommitmentCalculatorForTest).
func newConnectionServiceForTest(t *testing.T) (ConnectionService, *testutil.InMemoryConnectionStore) {
	t.Helper()

	log := logger.NewNoopLogger()
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}
	encSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)

	connRepo := testutil.NewInMemoryConnectionStore()

	params := ServiceParams{
		Logger:         log,
		Config:         cfg,
		ConnectionRepo: connRepo,
	}

	return NewConnectionService(params, encSvc), connRepo
}

// TestCreateConnection_SecondPublishedGCSConnection_Succeeds proves GCS connections are
// exempt from the "one published connection per provider per environment" rule, matching
// SecretProviderGCS's documented contract ("supports multiple connections per environment")
// in internal/types/secret.go — the same exemption S3 already has, since customers can have
// multiple GCS buckets, one connection per bucket.
func TestCreateConnection_SecondPublishedGCSConnection_Succeeds(t *testing.T) {
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}
	cfg.FlexpriceGCSExports.Bucket = "flexprice-managed-gcs-bucket"

	svc, _ := newConnectionServiceForTestWithConfig(t, cfg)
	ctx := testutil.SetupContext()

	req1 := dto.CreateConnectionRequest{
		Name:         "GCS Connection 1",
		ProviderType: types.SecretProviderGCS,
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				IsFlexpriceManaged: true,
			},
		},
	}
	resp1, err := svc.CreateConnection(ctx, req1)
	require.NoError(t, err)
	require.NotNil(t, resp1)

	req2 := dto.CreateConnectionRequest{
		Name:         "GCS Connection 2",
		ProviderType: types.SecretProviderGCS,
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				IsFlexpriceManaged: true,
			},
		},
	}
	resp2, err := svc.CreateConnection(ctx, req2)
	require.NoError(t, err, "a second published GCS connection in the same tenant/environment must be allowed")
	require.NotNil(t, resp2)
	require.NotEqual(t, resp1.ID, resp2.ID)
}

// newConnectionServiceForTestWithConfig is like newConnectionServiceForTest but lets the
// caller supply a pre-populated config (e.g. FlexpriceS3Exports), needed for managed-S3
// creation tests.
func newConnectionServiceForTestWithConfig(t *testing.T, cfg *config.Configuration) (ConnectionService, *testutil.InMemoryConnectionStore) {
	t.Helper()

	log := logger.NewNoopLogger()
	encSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)

	connRepo := testutil.NewInMemoryConnectionStore()

	params := ServiceParams{
		Logger:         log,
		Config:         cfg,
		ConnectionRepo: connRepo,
	}

	return NewConnectionService(params, encSvc), connRepo
}

// TestCreateConnection_FlexpriceManagedS3_Ambient_SucceedsAndPersistsNoCredentials proves
// the fix: a managed S3 connection with no static platform keys configured (ambient credential
// source) must succeed at creation time and must NOT have any credentials injected into
// EncryptedSecretData.S3 — credentials are resolved at runtime from platform config, not stored
// on the connection row.
func TestCreateConnection_FlexpriceManagedS3_Ambient_SucceedsAndPersistsNoCredentials(t *testing.T) {
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}
	cfg.FlexpriceS3Exports.Bucket = "flexprice-managed-bucket"
	cfg.FlexpriceS3Exports.Region = "ap-south-1"
	cfg.FlexpriceS3Exports.CredentialSource = config.CredentialSourceAmbient
	// Deliberately no AWSAccessKeyID / AWSSecretAccessKey configured.

	svc, connRepo := newConnectionServiceForTestWithConfig(t, cfg)
	ctx := testutil.SetupContext()

	req := dto.CreateConnectionRequest{
		Name:         "Managed S3 Connection",
		ProviderType: types.SecretProviderS3,
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				IsFlexpriceManaged: true,
			},
		},
	}

	resp, err := svc.CreateConnection(ctx, req)
	require.NoError(t, err, "ambient managed S3 creation must succeed with no static platform keys configured")
	require.NotNil(t, resp)

	stored, err := connRepo.Get(ctx, resp.ID)
	require.NoError(t, err)
	require.Nil(t, stored.EncryptedSecretData.S3,
		"managed S3 connection must not persist any credential snapshot")
}

// TestCreateConnection_FlexpriceManagedS3_MissingBucket_Fails proves creation still fails
// loudly when the platform's managed-S3 config is not usable at all (e.g. missing bucket),
// mirroring the equivalent managed-GCS guard.
func TestCreateConnection_FlexpriceManagedS3_MissingBucket_Fails(t *testing.T) {
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}
	// No FlexpriceS3Exports configured at all.

	svc, _ := newConnectionServiceForTestWithConfig(t, cfg)
	ctx := testutil.SetupContext()

	req := dto.CreateConnectionRequest{
		Name:         "Managed S3 Connection",
		ProviderType: types.SecretProviderS3,
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				IsFlexpriceManaged: true,
			},
		},
	}

	resp, err := svc.CreateConnection(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

// TestCreateConnection_FlexpriceManagedS3_Static_SetsBucketRegionAndKeyPrefix proves the
// static-credential-source path still sets bucket/region/key_prefix from platform config,
// matching the previous (pre-fix) behavior for these three fields — only credential injection
// was removed.
func TestCreateConnection_FlexpriceManagedS3_Static_SetsBucketRegionAndKeyPrefix(t *testing.T) {
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}
	cfg.FlexpriceS3Exports.Bucket = "flexprice-managed-bucket"
	cfg.FlexpriceS3Exports.Region = "ap-south-1"
	cfg.FlexpriceS3Exports.AWSAccessKeyID = "AKIAPLATFORMKEY"
	cfg.FlexpriceS3Exports.AWSSecretAccessKey = "platform-secret"

	svc, connRepo := newConnectionServiceForTestWithConfig(t, cfg)
	ctx := testutil.SetupContext()

	req := dto.CreateConnectionRequest{
		Name:         "Managed S3 Connection",
		ProviderType: types.SecretProviderS3,
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				IsFlexpriceManaged: true,
			},
		},
	}

	resp, err := svc.CreateConnection(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.SyncConfig)
	require.NotNil(t, resp.SyncConfig.Storage)
	require.Equal(t, "flexprice-managed-bucket", resp.SyncConfig.Storage.Bucket)
	require.Equal(t, "ap-south-1", resp.SyncConfig.Storage.Region)
	require.NotEmpty(t, resp.SyncConfig.Storage.KeyPrefix)

	stored, err := connRepo.Get(ctx, resp.ID)
	require.NoError(t, err)
	require.Nil(t, stored.EncryptedSecretData.S3,
		"managed S3 connection must not persist a credential snapshot even under the static credential source")
}

// TestCreateConnection_SecondPublishedStripeConnection_Fails is a control case confirming
// the uniqueness rule still applies to providers that are NOT exempted (e.g. Stripe),
// so the GCS exemption above is scoped correctly and doesn't accidentally disable the rule
// for everyone.
func TestCreateConnection_SecondPublishedStripeConnection_Fails(t *testing.T) {
	svc, _ := newConnectionServiceForTest(t)
	ctx := testutil.SetupContext()

	req1 := dto.CreateConnectionRequest{
		Name:         "Stripe Connection 1",
		ProviderType: types.SecretProviderStripe,
		EncryptedSecretData: types.ConnectionMetadata{
			Stripe: &types.StripeConnectionMetadata{
				PublishableKey: "pk_test_1",
				SecretKey:      "sk_test_1",
			},
		},
	}
	_, err := svc.CreateConnection(ctx, req1)
	require.NoError(t, err)

	req2 := dto.CreateConnectionRequest{
		Name:         "Stripe Connection 2",
		ProviderType: types.SecretProviderStripe,
		EncryptedSecretData: types.ConnectionMetadata{
			Stripe: &types.StripeConnectionMetadata{
				PublishableKey: "pk_test_2",
				SecretKey:      "sk_test_2",
			},
		},
	}
	_, err = svc.CreateConnection(ctx, req2)
	require.Error(t, err)
	require.True(t, ierr.IsAlreadyExists(err), "expected already-exists error, got: %v", err)
}

// TestCreateConnection_NilIntegrationFactory_StorageConnectionStillCreated proves the
// post-create storage validation block (added to wire ValidateConnection into
// CreateConnection) does not panic when the service is constructed without an
// IntegrationFactory — some test/bootstrap paths do this, mirroring the existing
// QuickBooks post-create block's own nil guard.
func TestCreateConnection_NilIntegrationFactory_StorageConnectionStillCreated(t *testing.T) {
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}
	cfg.FlexpriceGCSExports.Bucket = "flexprice-managed-gcs-bucket"

	svc, connRepo := newConnectionServiceForTestWithConfig(t, cfg)
	ctx := testutil.SetupContext()

	req := dto.CreateConnectionRequest{
		Name:         "Managed GCS Connection",
		ProviderType: types.SecretProviderGCS,
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				IsFlexpriceManaged: true,
			},
		},
	}

	resp, err := svc.CreateConnection(ctx, req)
	require.NoError(t, err, "nil IntegrationFactory must not panic or block storage connection creation")
	require.NotNil(t, resp)

	stored, err := connRepo.Get(ctx, resp.ID)
	require.NoError(t, err)
	require.Equal(t, resp.ID, stored.ID)
}

// TestCreateConnection_NonStorageProvider_NoValidationAttempted proves the new
// post-create validation block only triggers for S3/GCS providers: a Stripe connection
// created with a nil IntegrationFactory must succeed exactly as before, confirming the
// new code path is correctly scoped and doesn't touch unrelated provider types.
func TestCreateConnection_NonStorageProvider_NoValidationAttempted(t *testing.T) {
	svc, connRepo := newConnectionServiceForTest(t)
	ctx := testutil.SetupContext()

	req := dto.CreateConnectionRequest{
		Name:         "Stripe Connection",
		ProviderType: types.SecretProviderStripe,
		EncryptedSecretData: types.ConnectionMetadata{
			Stripe: &types.StripeConnectionMetadata{
				PublishableKey: "pk_test_1",
				SecretKey:      "sk_test_1",
			},
		},
	}

	resp, err := svc.CreateConnection(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	stored, err := connRepo.Get(ctx, resp.ID)
	require.NoError(t, err)
	require.Equal(t, resp.ID, stored.ID)
}

// --- Pre-persist storage validation tests (validateStorageReachable) ---
//
// These exercise the moved validation with a REAL *integration.Factory (see
// newConnectionServiceWithRealFactoryForTest), not a nil one, which is what the previous
// task could not do because ServiceParams.IntegrationFactory is a concrete *integration.Factory
// with no fake/mock seam.
//
// The deterministic, network-free failure lever available without new plumbing is the
// empty-credential guard inside Factory.buildS3Storage/buildGCSStorage (see
// internal/integration/factory.go): both return a validation error before ever constructing
// an s3backend/gcsbackend client when the connection's decrypted credentials are empty. This
// is enough to prove the create/update-before-persist behavior end-to-end.
//
// What is NOT covered here, and why: a true "credentials are valid but bucket does not exist /
// is unreachable" scenario requires ValidateConnection to make a real network call
// (gcsbackend.client.ValidateConnection calls c.gcs.Bucket(...).Attrs(ctx); s3backend is
// analogous). internal/storage/{s3backend,gcsbackend}/client_test.go fake this via
// httptest + Config.EndpointURL, but Config.EndpointURL is a backend-package-only field —
// internal/integration/factory.go's buildS3Storage/buildGCSStorage never set EndpointURL on
// the s3backend.Config/gcsbackend.Config they construct (confirmed: no EndpointURL reference
// anywhere in factory.go), and config.Configuration exposes no such override either. So a real
// *integration.Factory in this package cannot be pointed at a fake httptest bucket today.
// Reaching that scenario would require adding an EndpointURL passthrough from
// config.Configuration (or the connection's SyncConfig) through buildS3Storage/buildGCSStorage
// to s3backend.Config/gcsbackend.Config — genuinely new plumbing, which this task's brief
// explicitly says not to add. Also note UpdateConnection has no S3/GCS branch in its
// EncryptedSecretData merge logic at all (only QuickBooks/ZohoBooks/Whop are merged), so an
// update cannot even change stored credentials to something bad — only SyncConfig fields
// (e.g. bucket name) are updatable for S3/GCS today, and StorageExportConfig.Validate()
// requires Bucket to be non-empty for non-managed connections, so it cannot be blanked out
// either. This is reported rather than faked.

// TestCreateConnection_S3EmptyCredentials_FailsBeforePersistAndNoRowIsWritten proves the core
// regression this task is about on the create path: a storage connection whose credentials
// fail validation must error out AND must never be written to the repository (no row exists
// afterward, and no delete-after-create rollback path runs since nothing was ever persisted).
func TestCreateConnection_S3EmptyCredentials_FailsBeforePersistAndNoRowIsWritten(t *testing.T) {
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}
	svc, connRepo := newConnectionServiceWithRealFactoryForTest(t, cfg)
	ctx := testutil.SetupContext()

	req := dto.CreateConnectionRequest{
		Name:         "Customer S3 Connection",
		ProviderType: types.SecretProviderS3,
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				Bucket: "customer-bucket",
				Region: "us-east-1",
			},
		},
		EncryptedSecretData: types.ConnectionMetadata{
			S3: &types.S3ConnectionMetadata{
				AWSAccessKeyID:     "",
				AWSSecretAccessKey: "",
			},
		},
	}

	resp, err := svc.CreateConnection(ctx, req)
	require.Error(t, err, "empty S3 credentials must fail validation before the row is persisted")
	require.Nil(t, resp)

	all, err := connRepo.List(ctx, &types.ConnectionFilter{ProviderType: types.SecretProviderS3})
	require.NoError(t, err)
	require.Empty(t, all, "no S3 connection row must be persisted when pre-create validation fails")
}

// TestCreateConnection_GCSCustomer_FailsBeforePersistAndNoRowIsWritten is the GCS
// counterpart to the S3 test above: GCS BYOB (customer service-account-JSON connections) is
// out of scope, so a non-managed GCS connection must fail pre-persist validation regardless
// of credentials.
func TestCreateConnection_GCSCustomer_FailsBeforePersistAndNoRowIsWritten(t *testing.T) {
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}
	svc, connRepo := newConnectionServiceWithRealFactoryForTest(t, cfg)
	ctx := testutil.SetupContext()

	req := dto.CreateConnectionRequest{
		Name:         "Customer GCS Connection",
		ProviderType: types.SecretProviderGCS,
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				Bucket: "customer-bucket",
			},
		},
	}

	resp, err := svc.CreateConnection(ctx, req)
	require.Error(t, err, "GCS BYOB connections must fail validation before the row is persisted")
	require.Nil(t, resp)

	all, err := connRepo.List(ctx, &types.ConnectionFilter{ProviderType: types.SecretProviderGCS})
	require.NoError(t, err)
	require.Empty(t, all, "no GCS connection row must be persisted when pre-create validation fails")
}

// TestUpdateConnection_S3EmptyCredentials_FailsBeforePersistAndStoredBucketUnchanged is the
// key regression test for the update path: an existing S3 connection (seeded directly into the
// repo with empty/invalid credentials, bypassing CreateConnection's own validation, since a
// real network-reachable bucket is not available here — see the file-level comment) gets a
// bucket-changing update. validateStorageReachable must reject it before ConnectionRepo.Update
// runs, and the previously-stored SyncConfig (bucket) must be exactly what it was before the
// update attempt — proving no partial/bad write landed.
func TestUpdateConnection_S3EmptyCredentials_FailsBeforePersistAndStoredBucketUnchanged(t *testing.T) {
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}
	svc, connRepo := newConnectionServiceWithRealFactoryForTest(t, cfg)
	ctx := testutil.SetupContext()

	existing := &connection.Connection{
		ID:            "conn_s3_existing",
		EnvironmentID: types.GetEnvironmentID(ctx),
		Name:          "Customer S3 Connection",
		ProviderType:  types.SecretProviderS3,
		BaseModel: types.BaseModel{
			TenantID: types.GetTenantID(ctx),
			Status:   types.StatusPublished,
		},
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				Bucket: "original-bucket",
				Region: "us-east-1",
			},
		},
		EncryptedSecretData: types.ConnectionMetadata{
			S3: &types.S3ConnectionMetadata{
				AWSAccessKeyID:     "",
				AWSSecretAccessKey: "",
			},
		},
	}
	require.NoError(t, connRepo.Create(ctx, existing))

	updateReq := dto.UpdateConnectionRequest{
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				Bucket: "attacker-controlled-bucket",
				Region: "us-east-1",
			},
		},
	}

	updated, err := svc.UpdateConnection(ctx, existing.ID, updateReq)
	require.Error(t, err, "empty S3 credentials must fail validation before the update is persisted")
	require.Nil(t, updated)

	stored, err := connRepo.Get(ctx, existing.ID)
	require.NoError(t, err)
	require.Equal(t, "original-bucket", stored.SyncConfig.Storage.Bucket,
		"the bad bucket must never be persisted; the original config must remain untouched")
}

// TestUpdateConnection_NonStorageProvider_Unaffected proves the update-path validation is
// scoped identically to create: a non-storage provider (Stripe) with a real IntegrationFactory
// wired in must update successfully without ever attempting storage validation.
func TestUpdateConnection_NonStorageProvider_Unaffected(t *testing.T) {
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}
	svc, connRepo := newConnectionServiceWithRealFactoryForTest(t, cfg)
	ctx := testutil.SetupContext()

	created, err := svc.CreateConnection(ctx, dto.CreateConnectionRequest{
		Name:         "Stripe Connection",
		ProviderType: types.SecretProviderStripe,
		EncryptedSecretData: types.ConnectionMetadata{
			Stripe: &types.StripeConnectionMetadata{
				PublishableKey: "pk_test_1",
				SecretKey:      "sk_test_1",
			},
		},
	})
	require.NoError(t, err)

	updated, err := svc.UpdateConnection(ctx, created.ID, dto.UpdateConnectionRequest{
		Name: "Stripe Connection Renamed",
	})
	require.NoError(t, err, "non-storage provider update must not attempt storage validation")
	require.NotNil(t, updated)
	require.Equal(t, "Stripe Connection Renamed", updated.Name)

	stored, err := connRepo.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "Stripe Connection Renamed", stored.Name)
}

// TestUpdateConnection_NilIntegrationFactory_StillSucceeds proves an update to an S3
// connection with a nil IntegrationFactory (the pre-existing service construction used
// throughout the rest of this file) still succeeds, mirroring the equivalent create-path test.
func TestUpdateConnection_NilIntegrationFactory_StillSucceeds(t *testing.T) {
	svc, connRepo := newConnectionServiceForTest(t)
	ctx := testutil.SetupContext()

	created, err := svc.CreateConnection(ctx, dto.CreateConnectionRequest{
		Name:         "Customer S3 Connection",
		ProviderType: types.SecretProviderS3,
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				Bucket: "original-bucket",
				Region: "us-east-1",
			},
		},
		EncryptedSecretData: types.ConnectionMetadata{
			S3: &types.S3ConnectionMetadata{
				AWSAccessKeyID:     "AKIAEXAMPLE",
				AWSSecretAccessKey: "secretexample",
			},
		},
	})
	require.NoError(t, err, "nil IntegrationFactory must not block create")

	updated, err := svc.UpdateConnection(ctx, created.ID, dto.UpdateConnectionRequest{
		SyncConfig: &types.SyncConfig{
			Storage: &types.StorageExportConfig{
				Bucket: "updated-bucket",
				Region: "us-east-1",
			},
		},
	})
	require.NoError(t, err, "nil IntegrationFactory must not block update")
	require.NotNil(t, updated)

	stored, err := connRepo.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "updated-bucket", stored.SyncConfig.Storage.Bucket)
}
