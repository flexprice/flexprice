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
			name:    "malformed json object under form content type",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    `{"token":"leak",,`,
			wantRaw: "[redacted: unparseable body]",
		},
		{
			name:    "malformed json array under form content type",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    `[{"token":"leak"`,
			wantRaw: "[redacted: unparseable body]",
		},
		{
			name:    "malformed array space before object",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    `[ {"token":"leak",,`,
			wantRaw: "[redacted: unparseable body]",
		},
		{
			name:    "malformed array newline before object",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    "[\n{\"token\":\"leak\"",
			wantRaw: "[redacted: unparseable body]",
		},
		{
			name:    "malformed array scalar number start",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    `[1,"leak"`,
			wantRaw: "[redacted: unparseable body]",
		},
		{
			name:    "malformed array spaced string start",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    `[ "leak"`,
			wantRaw: "[redacted: unparseable body]",
		},
		{
			name:    "malformed array literal start",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    `[true,"leak"`,
			wantRaw: "[redacted: unparseable body]",
		},
		{
			name:    "bracket form key stays form",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    "[name]=x",
			wantRaw: "%5Bname%5D=x",
		},
		{
			name:    "json body under form content type",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    `{"secret_token":"leak"}`,
			wantRaw: `{"secret_token":"[redacted]"}`,
		},
		{
			name:    "bracket-prefixed form field",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    "[items]=one&card_token=leak",
			wantRaw: "%5Bitems%5D=one&card_token=%5Bredacted%5D",
		},
		{
			name:    "provider-specific credential key",
			body:    `{"data":{"source":{"payment_token":"leak","last4":"4242"}}}`,
			wantRaw: `{"data":{"source":{"last4":"4242","payment_token":"[redacted]"}}}`,
		},
		{
			name:    "moyasar card fields",
			body:    `{"data":{"number":"INV-1","source":{"type":"creditcard","number":"4111111111111111","cvc":"123","expiry_month":"12","last4":"4242"}}}`,
			wantRaw: `{"data":{"number":"INV-1","source":{"cvc":"[redacted]","expiry_month":"[redacted]","last4":"4242","number":"[redacted]","type":"creditcard"}}}`,
		},
		{
			name:    "embedded json string",
			body:    `{"payload":"{\"password\":\"x\",\"ok\":1}","list":"[{\"token\":\"t\"}]","note":"{not json"}`,
			wantRaw: `{"list":"[{\"token\":\"[redacted]\"}]","note":"[redacted]","payload":"{\"ok\":1,\"password\":\"[redacted]\"}"}`,
		},
		{
			name:    "embedded json keeps big numbers",
			body:    `{"p":"{\"id\":9007199254740993,\"n\":1e400,\"token\":\"t\"}"}`,
			wantRaw: `{"p":"{\"id\":9007199254740993,\"n\":1e400,\"token\":\"[redacted]\"}"}`,
		},
		{
			name:    "truncated embedded json fails closed",
			body:    `{"p":"{\"access_token\":\"secret\""}`,
			wantRaw: `{"p":"[redacted]"}`,
		},
		{
			name:    "camelCase and bracketed card keys json",
			body:    `{"cardNumber":"4111","card":{"expMonth":"12"},"card[number]":"4111","source[exp_year]":"30","cardCvc":"1"}`,
			wantRaw: `{"card":{"expMonth":"[redacted]"},"cardCvc":"[redacted]","cardNumber":"[redacted]","card[number]":"[redacted]","source[exp_year]":"[redacted]"}`,
		},
		{
			name:    "uppercase card keys json",
			body:    `{"CVC":"123","CARD_NUMBER":"4111","CardNumber":"4111","card[NUMBER]":"4111"}`,
			wantRaw: `{"CARD_NUMBER":"[redacted]","CVC":"[redacted]","CardNumber":"[redacted]","card[NUMBER]":"[redacted]"}`,
		},
		{
			name:    "uppercase card keys form",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    "CVC=123&CARD_NUMBER=4111&CardNumber=4111&card[NUMBER]=4111",
			wantRaw: "CARD_NUMBER=%5Bredacted%5D&CVC=%5Bredacted%5D&CardNumber=%5Bredacted%5D&card%5BNUMBER%5D=%5Bredacted%5D",
		},
		{
			name:    "bracketed card keys form",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    "card[number]=4111&card[exp_month]=12&cardNumber=4111&order[number]=INV-1",
			wantRaw: "cardNumber=%5Bredacted%5D&card%5Bexp_month%5D=%5Bredacted%5D&card%5Bnumber%5D=%5Bredacted%5D&order%5Bnumber%5D=INV-1",
		},
		{
			name:    "json inside form value",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			body:    `payload=%7B%22access_token%22%3A%22x%22%2C%22id%22%3A9007199254740993%7D&bad=%7B%22access_token%22%3A%22x%22`,
			wantRaw: `bad=%5Bredacted%5D&payload=%7B%22access_token%22%3A%22%5Bredacted%5D%22%2C%22id%22%3A9007199254740993%7D`,
		},
		{
			name:    "encoded body placeholder",
			headers: map[string]string{"Content-Encoding": "gzip", "Content-Type": "application/json"},
			body:    "\x1f\x8b\x08binary",
			wantRaw: "[redacted: encoded body]",
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
