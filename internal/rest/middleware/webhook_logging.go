package middleware

import (
	"bytes"
	"io"
	"strings"

	domainIncomingWebhookEvent "github.com/flexprice/flexprice/internal/domain/incomingwebhookevent"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// WebhookLoggingMiddleware logs every inbound webhook request and optionally
// persists it to the incoming_webhook_events table.
func WebhookLoggingMiddleware(
	log *logger.Logger,
	repo domainIncomingWebhookEvent.Repository,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		peek, readErr := io.ReadAll(c.Request.Body)
		if readErr != nil {
			if log != nil {
				log.Error(c.Request.Context(), "failed to read webhook request body", "error", readErr)
			}
			c.Next()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(peek))

		tenantID := c.Param("tenant_id")
		environmentID := c.Param("environment_id")
		provider := extractProvider(c.Request.URL.Path)
		requestID := types.GetRequestID(c.Request.Context())

		headers := make(map[string][]string, len(c.Request.Header))
		for k, v := range c.Request.Header {
			headers[k] = v
		}
		req := &domainIncomingWebhookEvent.IncomingWebhookEvent{
			ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INCOMING_WEBHOOK_EVENT),
			TenantID:      tenantID,
			EnvironmentID: environmentID,
			Provider:      provider,
			Method:        c.Request.Method,
			Path:          c.Request.URL.Path,
			RequestID:     requestID,
			Headers:       headers,
			Body:          string(peek),
		}
		if err := repo.Create(c.Request.Context(), req); err != nil {
			log.Error(c.Request.Context(), "failed to persist webhook event",
				"error", err,
				"provider", provider,
				"tenant_id", tenantID,
				"environment_id", environmentID,
			)
		}
		c.Next()
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
