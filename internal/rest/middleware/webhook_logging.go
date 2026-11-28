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
	key = normalizeKey(key)
	for _, part := range []string{"cvc", "cvv", "card_number"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	if key == "pan" {
		return true
	}
	parent = normalizeKey(parent)
	if parent != "source" && parent != "card" {
		for _, prefix := range []string{"source_", "card_"} {
			if rest, ok := strings.CutPrefix(key, prefix); ok {
				return isCardField(rest)
			}
		}
		return false
	}
	return isCardField(key)
}

func isCardField(key string) bool {
	return key == "number" || strings.HasPrefix(key, "exp")
}

// normalizeKey lowercases and maps camelCase and brackets to snake_case.
func normalizeKey(key string) string {
	var b strings.Builder
	for i, r := range key {
		switch {
		case r >= 'A' && r <= 'Z':
			if i > 0 && key[i-1] >= 'a' && key[i-1] <= 'z' {
				b.WriteByte('_')
			}
			b.WriteRune(r + 'a' - 'A')
		case r == '-' || r == '[' || r == '.':
			b.WriteByte('_')
		case r == ']':
		default:
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "_")
}

// jsonShaped reports whether b opens like a JSON object or array.
func jsonShaped(b []byte) bool {
	if b[0] == '{' {
		return true
	}
	if b[0] != '[' {
		return false
	}
	rest := bytes.TrimLeft(b[1:], " \t\n\r")
	if len(rest) == 0 {
		return false
	}
	switch c := rest[0]; {
	case c == '{' || c == '[' || c == '"' || c == '-' || (c >= '0' && c <= '9'):
		return true
	}
	for _, lit := range []string{"true", "false", "null"} {
		if !bytes.HasPrefix(rest, []byte(lit)) {
			continue
		}
		if len(rest) == len(lit) {
			return true
		}
		return strings.IndexByte(" \t\n\r,]", rest[len(lit)]) >= 0
	}
	return false
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
		if jsonShaped(trimmed) {
			return unparseableBody
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return unparseableBody
		}
		for k, values := range form {
			if isSensitiveKey(k) || isCardKey(k, "") {
				form[k] = []string{redactedValue}
				continue
			}
			for i, v := range values {
				values[i] = redactEmbedded(v, k)
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
		return redactEmbedded(t, parentKey)
	}
	return v
}

// redactEmbedded redacts JSON-looking strings; unparseable ones fail closed.
func redactEmbedded(s, parentKey string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return s
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	var inner any
	if err := dec.Decode(&inner); err != nil {
		return redactedValue
	}
	if _, err := dec.Token(); err != io.EOF {
		return redactedValue
	}
	out, err := json.Marshal(redactJSON(inner, parentKey))
	if err != nil {
		return redactedValue
	}
	return string(out)
}
