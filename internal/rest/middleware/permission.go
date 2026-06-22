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

// RequirePermission returns a middleware that:
//  1. Blocks ALL callers from write operations when the tenant is suspended.
//  2. Enforces RBAC only for service accounts
func (pm *PermissionMiddleware) RequirePermission(entity string, action types.Action) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// Gate 1: suspended tenants cannot perform any write, regardless of caller type.
		if action == types.ActionWrite && types.GetTenantInternalStatus(ctx) == types.TenantInternalStatusSuspended {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"message": "tenant account is suspended",
			})
			return
		}

		// Gate 2: RBAC enforcement for service accounts only.
		if types.IsServiceAccount(ctx) {
			roles := types.GetRoles(ctx)
			if !pm.rbacService.HasPermission(roles, entity, string(action)) {
				pm.logger.Info(ctx, "permission denied",
					"user_id", types.GetUserID(ctx),
					"roles", roles,
					"entity", entity,
					"action", action,
					"path", c.Request.URL.Path,
				)
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"message": fmt.Sprintf("Insufficient permissions to %s %s", action, entity),
				})
				return
			}
		}

		c.Next()
	}
}
