package middleware

import (
	"context"
	"net/http"

	domainTenant "github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// TenantAccessMiddleware stamps internal_status onto the context and blocks suspended tenants
// from performing write operations. Cache logic lives in the repo — GetByID always serves
// from force-cache on a hit, falling back to DB on a miss.
func TenantAccessMiddleware(tenantRepo domainTenant.Repository, logger *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := types.GetTenantID(c.Request.Context())
		if tenantID == "" {
			c.Next()
			return
		}

		t, err := tenantRepo.GetByID(c.Request.Context(), tenantID)
		if err != nil {
			logger.Errorw("tenant access: failed to load tenant", "tenant_id", tenantID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to verify tenant access",
			})
			c.Abort()
			return
		}

		// Suspended tenants can still read data; block only write operations.
		isWriteRequest := c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead
		if isWriteRequest && t.InternalStatus == types.TenantInternalStatusSuspended {
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
