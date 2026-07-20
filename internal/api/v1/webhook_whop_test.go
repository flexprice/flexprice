package v1

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

// setupWhopWebhookHandler builds a WebhookHandler wired to a real integration.Factory
// backed by an in-memory connection repository containing a single published Whop
// connection. All non-Whop service dependencies are left nil: when signature
// verification fails, HandleWhopWebhook must return before touching any of them,
// so a nil-dereference would itself prove the processing path was reached.
func setupWhopWebhookHandler(t *testing.T, webhookSecret string) (*WebhookHandler, *logger.Logger) {
	t.Helper()

	cfg := &config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
		Secrets: config.SecretsConfig{EncryptionKey: "test-encryption-key-32-bytes!!!"},
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
		nil, // db
		nil, // webhookService
	)

	return handler, log
}

func TestHandleWhopWebhook_RejectsMissingSignatureWhenSecretConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, _ := setupWhopWebhookHandler(t, "test_webhook_secret")

	router := gin.New()
	router.POST("/v1/webhooks/whop/:tenant_id/:environment_id", handler.HandleWhopWebhook)

	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/whop/tenant_test/env_test",
		strings.NewReader(`{"type":"payment.succeeded","data":{}}`))
	// deliberately NOT setting X-Whop-Signature header
	w := httptest.NewRecorder()

	// If signature verification is bypassed, HandleWhopWebhook proceeds to call
	// whopIntegration.WebhookHandler.HandleWebhookEvent with nil service
	// dependencies, which would panic. A panic here (not a clean 200) is itself
	// proof the fix regressed — recover and fail explicitly so the failure mode
	// is legible instead of a raw test-runner crash.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("HandleWhopWebhook panicked (implies it reached HandleWebhookEvent without a valid signature): %v", r)
		}
	}()

	router.ServeHTTP(w, req)

	// Match Moyasar: always return 200 to avoid retry storms even when rejected.
	require.Equal(t, http.StatusOK, w.Code)
}

func TestHandleWhopWebhook_RejectsInvalidSignatureWhenSecretConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, _ := setupWhopWebhookHandler(t, "test_webhook_secret")

	router := gin.New()
	router.POST("/v1/webhooks/whop/:tenant_id/:environment_id", handler.HandleWhopWebhook)

	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/whop/tenant_test/env_test",
		strings.NewReader(`{"type":"payment.succeeded","data":{}}`))
	req.Header.Set("X-Whop-Signature", "deadbeef")
	w := httptest.NewRecorder()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("HandleWhopWebhook panicked (implies it reached HandleWebhookEvent with an invalid signature): %v", r)
		}
	}()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

func TestHandleWhopWebhook_AcceptsValidSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)

	secret := "test_webhook_secret"
	handler, _ := setupWhopWebhookHandler(t, secret)

	router := gin.New()
	router.POST("/v1/webhooks/whop/:tenant_id/:environment_id", handler.HandleWhopWebhook)

	// An unrecognized event type ("no-op.event") is used so HandleWebhookEvent's
	// default branch (log + return nil) is hit instead of a real payment/invoice
	// path that would dereference the nil service dependencies.
	body := `{"type":"no-op.event","data":{}}`

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	validSig := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/whop/tenant_test/env_test", strings.NewReader(body))
	req.Header.Set("X-Whop-Signature", validSig)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

// Match Moyasar: connections without webhook_secret keep working (process events,
// no signature required). Uses no-op.event so nil service deps are never touched.
func TestHandleWhopWebhook_AllowsWhenSecretNotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, _ := setupWhopWebhookHandler(t, "")

	router := gin.New()
	router.POST("/v1/webhooks/whop/:tenant_id/:environment_id", handler.HandleWhopWebhook)

	body := `{"type":"no-op.event","data":{}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/whop/tenant_test/env_test", strings.NewReader(body))
	// deliberately no X-Whop-Signature — should still process when secret unset
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}
