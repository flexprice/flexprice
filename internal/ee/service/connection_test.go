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
