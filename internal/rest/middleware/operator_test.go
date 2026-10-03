package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestOperatorAPIKeyOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const operatorKey = "sk_operator_key"
	cfg := &config.Configuration{Auth: config.AuthConfig{APIKey: config.APIKeyConfig{
		Header: "x-api-key",
		Keys: map[string]config.APIKeyDetails{
			auth.HashAPIKey(operatorKey):   {TenantID: "tenant_op", UserID: "user_op", Name: "operator", IsActive: true},
			auth.HashAPIKey("sk_inactive"): {TenantID: "tenant_op", UserID: "user_op", Name: "retired", IsActive: false},
		},
	}}}

	tests := []struct {
		name   string
		apiKey string
		want   int
	}{
		{name: "operator config key passes", apiKey: operatorKey, want: http.StatusOK},
		{name: "tenant database key is refused", apiKey: "sk_tenant_key", want: http.StatusForbidden},
		{name: "inactive config key is refused", apiKey: "sk_inactive", want: http.StatusForbidden},
		{name: "missing key is refused", apiKey: "", want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/admin/dlq/replay", OperatorAPIKeyOnly(cfg, newTestLogger(t)), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodPost, "/admin/dlq/replay", nil)
			if tt.apiKey != "" {
				req.Header.Set("x-api-key", tt.apiKey)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.want, w.Code)
		})
	}
}
