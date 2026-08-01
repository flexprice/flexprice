package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

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
	svc, _ := newConnectionServiceForTest(t)
	ctx := testutil.SetupContext()

	req1 := dto.CreateConnectionRequest{
		Name:         "GCS Connection 1",
		ProviderType: types.SecretProviderGCS,
		EncryptedSecretData: types.ConnectionMetadata{
			GCS: &types.GCSConnectionMetadata{
				ServiceAccountJSON: `{"type":"service_account"}`,
			},
		},
	}
	resp1, err := svc.CreateConnection(ctx, req1)
	require.NoError(t, err)
	require.NotNil(t, resp1)

	req2 := dto.CreateConnectionRequest{
		Name:         "GCS Connection 2",
		ProviderType: types.SecretProviderGCS,
		EncryptedSecretData: types.ConnectionMetadata{
			GCS: &types.GCSConnectionMetadata{
				ServiceAccountJSON: `{"type":"service_account"}`,
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
	svc, connRepo := newConnectionServiceForTest(t)
	ctx := testutil.SetupContext()

	req := dto.CreateConnectionRequest{
		Name:         "Customer GCS Connection",
		ProviderType: types.SecretProviderGCS,
		EncryptedSecretData: types.ConnectionMetadata{
			GCS: &types.GCSConnectionMetadata{
				ServiceAccountJSON: `{"type":"service_account"}`,
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
