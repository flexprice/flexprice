package middleware

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainIncomingWebhookEvent "github.com/flexprice/flexprice/internal/domain/incomingwebhookevent"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestExtractProvider(t *testing.T) {
	cases := []struct {
		path     string
		expected string
	}{
		{"/v1/webhooks/stripe/t_xxx/env_yyy", "stripe"},
		{"/v1/webhooks/paddle/t_aaa/env_bbb", "paddle"},
		{"/v1/webhooks/chargebee/t_ccc/env_ddd", "chargebee"},
		{"/v1/webhooks/", "unknown"},
		{"", "unknown"},
	}
	for _, c := range cases {
		assert.Equal(t, c.expected, extractProvider(c.path), "path: %s", c.path)
	}
}

type captureRepo struct {
	got *domainIncomingWebhookEvent.IncomingWebhookEvent
}

func (r *captureRepo) Create(_ context.Context, e *domainIncomingWebhookEvent.IncomingWebhookEvent) error {
	r.got = e
	return nil
}

func TestWebhookLoggingMiddleware_Redaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		headers    map[string]string
		body       string
		wantHeader map[string]string
		wantBody   map[string]any
		wantRaw    string
	}{
		{
			name:       "sensitive headers case-insensitive",
			headers:    map[string]string{"authorization": "Bearer abc", "X-Api-Key": "k", "Cookie": "s=1", "Content-Type": "application/json"},
			body:       `{"a":"b"}`,
			wantHeader: map[string]string{"Authorization": "[redacted]", "X-Api-Key": "[redacted]", "Cookie": "[redacted]", "Content-Type": "application/json"},
			wantBody:   map[string]any{"a": "b"},
		},
		{
			name:     "top-level and nested body keys",
			body:     `{"secret_token":"s","Password":"p","n":{"access_token":"t","ok":1},"l":[{"refresh_token":"r"}]}`,
			wantBody: map[string]any{"secret_token": "[redacted]", "Password": "[redacted]", "n": map[string]any{"access_token": "[redacted]", "ok": float64(1)}, "l": []any{map[string]any{"refresh_token": "[redacted]"}}},
		},
		{
			name:     "key variants and bare keys",
			body:     `{"apiKey":"a","api-key":"b","API_KEY":"c","token":"t","secret":"s","authorization":"x","private_key":"p","id_token":"i","webhook_secret":"w","signing_secret":"g","client_secret":"c"}`,
			wantBody: map[string]any{"apiKey": "[redacted]", "api-key": "[redacted]", "API_KEY": "[redacted]", "token": "[redacted]", "secret": "[redacted]", "authorization": "[redacted]", "private_key": "[redacted]", "id_token": "[redacted]", "webhook_secret": "[redacted]", "signing_secret": "[redacted]", "client_secret": "[redacted]"},
		},
		{
			name:     "moyasar nested source token",
			body:     `{"type":"payment_paid","data":{"id":"p1","source":{"type":"creditcard","token":"token_abc"}}}`,
			wantBody: map[string]any{"type": "payment_paid", "data": map[string]any{"id": "p1", "source": map[string]any{"type": "creditcard", "token": "[redacted]"}}},
		},
		{
			name:    "huge number does not fail open",
			headers: map[string]string{"Content-Type": "application/json"},
			body:    `{"n":1e400,"token":"leak"}`,
			wantRaw: `{"n":1e400,"token":"[redacted]"}`,
		},
		{
			name:    "unparseable json placeholder",
			body:    `{"token":"leak",`,
			wantRaw: "[redacted: unparseable body]",
		},
		{
			name:    "large integer not rounded",
			body:    `{"id":9007199254740993,"token":"t"}`,
			wantRaw: `{"id":9007199254740993,"token":"[redacted]"}`,
		},
		{
			name:    "form encoded",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    "a=1&token=leak&client-secret=z",
			wantRaw: "a=1&client-secret=%5Bredacted%5D&token=%5Bredacted%5D",
		},
		{
			name:    "plain text passes through",
			headers: map[string]string{"Content-Type": "text/plain"},
			body:    "token=leak",
			wantRaw: "token=leak",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &captureRepo{}
			var seen string
			r := gin.New()
			r.POST("/v1/webhooks/stripe/:tenant_id/:environment_id", WebhookLoggingMiddleware(nil, repo), func(c *gin.Context) {
				b, _ := io.ReadAll(c.Request.Body)
				seen = string(b)
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/stripe/t/e", strings.NewReader(tc.body))
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			r.ServeHTTP(httptest.NewRecorder(), req)

			assert.Equal(t, tc.body, seen)
			for k, v := range tc.wantHeader {
				assert.Equal(t, []string{v}, repo.got.Headers[k], k)
			}
			if tc.wantRaw != "" {
				assert.Equal(t, tc.wantRaw, repo.got.Body)
				return
			}
			var got map[string]any
			assert.NoError(t, json.Unmarshal([]byte(repo.got.Body), &got))
			assert.Equal(t, tc.wantBody, got)
		})
	}
}
