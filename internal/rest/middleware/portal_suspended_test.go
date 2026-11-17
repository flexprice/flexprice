package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortalInfoRouteWithSessionToken(t *testing.T) {
	cfg := &config.Configuration{Auth: config.AuthConfig{Secret: "test-secret-key-32-bytes-minimum!"}}
	token, _, err := auth.NewFlexpriceAuth(cfg).GenerateSessionToken("cust_1", "ext_1", "tenant_1", "env_1", 1)
	require.NoError(t, err)

	tests := []struct {
		name       string
		status     types.TenantInternalStatus
		wantStatus int
	}{
		{"active tenant, roleless session allowed", types.TenantInternalStatusActive, http.StatusOK},
		{"suspended tenant rejected", types.TenantInternalStatusSuspended, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			log := newTestLogger(t)
			r := gin.New()
			r.Use(SessionTokenAuthMiddleware(cfg, log))
			tenants := &mockTenantService{status: tt.status}
			r.Use(TenantStatusMiddleware(tenants, log))
			r.PUT("/info", BlockWhenSuspended(tenants), func(c *gin.Context) { c.Status(http.StatusOK) })

			req := httptest.NewRequest(http.MethodPut, "/info", nil)
			req.Header.Set(types.HeaderSessionToken, token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestPortalBlockWhenSuspended(t *testing.T) {
	tests := []struct {
		name        string
		status      types.TenantInternalStatus
		tenant      *dto.TenantResponse
		wantStatus  int
		wantMessage string
	}{
		{"suspended tenant rejected with tenant name", types.TenantInternalStatusSuspended, &dto.TenantResponse{Name: "Acme"}, http.StatusForbidden, "This action is unavailable, contact Acme."},
		{"suspended tenant rejected without tenant name", types.TenantInternalStatusSuspended, nil, http.StatusForbidden, "This action is unavailable, contact the business that shared this portal."},
		{"active tenant allowed", types.TenantInternalStatusActive, nil, http.StatusOK, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.CtxTenantID, "tenant_1"))
				c.Next()
			})
			tenants := &mockTenantService{status: tt.status, tenant: tt.tenant}
			r.Use(TenantStatusMiddleware(tenants, newTestLogger(t)))
			r.PUT("/info", BlockWhenSuspended(tenants), func(c *gin.Context) { c.Status(http.StatusOK) })

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/info", nil))
			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantMessage != "" {
				assert.NotContains(t, w.Body.String(), "suspended")
				assert.JSONEq(t, `{"message":"`+tt.wantMessage+`"}`, w.Body.String())
			}
		})
	}
}
