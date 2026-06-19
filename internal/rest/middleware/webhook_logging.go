package middleware

import (
	"bytes"
	"io"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	domainIncomingWebhookEvent "github.com/flexprice/flexprice/internal/domain/incomingwebhookevent"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// WebhookLoggingMiddleware logs every inbound webhook request and optionally
// persists it to the webhook_requests table when the tenant_id or environment_id
// matches the config whitelist.
func WebhookLoggingMiddleware(
	cfg *config.Configuration,
	log *logger.Logger,
	repo domainIncomingWebhookEvent.Repository,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Buffer the body so the downstream handler can still read it.
		var buf bytes.Buffer
		body, _ := io.ReadAll(io.TeeReader(c.Request.Body, &buf))
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		tenantID := c.Param("tenant_id")
		environmentID := c.Param("environment_id")
		provider := extractProvider(c.Request.URL.Path)
		requestID := types.GetRequestID(c.Request.Context())

		headers := make(map[string][]string, len(c.Request.Header))
		for k, v := range c.Request.Header {
			headers[k] = v
		}

		persisted := false
		if repo != nil && shouldPersistRequest(cfg.WebhookLogging, tenantID, environmentID) {
			req := &domainIncomingWebhookEvent.IncomingWebhookEvent{
				ID:            types.GenerateUUID(),
				TenantID:      tenantID,
				EnvironmentID: environmentID,
				Provider:      provider,
				Method:        c.Request.Method,
				Path:          c.Request.URL.Path,
				RequestID:     requestID,
				Headers:       headers,
				Body:          string(body),
				CreatedAt:     time.Now().UTC(),
			}
			if err := repo.Create(c.Request.Context(), req); err != nil {
				if log != nil {
					log.Error(c.Request.Context(), "failed to persist webhook request",
						"error", err,
						"provider", provider,
						"tenant_id", tenantID,
						"environment_id", environmentID,
					)
				}
			} else {
				persisted = true
			}
		}

		c.Next()

		if log != nil {
			log.Debug(c.Request.Context(), "inbound webhook request",
				"provider", provider,
				"tenant_id", tenantID,
				"environment_id", environmentID,
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"request_id", requestID,
				"payload_size_bytes", len(body),
				"persisted", persisted,
			)
		}
	}
}

// extractProvider pulls the provider name from a webhook URL path.
// Expected form: /v1/webhooks/{provider}/{tenant_id}/{environment_id}
func extractProvider(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) >= 3 && parts[2] != "" {
		return parts[2]
	}
	return "unknown"
}

// shouldPersistRequest returns true when the request's tenant_id or environment_id
// appears in the config whitelist.
func shouldPersistRequest(cfg config.WebhookLoggingConfig, tenantID, environmentID string) bool {
	for _, tid := range cfg.TenantIDs {
		if tid != "" && tid == tenantID {
			return true
		}
	}
	for _, eid := range cfg.EnvironmentIDs {
		if eid != "" && eid == environmentID {
			return true
		}
	}
	return false
}
