package v1

import (
	"context"
	"encoding/json"
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

// setupChargebeeWebhookHandler builds a WebhookHandler wired to a real
// integration.Factory backed by an in-memory connection repository holding one
// published Chargebee connection.
//
// webhookUsername/webhookPassword are stored encrypted, mirroring production. Pass
// empty strings to model a legacy connection that predates the Basic Auth
// requirement. storeCorruptCreds writes ciphertext that cannot be decrypted, which
// is the decryption-failure path: the raw encrypted fields look configured, so the
// handler enters the "verify" branch, but the decrypted values arrive blank.
//
// Non-Chargebee service dependencies are deliberately nil: every rejection path
// must return before touching them, so a nil dereference would itself prove the
// request reached event processing.
func setupChargebeeWebhookHandler(t *testing.T, webhookUsername, webhookPassword string, storeCorruptCreds bool) *WebhookHandler {
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
	encryptedSite, err := encryptionSvc.Encrypt("test-site")
	require.NoError(t, err)

	cbMetadata := &types.ChargebeeConnectionMetadata{
		APIKey: encryptedAPIKey,
		Site:   encryptedSite,
	}

	switch {
	case storeCorruptCreds:
		// Non-empty but undecryptable ciphertext.
		cbMetadata.WebhookUsername = "not-valid-ciphertext"
		cbMetadata.WebhookPassword = "not-valid-ciphertext"
	case webhookUsername != "" && webhookPassword != "":
		encUser, err := encryptionSvc.Encrypt(webhookUsername)
		require.NoError(t, err)
		encPass, err := encryptionSvc.Encrypt(webhookPassword)
		require.NoError(t, err)
		cbMetadata.WebhookUsername = encUser
		cbMetadata.WebhookPassword = encPass
	}

	conn := &connection.Connection{
		ID:           types.GenerateUUIDWithPrefix("conn"),
		Name:         "test-chargebee-connection",
		ProviderType: types.SecretProviderChargebee,
		EncryptedSecretData: types.ConnectionMetadata{
			Chargebee: cbMetadata,
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

	return NewWebhookHandler(
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
}

func chargebeeWebhookRequest(t *testing.T, handler *WebhookHandler, username, password string) *httptest.ResponseRecorder {
	t.Helper()

	router := gin.New()
	router.POST("/v1/webhooks/chargebee/:tenant_id/:environment_id", handler.HandleChargebeeWebhook)

	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/chargebee/tenant_test/env_test",
		strings.NewReader(`{"id":"ev_test","event_type":"payment_succeeded","content":{}}`))
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}
	w := httptest.NewRecorder()

	require.NotPanics(t, func() {
		router.ServeHTTP(w, req)
	}, "handler panicked, implying it reached event processing with nil service deps")

	return w
}

// requireUnauthorizedJSON asserts the rejection carries a 401 *and* a JSON error
// body. A bare AbortWithStatus would produce an empty body, and an unguarded
// deferred success render would overwrite the 401 with 200 — this catches both.
func requireUnauthorizedJSON(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()

	require.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body),
		"401 response body is not JSON: %q", w.Body.String())

	errMsg, ok := body["error"].(string)
	require.True(t, ok, `401 response has no string "error" field: %q`, w.Body.String())
	require.NotEmpty(t, errMsg)

	require.NotContains(t, w.Body.String(), "Webhook received",
		"deferred success body overwrote the rejection")
}

// newChargebeeWebhookCredentials returns opaque credentials generated at runtime.
// These are fixture values with no meaning outside a single test run; generating
// them avoids committing credential-shaped string literals that secret scanners
// flag (suppressing the scanner instead would train it to ignore this file).
func newChargebeeWebhookCredentials() (string, string) {
	return types.GenerateUUIDWithPrefix("cbuser"),
		types.GenerateUUIDWithPrefix("cbsecret")
}

// Case 1: creds configured, request sends none.
func TestHandleChargebeeWebhook_RejectsMissingBasicAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	username, password := newChargebeeWebhookCredentials()
	handler := setupChargebeeWebhookHandler(t, username, password, false)

	requireUnauthorizedJSON(t, chargebeeWebhookRequest(t, handler, "", ""))
}

// Case 3: creds configured, request sends wrong ones.
func TestHandleChargebeeWebhook_RejectsInvalidBasicAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	username, password := newChargebeeWebhookCredentials()
	handler := setupChargebeeWebhookHandler(t, username, password, false)

	// Deliberately different from the configured pair.
	wrongUsername, wrongPassword := newChargebeeWebhookCredentials()
	requireUnauthorizedJSON(t, chargebeeWebhookRequest(t, handler, wrongUsername, wrongPassword))
}

// Case 4 — the VAPT finding. Neither side has auth: previously allowed with a
// warning, letting anyone who knew a Chargebee invoice ID forge payment events.
func TestHandleChargebeeWebhook_RejectsWhenNeitherSideHasAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupChargebeeWebhookHandler(t, "", "", false)

	requireUnauthorizedJSON(t, chargebeeWebhookRequest(t, handler, "", ""))
}

// Case 2: no creds configured but the request supplies some.
func TestHandleChargebeeWebhook_RejectsAuthWhenNoneConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupChargebeeWebhookHandler(t, "", "", false)

	username, password := newChargebeeWebhookCredentials()
	requireUnauthorizedJSON(t, chargebeeWebhookRequest(t, handler, username, password))
}

// The decryption-failure bypass: ciphertext present but undecryptable. The raw
// fields read as configured, so the handler enters the verify branch, but
// VerifyWebhookBasicAuth sees blank decrypted creds. It must fail closed rather
// than treating "no configured credential" as "nothing to check".
func TestHandleChargebeeWebhook_RejectsWhenCredentialsCannotBeDecrypted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupChargebeeWebhookHandler(t, "", "", true)

	attackerUsername, attackerPassword := newChargebeeWebhookCredentials()
	requireUnauthorizedJSON(t, chargebeeWebhookRequest(t, handler, attackerUsername, attackerPassword))
}
