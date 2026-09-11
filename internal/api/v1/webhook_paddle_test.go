package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type connectionRepoGetByProviderErr struct {
	*testutil.InMemoryConnectionStore
	err error
}

func (s *connectionRepoGetByProviderErr) GetByProvider(_ context.Context, _ types.SecretProvider) (*connection.Connection, error) {
	return nil, s.err
}

func setupPaddleWebhookHandler(t *testing.T, connRepo connection.Repository) *WebhookHandler {
	t.Helper()

	cfg := &config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
		Secrets: config.SecretsConfig{EncryptionKey: testutil.NewEncryptionKey()},
	}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)

	encryptionSvc, err := security.NewEncryptionService(cfg, log)
	require.NoError(t, err)

	if connRepo == nil {
		connRepo = testutil.NewInMemoryConnectionStore()
	}

	factory := integration.NewFactory(
		cfg,
		log,
		connRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		encryptionSvc,
		nil,
		testutil.NewInMemoryRedisLocker(nil),
	)

	return NewWebhookHandler(
		cfg,
		nil,
		log,
		factory,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
}

func paddleWebhookRequest(t *testing.T, handler *WebhookHandler) *httptest.ResponseRecorder {
	t.Helper()

	router := gin.New()
	router.POST("/v1/webhooks/paddle/:tenant_id/:environment_id", handler.HandlePaddleWebhook)

	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/paddle/tenant_test/env_test",
		strings.NewReader(`{"event_type":"transaction.completed"}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestHandlePaddleWebhook_NoConnection_ReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := paddleWebhookRequest(t, setupPaddleWebhookHandler(t, nil))

	require.Equal(t, http.StatusOK, w.Code)
}

func TestHandlePaddleWebhook_ConnectionLookupFailure_Returns500(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &connectionRepoGetByProviderErr{
		InMemoryConnectionStore: testutil.NewInMemoryConnectionStore(),
		err: ierr.NewError("connection to database lost").
			WithHint("Transient database failure").
			Mark(ierr.ErrDatabase),
	}

	w := paddleWebhookRequest(t, setupPaddleWebhookHandler(t, repo))

	require.Equal(t, http.StatusInternalServerError, w.Code)
}
