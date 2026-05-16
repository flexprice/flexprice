package middleware

import (
	"fmt"
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// PermissionMiddleware handles RBAC permission checks
type PermissionMiddleware struct {
	rbacService *rbac.RBACService
	logger      *logger.Logger
}

// NewPermissionMiddleware creates a new permission middleware instance
func NewPermissionMiddleware(rbacService *rbac.RBACService, logger *logger.Logger) *PermissionMiddleware {
	return &PermissionMiddleware{
		rbacService: rbacService,
		logger:      logger,
	}
}

// RequirePermission returns a middleware that checks for specific entity.action.
// For write actions it also blocks suspended tenants, so a single inline call
// handles both RBAC and tenant access control.
func (pm *PermissionMiddleware) RequirePermission(entity string, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		if action == "write" && types.GetTenantInternalStatus(ctx) == types.TenantInternalStatusSuspended {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "tenant account is suspended",
			})
			return
		}

		roles := types.GetRoles(ctx)
		if !pm.rbacService.HasPermission(roles, entity, action) {
			pm.logger.Info("Permission denied",
				"user_id", types.GetUserID(ctx),
				"roles", roles,
				"entity", entity,
				"action", action,
				"path", c.Request.URL.Path,
			)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "Forbidden",
				"message": fmt.Sprintf("Insufficient permissions to %s %s", action, entity),
			})
			return
		}

		c.Next()
	}
}
