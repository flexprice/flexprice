package middleware

import (
	"context"
	"net/http"

	domainTenant "github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// TenantContextMiddleware loads the tenant on every authenticated request and
// stamps InternalStatus onto the context. Write-access enforcement is handled
// by RequirePermission("entity", "write") on individual routes.
func TenantContextMiddleware(tenantRepo domainTenant.Repository, logger *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := types.GetTenantID(c.Request.Context())
		if tenantID == "" {
			c.Next()
			return
		}

		tenant, err := tenantRepo.GetByID(c.Request.Context(), tenantID)
		if err != nil {
			logger.Errorw("tenant context: failed to load tenant", "tenant_id", tenantID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to verify tenant access",
			})
			c.Abort()
			return
		}

		ctx := context.WithValue(c.Request.Context(), types.CtxTenantInternalStatus, tenant.InternalStatus)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
