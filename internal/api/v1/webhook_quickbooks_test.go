package v1

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const quickBooksTestBody = `{"eventNotifications":[]}`

func setupQuickBooksWebhookHandler(t *testing.T, verifierToken string) *WebhookHandler {
	t.Helper()

	cfg := &config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
		Secrets: config.SecretsConfig{EncryptionKey: testutil.NewEncryptionKey()},
	}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)

	encryptionSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)

	encrypt := func(v string) string {
		out, encErr := encryptionSvc.Encrypt(v)
		require.NoError(t, encErr)
		return out
	}

	qbMetadata := &types.QuickBooksConnectionMetadata{
		ClientID:     encrypt("client-id"),
		ClientSecret: encrypt("client-secret"),
		RealmID:      "realm_test",
		Environment:  "sandbox",
	}
	if verifierToken != "" {
		qbMetadata.WebhookVerifierToken = encrypt(verifierToken)
	}

	connRepo := testutil.NewInMemoryConnectionStore()
	conn := &connection.Connection{
		ID:                  types.GenerateUUIDWithPrefix("conn"),
		Name:                "test-quickbooks-connection",
		ProviderType:        types.SecretProviderQuickBooks,
		EncryptedSecretData: types.ConnectionMetadata{QuickBooks: qbMetadata},
		EnvironmentID:       "env_test",
		BaseModel:           types.BaseModel{TenantID: "tenant_test", Status: types.StatusPublished},
	}
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_test"), "env_test")
	require.NoError(t, connRepo.Create(ctx, conn))

	factory := integration.NewFactory(
		cfg, log, connRepo,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		encryptionSvc,
		nil,
		testutil.NewInMemoryRedisLocker(nil),
		nil,
	)

	return NewWebhookHandler(cfg, nil, log, factory, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func quickBooksSignature(token, body string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(body))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestHandleQuickBooksWebhook_SignatureRequired(t *testing.T) {
	const token = "verifier-token-for-test"

	tests := []struct {
		name       string
		storedTok  string
		signature  func() string
		wantStatus int
	}{
		{name: "missing header", storedTok: token, signature: func() string { return "" }, wantStatus: http.StatusUnauthorized},
		{name: "missing verifier token", storedTok: "", signature: func() string { return quickBooksSignature(token, quickBooksTestBody) }, wantStatus: http.StatusUnauthorized},
		{name: "bad signature", storedTok: token, signature: func() string { return quickBooksSignature("other-token", quickBooksTestBody) }, wantStatus: http.StatusUnauthorized},
		{name: "valid signature", storedTok: token, signature: func() string { return quickBooksSignature(token, quickBooksTestBody) }, wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := setupQuickBooksWebhookHandler(t, tt.storedTok)

			router := gin.New()
			router.POST("/v1/webhooks/quickbooks/:tenant_id/:environment_id", handler.HandleQuickBooksWebhook)

			req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/quickbooks/tenant_test/env_test",
				strings.NewReader(quickBooksTestBody))
			if sig := tt.signature(); sig != "" {
				req.Header.Set("intuit-signature", sig)
			}
			w := httptest.NewRecorder()

			require.NotPanics(t, func() { router.ServeHTTP(w, req) })

			if tt.wantStatus == http.StatusUnauthorized {
				requireUnauthorizedJSON(t, w)
				return
			}
			require.Equal(t, tt.wantStatus, w.Code)
		})
	}
}
