package middleware

import (
	"context"
	"net/http"

	"github.com/flexprice/flexprice/internal/cache"
	domainTenant "github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// TenantAccessMiddleware checks the tenant's internal_status on every authenticated request.
// It reads from the force-cache (always enabled) to avoid a DB round-trip on hot paths,
// falling back to the DB only on a cache miss and re-populating the cache afterwards.
// Suspended tenants are blocked with 401; all other statuses pass through.
func TenantAccessMiddleware(tenantRepo domainTenant.Repository, logger *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := types.GetTenantID(c.Request.Context())
		if tenantID == "" {
			c.Next()
			return
		}

		var t *domainTenant.Tenant

		cacheClient := cache.GetInMemoryCache()
		cacheKey := cache.GenerateKey(cache.PrefixTenant, tenantID)

		if cached, found := cacheClient.ForceCacheGet(c.Request.Context(), cacheKey); found {
			if tenant, ok := cached.(*domainTenant.Tenant); ok {
				logger.Debugw("tenant access: cache hit", "tenant_id", tenantID)
				t = tenant
			}
		}

		if t == nil {
			var err error
			t, err = tenantRepo.GetByID(c.Request.Context(), tenantID)
			if err != nil {
				logger.Errorw("tenant access: failed to load tenant", "tenant_id", tenantID, "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "failed to verify tenant access",
				})
				c.Abort()
				return
			}
			cacheClient.ForceCacheSet(c.Request.Context(), cacheKey, t, cache.ExpiryDefaultInMemory)
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
