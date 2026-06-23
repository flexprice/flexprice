package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestWebhookLoggingMiddleware_BuffersBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	bodyRead := ""
	router := gin.New()
	router.Use(WebhookLoggingMiddleware(nil, nil))
	router.POST("/v1/webhooks/stripe/:tenant_id/:environment_id", func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		bodyRead = string(b)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/v1/webhooks/stripe/t_xxx/env_yyy",
		strings.NewReader(`{"event":"test"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, `{"event":"test"}`, bodyRead, "handler must still read the buffered body")
}
