package middleware

import (
	"context"
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// TenantStatusMiddleware loads the tenant's InternalStatus on every authenticated
// request and stamps it onto the context. Write-access enforcement is handled by
// RequirePermission("entity", "write") on individual routes.
func TenantStatusMiddleware(tenantService service.TenantService, logger *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := types.GetTenantID(c.Request.Context())
		if tenantID == "" {
			logger.Info(c.Request.Context(), "tenant id not found", "tenant_id", tenantID)
			c.Next()
			return
		}

		status, err := tenantService.GetTenantInternalStatus(c.Request.Context(), tenantID)
		if err != nil {
			logger.Error(c.Request.Context(), "tenant status: failed to load tenant", "tenant_id", tenantID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to verify tenant access",
			})
			c.Abort()
			return
		}

		ctx := context.WithValue(c.Request.Context(), types.CtxTenantInternalStatus, status)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
