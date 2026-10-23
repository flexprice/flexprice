package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWebhookDashboardGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Configuration{
		Logging: config.LoggingConfig{Level: "info"},
		RBAC:    config.RBACConfig{RolesConfigPath: "../config/rbac/roles.json"},
	}
	rbacSvc, err := rbac.NewRBACService(cfg)
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	pm := middleware.NewPermissionMiddleware(rbacSvc, log)

	tests := []struct {
		name       string
		roles      []string
		wantStatus int
		wantCalled bool
	}{
		{"read-only role denied", []string{"all_reader"}, http.StatusForbidden, false},
		{"unrelated write role denied", []string{"event_ingestor"}, http.StatusForbidden, false},
		{"write role allowed", []string{"all_writer"}, http.StatusOK, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			r := gin.New()
			r.Use(func(c *gin.Context) {
				ctx := context.WithValue(c.Request.Context(), types.CtxUserType, "user")
				ctx = context.WithValue(ctx, types.CtxRoles, tc.roles)
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			})
			r.GET("/webhooks/dashboard", webhookDashboardGate(pm), func(c *gin.Context) {
				called = true
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/webhooks/dashboard", nil))

			require.Equal(t, tc.wantStatus, w.Code)
			require.Equal(t, tc.wantCalled, called)
		})
	}
}
