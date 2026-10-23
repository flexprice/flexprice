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
			var got map[string]any
			assert.NoError(t, json.Unmarshal([]byte(repo.got.Body), &got))
			assert.Equal(t, tc.wantBody, got)
		})
	}
}
