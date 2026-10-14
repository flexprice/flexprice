package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRequirePermission_Suspended(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pm := NewPermissionMiddleware(newRBACServiceWithRoles(t, superAdminRolesJSON), newTestLogger(t))

	tests := []struct {
		name   string
		status types.TenantInternalStatus
		opts   []PermissionOption
		want   int
	}{
		{"suspended write blocked", types.TenantInternalStatusSuspended, nil, http.StatusForbidden},
		{"suspended write allowed with option", types.TenantInternalStatusSuspended, []PermissionOption{AllowWhenSuspended()}, http.StatusOK},
		{"active write passes", types.TenantInternalStatusActive, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				ctx := context.WithValue(c.Request.Context(), types.CtxTenantInternalStatus, tt.status)
				ctx = context.WithValue(ctx, types.CtxRoles, []string{"super_admin"})
				c.Request = c.Request.WithContext(ctx)
			})
			r.POST("/x", pm.RequirePermission(types.EntityEvent, types.ActionWrite, tt.opts...), func(c *gin.Context) { c.Status(http.StatusOK) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", nil))
			assert.Equal(t, tt.want, w.Code)
		})
	}
}
