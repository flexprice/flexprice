package middleware

import (
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

// RequirePermission returns a middleware that enforces two access controls:
// suspended tenants are blocked from write operations regardless of caller type,
// and service accounts are subject to RBAC role checks for the given entity and action.
func (pm *PermissionMiddleware) RequirePermission(entity string, action types.Action) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// Suspended tenants are blocked from all write operations.
		if action == types.ActionWrite && types.GetTenantInternalStatus(ctx) == types.TenantInternalStatusSuspended {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"message": "tenant account is suspended",
			})
			return
		}

		// Service accounts are subject to RBAC; JWT users and config keys are not.
		if types.IsServiceAccount(ctx) {
			roles := types.GetRoles(ctx)
			if !pm.rbacService.HasPermission(roles, entity, string(action)) {
				pm.logger.Info(ctx, "service account access refrained due to insufficient RBAC roles",
					"user_id", types.GetUserID(ctx),
					"tenant_id", types.GetTenantID(ctx),
					"environment_id", types.GetEnvironmentID(ctx),
					"roles", roles,
					"entity", entity,
					"action", action,
					"path", c.Request.URL.Path,
				)
			}
		}

		c.Next()
	}
}
