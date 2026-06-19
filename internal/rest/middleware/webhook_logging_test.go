package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
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

func TestShouldPersistRequest(t *testing.T) {
	cfg := config.WebhookLoggingConfig{
		TenantIDs:      []string{"t_aaa", "t_bbb"},
		EnvironmentIDs: []string{"env_yyy"},
	}

	assert.True(t, shouldPersistRequest(cfg, "t_aaa", "env_xxx"))  // matches tenant
	assert.True(t, shouldPersistRequest(cfg, "t_zzz", "env_yyy"))  // matches environment
	assert.True(t, shouldPersistRequest(cfg, "t_aaa", "env_yyy"))  // matches both
	assert.False(t, shouldPersistRequest(cfg, "t_zzz", "env_zzz")) // matches neither
	assert.False(t, shouldPersistRequest(cfg, "", ""))              // empty IDs
}

func TestWebhookLoggingMiddleware_BuffersBody(t *testing.T) {
	// nil logger is safe here: empty whitelist means no persist attempt,
	// so the log.Error / log.Debug paths are not reached.
	gin.SetMode(gin.TestMode)

	cfg := &config.Configuration{
		WebhookLogging: config.WebhookLoggingConfig{},
	}

	bodyRead := ""
	router := gin.New()
	router.Use(WebhookLoggingMiddleware(cfg, nil, nil))
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
