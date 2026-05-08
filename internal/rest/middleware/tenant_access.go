package middleware

import (
	"context"
	"net/http"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// TenantAccessMiddleware fetches the tenant (served from in-memory cache after the first
// request per tenant), stamps internal_status onto the context, and blocks the request if
// the tenant is suspended.
func TenantAccessMiddleware(tenantRepo tenant.Repository, logger *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := types.GetTenantID(c.Request.Context())
		if tenantID == "" {
			c.Next()
			return
		}

		t, err := tenantRepo.GetByID(c.Request.Context(), tenantID)
		if err != nil {
			logger.Errorw("tenant access: failed to load tenant", "tenant_id", tenantID, "error", err)
			c.Next()
			return
		}

		if t.InternalStatus == types.TenantInternalStatusSuspended {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "tenant account is suspended",
			})
			c.Abort()
			return
		}

		ctx := context.WithValue(c.Request.Context(), types.CtxTenantInternalStatus, t.InternalStatus)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
