package v1

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// signWhopWebhookTestPayload mirrors client.VerifyWebhookSignature's Standard
// Webhooks scheme for constructing valid test signatures.
func signWhopWebhookTestPayload(secretB64, webhookID, timestamp, body string) string {
	key, err := base64.StdEncoding.DecodeString(secretB64)
	if err != nil {
		panic(err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(fmt.Sprintf("%s.%s.%s", webhookID, timestamp, body)))
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// setupWhopWebhookHandler builds a WebhookHandler wired to a real integration.Factory
// backed by an in-memory connection repository containing a single published Whop
// connection. All non-Whop service dependencies are left nil: when signature
// verification fails, HandleWhopWebhook must return before touching any of them,
// so a nil-dereference would itself prove the processing path was reached.
func setupWhopWebhookHandler(t *testing.T, webhookSecret string) (*WebhookHandler, *logger.Logger) {
	t.Helper()

	cfg := &config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
		Secrets: config.SecretsConfig{EncryptionKey: testutil.NewEncryptionKey()},
	}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)

	encryptionSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)

	connRepo := testutil.NewInMemoryConnectionStore()

	encryptedAPIKey, err := encryptionSvc.Encrypt("test-api-key")
	require.NoError(t, err)
	encryptedCompanyID, err := encryptionSvc.Encrypt("biz_test123")
	require.NoError(t, err)

	whopMetadata := &types.WhopConnectionMetadata{
		APIKey:    encryptedAPIKey,
		CompanyID: encryptedCompanyID,
	}
	if webhookSecret != "" {
		encryptedSecret, err := encryptionSvc.Encrypt(webhookSecret)
		require.NoError(t, err)
		whopMetadata.WebhookSecret = encryptedSecret
	}

	conn := &connection.Connection{
		ID:           types.GenerateUUIDWithPrefix("conn"),
		Name:         "test-whop-connection",
		ProviderType: types.SecretProviderWhop,
		EncryptedSecretData: types.ConnectionMetadata{
			Whop: whopMetadata,
		},
		EnvironmentID: "env_test",
		BaseModel: types.BaseModel{
			TenantID: "tenant_test",
			Status:   types.StatusPublished,
		},
	}

	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "tenant_test")
	ctx = types.SetEnvironmentID(ctx, "env_test")
	require.NoError(t, connRepo.Create(ctx, conn))

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
		encryptionSvc,
		nil, // temporalSvc
		testutil.NewInMemoryRedisLocker(nil),
	)

	handler := NewWebhookHandler(
		cfg,
		nil, // svixClient
		log,
		factory,
		nil, // customerService
		nil, // paymentService
		nil, // invoiceService
		nil, // planService
		nil, // subscriptionService
		nil, // entityIntegrationMappingService
		nil, // checkoutSessionService
		nil, // refundService
		nil, // db
		nil, // webhookService
	)

	return handler, log
}

// testWhopWebhookSecretB64 mimics the base64-encoded secret Whop issues from its
// dashboard (the raw bytes here are arbitrary).
var testWhopWebhookSecretB64 = base64.StdEncoding.EncodeToString([]byte("test_webhook_secret"))

func TestHandleWhopWebhook_RejectsMissingSignatureWhenSecretConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, _ := setupWhopWebhookHandler(t, testWhopWebhookSecretB64)

	router := gin.New()
	router.POST("/v1/webhooks/whop/:tenant_id/:environment_id", handler.HandleWhopWebhook)

	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/whop/tenant_test/env_test",
		strings.NewReader(`{"type":"payment.succeeded","data":{}}`))
	// deliberately NOT setting the webhook-signature headers
	w := httptest.NewRecorder()

	// If signature verification is bypassed, HandleWhopWebhook proceeds to call
	// whopIntegration.WebhookHandler.HandleWebhookEvent with nil service
	// dependencies, which would panic. A panic here (not a clean 200) is itself
	// proof the fix regressed, so we assert explicitly that it doesn't happen.
	require.NotPanics(t, func() {
		router.ServeHTTP(w, req)
	}, "HandleWhopWebhook panicked (implies it reached HandleWebhookEvent without a valid signature)")

	// Missing signature headers on a secret-configured connection are rejected 401
	// (mirrors Moyasar/Nomod/Chargebee); the deferred success body is skipped.
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleWhopWebhook_RejectsInvalidSignatureWhenSecretConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, _ := setupWhopWebhookHandler(t, testWhopWebhookSecretB64)

	router := gin.New()
	router.POST("/v1/webhooks/whop/:tenant_id/:environment_id", handler.HandleWhopWebhook)

	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/whop/tenant_test/env_test",
		strings.NewReader(`{"type":"payment.succeeded","data":{}}`))
	req.Header.Set("webhook-id", "msg_test123")
	req.Header.Set("webhook-timestamp", fmt.Sprintf("%d", time.Now().Unix()))
	req.Header.Set("webhook-signature", "v1,deadbeef")
	w := httptest.NewRecorder()

	require.NotPanics(t, func() {
		router.ServeHTTP(w, req)
	}, "HandleWhopWebhook panicked (implies it reached HandleWebhookEvent with an invalid signature)")

	// Invalid signature is rejected 401; the deferred success body is skipped.
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleWhopWebhook_AcceptsValidSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, _ := setupWhopWebhookHandler(t, testWhopWebhookSecretB64)

	router := gin.New()
	router.POST("/v1/webhooks/whop/:tenant_id/:environment_id", handler.HandleWhopWebhook)

	// An unrecognized event type ("no-op.event") is used so HandleWebhookEvent's
	// default branch (log + return nil) is hit instead of a real payment/invoice
	// path that would dereference the nil service dependencies.
	body := `{"type":"no-op.event","data":{}}`
	webhookID := "msg_test123"
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	validSig := signWhopWebhookTestPayload(testWhopWebhookSecretB64, webhookID, timestamp, body)

	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/whop/tenant_test/env_test", strings.NewReader(body))
	req.Header.Set("webhook-id", webhookID)
	req.Header.Set("webhook-timestamp", timestamp)
	req.Header.Set("webhook-signature", validSig)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

// Fail closed when no webhook_secret is configured. This route is unauthenticated
// and takes tenant/environment from the URL, so a request that cannot be verified
// against a configured secret must be rejected — it drives invoice payment state.
// Mirrors the Moyasar/Nomod/Chargebee handlers, which reject 401 in this case.
func TestHandleWhopWebhook_RejectsWhenSecretNotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, _ := setupWhopWebhookHandler(t, "")

	router := gin.New()
	router.POST("/v1/webhooks/whop/:tenant_id/:environment_id", handler.HandleWhopWebhook)

	body := `{"type":"no-op.event","data":{}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/whop/tenant_test/env_test", strings.NewReader(body))
	// no signature headers and no configured secret — must be rejected, not processed
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}
