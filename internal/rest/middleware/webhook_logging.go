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
			if isSensitiveKey(k) {
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
			Body:          redactWebhookBody(peek, c.GetHeader("Content-Type"), c.GetHeader("Content-Encoding")),
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

const unparseableBody = "[redacted: unparseable body]"

// isSensitiveKey matches header names and body keys.
func isSensitiveKey(name string) bool {
	name = strings.ReplaceAll(strings.ToLower(name), "-", "_")
	for _, part := range []string{"authorization", "api_key", "apikey", "cookie", "secret", "token", "signature", "password", "private_key"} {
		if strings.Contains(name, part) {
			return true
		}
	}
	return false
}

const encodedBody = "[redacted: encoded body]"

// isCardKey matches card fields; number/exp need a card parent.
func isCardKey(key, parent string) bool {
	key = strings.ToLower(key)
	for _, part := range []string{"cvc", "cvv", "card_number"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	if key == "pan" {
		return true
	}
	parent = strings.ToLower(parent)
	if parent != "source" && parent != "card" {
		return false
	}
	return key == "number" || strings.HasPrefix(key, "exp")
}

// redactWebhookBody returns a copy safe to store; other bodies pass through.
func redactWebhookBody(body []byte, contentType, contentEncoding string) string {
	if enc := strings.TrimSpace(contentEncoding); enc != "" && !strings.EqualFold(enc, "identity") {
		return encodedBody
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return string(body)
	}
	isForm := mediaType == "application/x-www-form-urlencoded"
	looksJSON := strings.Contains(mediaType, "json") ||
		((trimmed[0] == '{' || trimmed[0] == '[') && (!isForm || json.Valid(trimmed)))
	if !looksJSON {
		if !isForm {
			return string(body)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return unparseableBody
		}
		for k := range form {
			if isSensitiveKey(k) || isCardKey(k, "") {
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
	out, err := json.Marshal(redactJSON(doc, ""))
	if err != nil {
		return unparseableBody
	}
	return string(out)
}

// redactJSON returns v with sensitive values replaced, including embedded JSON strings.
func redactJSON(v any, parentKey string) any {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if isSensitiveKey(k) || isCardKey(k, parentKey) {
				t[k] = redactedValue
				continue
			}
			t[k] = redactJSON(child, k)
		}
	case []any:
		for i, child := range t {
			t[i] = redactJSON(child, parentKey)
		}
	case string:
		trimmed := strings.TrimSpace(t)
		if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
			return t
		}
		var inner any
		if err := json.Unmarshal([]byte(trimmed), &inner); err != nil {
			return t
		}
		out, err := json.Marshal(redactJSON(inner, parentKey))
		if err != nil {
			return redactedValue
		}
		return string(out)
	}
	return v
}
