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

// RequirePermission returns a middleware that checks for specific entity.action
// This is called explicitly in route definitions
func (pm *PermissionMiddleware) RequirePermission(entity string, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get roles from context (set by auth middleware)
		roles := types.GetRoles(c.Request.Context())

		// Check permission using set-based lookup
		if !pm.rbacService.HasPermission(roles, entity, action) {
			pm.logger.Info("Permission denied",
				"user_id", types.GetUserID(c.Request.Context()),
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

		// Permission granted, continue to handler
		c.Next()
	}
}
