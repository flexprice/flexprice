package hubspot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func mustTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	cfg := &config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	return log
}

func mustTestEncryptionService(t *testing.T) security.EncryptionService {
	t.Helper()
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "031f6bbed1156eca651d48652c17a5bce727514cc804f185aca207153b2915abb79c0f1b53945915866dc3b63f37ea73aa86fc062f13e6008249e30819f87483",
		},
	}
	svc, err := security.NewEncryptionService(cfg, mustTestLogger(t))
	require.NoError(t, err)
	return svc
}

// fakeConnectionRepo overrides only GetByProvider; DeleteDealLineItem never calls any other
// connection.Repository method, so the embedded nil interface is safe.
type fakeConnectionRepo struct {
	connection.Repository
	conn *connection.Connection
}

func (f *fakeConnectionRepo) GetByProvider(ctx context.Context, provider types.SecretProvider) (*connection.Connection, error) {
	return f.conn, nil
}

func newTestClient(t *testing.T, accessToken string) HubSpotClient {
	t.Helper()
	enc := mustTestEncryptionService(t)
	encryptedToken, err := enc.Encrypt(accessToken)
	require.NoError(t, err)
	encryptedSecret, err := enc.Encrypt("test-client-secret")
	require.NoError(t, err)

	conn := &connection.Connection{
		ProviderType: types.SecretProviderHubSpot,
		EncryptedSecretData: types.ConnectionMetadata{
			HubSpot: &types.HubSpotConnectionMetadata{
				AccessToken:  encryptedToken,
				ClientSecret: encryptedSecret,
			},
		},
	}
	return NewClient(&fakeConnectionRepo{conn: conn}, enc, mustTestLogger(t))
}

func TestDeleteDealLineItem_NotFoundIsDistinguishable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	origBaseURL := HubSpotAPIBaseURL
	HubSpotAPIBaseURL = srv.URL
	t.Cleanup(func() { HubSpotAPIBaseURL = origBaseURL })

	client := newTestClient(t, "test-token")
	err := client.DeleteDealLineItem(context.Background(), "line-item-123")
	require.Error(t, err)
	require.True(t, ierr.IsNotFound(err), "expected a not-found error, got: %v", err)
}

func TestDeleteDealLineItem_SuccessOnNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	origBaseURL := HubSpotAPIBaseURL
	HubSpotAPIBaseURL = srv.URL
	t.Cleanup(func() { HubSpotAPIBaseURL = origBaseURL })

	client := newTestClient(t, "test-token")
	err := client.DeleteDealLineItem(context.Background(), "line-item-123")
	require.NoError(t, err)
}
