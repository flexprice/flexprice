package middleware

import (
	"fmt"
	"net/http"

	"github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rbac"
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
		// Get secret from context (set by auth middleware)
		secretInterface, exists := c.Get("secret")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Unauthorized",
			})
			return
		}

		// Convert to domain secret
		secretEntity, ok := secretInterface.(*secret.Secret)
		if !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "Internal server error",
			})
			return
		}

		// Check permission using set-based lookup
		if !pm.rbacService.HasPermission(secretEntity.Roles, entity, action) {
			pm.logger.Info("Permission denied",
				"secret_id", secretEntity.ID,
				"roles", secretEntity.Roles,
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
