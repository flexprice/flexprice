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

// RequirePermission is the standard gate for mutating routes. It:
//  1. Blocks suspended tenants on write actions (all caller types).
//  2. For service accounts only, enforces RBAC — the service account's roles must
//     include (entity, action). JWT users and config API keys are never RBAC-restricted.
func (pm *PermissionMiddleware) RequirePermission(entity string, action types.Action) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		if action == types.ActionWrite && types.GetTenantInternalStatus(ctx) == types.TenantInternalStatusSuspended {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "tenant account is suspended",
			})
			return
		}

		if types.IsServiceAccount(ctx) {
			roles := types.GetRoles(ctx)
			if !pm.rbacService.HasPermission(roles, entity, string(action)) {
				pm.logger.Infow("Service account permission denied",
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
		}

		c.Next()
	}
}
