package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type plainEncryption struct{ security.EncryptionService }

func (plainEncryption) Encrypt(s string) (string, error) { return s, nil }
func (plainEncryption) Decrypt(s string) (string, error) { return s, nil }

func TestStoreOAuthSessionIgnoresReservedMetadataKeys(t *testing.T) {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "tenant_1")
	ctx = types.SetEnvironmentID(ctx, "env_1")

	log := logger.NewNoopLogger()
	params := ServiceParams{Logger: log, DB: testutil.NewMockPostgresClient(log), SettingsRepo: testutil.NewInMemorySettingsStore()}
	svc := NewOAuthService(testutil.NewInMemoryConnectionStore(), plainEncryption{}, nil, nil, log, params)

	expiresAt := clock.Now().Add(10 * time.Minute)
	session := &types.OAuthSession{
		SessionID:     "real_session",
		Provider:      types.OAuthProviderZohoBooks,
		TenantID:      "tenant_1",
		EnvironmentID: "env_1",
		Name:          "zoho",
		Credentials:   map[string]string{types.OAuthCredentialClientID: "id", types.OAuthCredentialClientSecret: "secret"},
		CSRFState:     "real_csrf",
		ExpiresAt:     expiresAt,
		Metadata: map[string]string{
			"session_id":        "evil_session",
			"csrf_state":        "evil_csrf",
			"expires_at":        "2999-01-01T00:00:00Z",
			"oauth_provider":    "quickbooks",
			"credentials":       "evil",
			"sync_config":       "evil",
			"billing_entity_id": "evil",
			"organization_name": "kept",
		},
	}
	require.NoError(t, svc.StoreOAuthSession(ctx, session))

	got, err := svc.GetOAuthSession(ctx, "real_session")
	require.NoError(t, err)
	require.Equal(t, "real_session", got.SessionID)
	require.Equal(t, "real_csrf", got.CSRFState)
	require.Equal(t, types.OAuthProviderZohoBooks, got.Provider)
	require.WithinDuration(t, expiresAt, got.ExpiresAt, time.Second)
	require.Equal(t, "kept", got.Metadata["organization_name"])
	require.Len(t, got.Credentials, 2)
	require.Nil(t, got.BillingEntityID)
	require.NotContains(t, got.Metadata, "billing_entity_id")

	_, err = svc.GetOAuthSession(ctx, "evil_session")
	require.Error(t, err)
}
