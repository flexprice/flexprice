package middleware

import (
	"bytes"
	"io"
	"strings"

	"github.com/flexprice/flexprice/internal/config"
	domainIncomingWebhookEvent "github.com/flexprice/flexprice/internal/domain/incomingwebhookevent"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

const maxWebhookBodyBytes = 1 << 20 // 1 MiB

// WebhookLoggingMiddleware logs every inbound webhook request and optionally
// persists it to the incoming_webhook_events table when the tenant_id or
// environment_id matches the config whitelist.
func WebhookLoggingMiddleware(
	cfg *config.Configuration,
	log *logger.Logger,
	repo domainIncomingWebhookEvent.Repository,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Read up to maxWebhookBodyBytes+1 to detect oversized payloads without
		// loading arbitrarily large bodies into memory.
		peek, _ := io.ReadAll(io.LimitReader(c.Request.Body, int64(maxWebhookBodyBytes)+1))
		bodyTooLarge := len(peek) > maxWebhookBodyBytes

		// Always restore the full body so the downstream handler is unaffected.
		if bodyTooLarge {
			c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(peek), c.Request.Body))
		} else {
			c.Request.Body = io.NopCloser(bytes.NewReader(peek))
		}

		tenantID := c.Param("tenant_id")
		environmentID := c.Param("environment_id")
		provider := extractProvider(c.Request.URL.Path)
		requestID := types.GetRequestID(c.Request.Context())

		headers := make(map[string][]string, len(c.Request.Header))
		for k, v := range c.Request.Header {
			headers[k] = v
		}

		persisted := false
		if bodyTooLarge {
			if log != nil {
				log.Warn(c.Request.Context(), "webhook body exceeds max size, skipping db persistence",
					"max_bytes", maxWebhookBodyBytes,
					"provider", provider,
					"tenant_id", tenantID,
					"environment_id", environmentID,
					"request_id", requestID,
				)
			}
		} else if repo != nil && shouldPersistRequest(cfg.WebhookLogging, tenantID, environmentID) {
			req := &domainIncomingWebhookEvent.IncomingWebhookEvent{
				ID:            types.GenerateUUID(),
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
				if log != nil {
					log.Error(c.Request.Context(), "failed to persist webhook event",
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
				"payload_size_bytes", len(peek),
				"body_too_large", bodyTooLarge,
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
