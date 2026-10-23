package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/url"
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
			if isSensitiveWebhookHeader(k) {
				headers[k] = []string{redactedValue}
				continue
			}
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
			Body:          redactWebhookBody(peek, c.GetHeader("Content-Type")),
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

const redactedValue = "[redacted]"

var sensitiveHeaderParts = []string{"authorization", "api-key", "apikey", "cookie", "secret", "token", "signature"}

func isSensitiveWebhookHeader(name string) bool {
	name = strings.ToLower(name)
	for _, part := range sensitiveHeaderParts {
		if strings.Contains(name, part) {
			return true
		}
	}
	return false
}

const unparseableBody = "[redacted: unparseable body]"

func isSensitiveKey(name string) bool {
	switch strings.ReplaceAll(strings.ToLower(name), "-", "_") {
	case "token", "secret", "authorization", "private_key", "id_token",
		"webhook_secret", "signing_secret", "client_secret", "access_token",
		"refresh_token", "password", "secret_token", "api_key", "apikey":
		return true
	}
	return false
}

// redactWebhookBody returns a copy safe to store; other bodies pass through.
func redactWebhookBody(body []byte, contentType string) string {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return string(body)
	}
	looksJSON := strings.Contains(mediaType, "json") || trimmed[0] == '{' || trimmed[0] == '['
	if !looksJSON {
		if mediaType != "application/x-www-form-urlencoded" {
			return string(body)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return unparseableBody
		}
		for k := range form {
			if isSensitiveKey(k) {
				form[k] = []string{redactedValue}
			}
		}
		return form.Encode()
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return unparseableBody
	}
	if _, err := dec.Token(); err != io.EOF {
		return unparseableBody
	}
	redactJSON(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		return unparseableBody
	}
	return string(out)
}

func redactJSON(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if isSensitiveKey(k) {
				t[k] = redactedValue
				continue
			}
			redactJSON(child)
		}
	case []any:
		for _, child := range t {
			redactJSON(child)
		}
	}
}
