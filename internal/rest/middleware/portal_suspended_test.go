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

func TestPortalBlockWhenSuspended(t *testing.T) {
	tests := []struct {
		name       string
		status     types.TenantInternalStatus
		wantStatus int
	}{
		{"suspended tenant rejected", types.TenantInternalStatusSuspended, http.StatusForbidden},
		{"active tenant allowed", types.TenantInternalStatusActive, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.CtxTenantID, "tenant_1"))
				c.Next()
			})
			r.Use(TenantStatusMiddleware(&mockTenantService{status: tt.status}, newTestLogger(t)))
			r.PUT("/info", BlockWhenSuspended(), func(c *gin.Context) { c.Status(http.StatusOK) })

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/info", nil))
			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantStatus == http.StatusForbidden {
				assert.JSONEq(t, `{"message":"tenant account is suspended"}`, w.Body.String())
			}
		})
	}
}
