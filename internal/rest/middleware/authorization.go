package middleware

import (
	"net/http"
	"strings"

	"github.com/flexprice/flexprice/internal/auth/rbac"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// AuthorizationMiddleware creates middleware for RBAC authorization
func AuthorizationMiddleware(rbacService *rbac.Service, logger *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user and tenant info from context (set by authentication middleware)
		userID := types.GetUserID(c.Request.Context())
		tenantID := types.GetTenantID(c.Request.Context())

		if userID == "" || tenantID == "" {
			logger.Errorw("missing user or tenant information in context")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		// Extract resource and action from the request
		resource, action := extractResourceAndAction(c)

		// Check permission
		allowed, err := rbacService.CheckPermission(c.Request.Context(), userID, resource, action, tenantID)
		if err != nil {
			logger.Errorw("failed to check permission", "error", err, "user_id", userID, "resource", resource, "action", action, "tenant", tenantID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			c.Abort()
			return
		}

		if !allowed {
			logger.Warnw("permission denied", "user_id", userID, "resource", resource, "action", action, "tenant", tenantID)
			c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden", "message": "You don't have permission to perform this action"})
			c.Abort()
			return
		}

		logger.Debugw("permission granted", "user_id", userID, "resource", resource, "action", action, "tenant", tenantID)
		c.Next()
	}
}

// extractResourceAndAction extracts the resource and action from the HTTP request
func extractResourceAndAction(c *gin.Context) (string, string) {
	path := c.Request.URL.Path
	method := c.Request.Method

	// Remove /v1 prefix if present
	if strings.HasPrefix(path, "/v1") {
		path = strings.TrimPrefix(path, "/v1")
	}

	// Map HTTP methods to actions
	action := mapMethodToAction(method)

	// Extract resource from path
	resource := extractResourceFromPath(path)

	return resource, action
}

// mapMethodToAction maps HTTP methods to RBAC actions
func mapMethodToAction(method string) string {
	switch method {
	case "GET":
		return "read"
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return "read"
	}
}

// extractResourceFromPath extracts the resource type from the URL path
func extractResourceFromPath(path string) string {
	// Remove leading slash
	path = strings.TrimPrefix(path, "/")

	// Split by slashes
	parts := strings.Split(path, "/")

	// Return the first part as the resource
	if len(parts) > 0 {
		return parts[0]
	}

	return "unknown"
}

// ResourceOwnershipMiddleware creates middleware for checking resource ownership
func ResourceOwnershipMiddleware(rbacService *rbac.Service, logger *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user and tenant info from context
		userID := types.GetUserID(c.Request.Context())
		tenantID := types.GetTenantID(c.Request.Context())

		if userID == "" || tenantID == "" {
			logger.Errorw("missing user or tenant information in context")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		// Extract resource ID from path
		resourceID := c.Param("id")
		if resourceID == "" {
			// No resource ID to check, continue
			c.Next()
			return
		}

		// Extract resource type from path
		resourceType := extractResourceFromPath(c.Request.URL.Path)

		// Check resource ownership
		ownsResource, err := rbacService.ValidateResourceOwnership(c.Request.Context(), userID, resourceID, resourceType, tenantID)
		if err != nil {
			logger.Errorw("failed to validate resource ownership", "error", err, "user_id", userID, "resource_id", resourceID, "resource_type", resourceType, "tenant", tenantID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			c.Abort()
			return
		}

		if !ownsResource {
			logger.Warnw("resource ownership denied", "user_id", userID, "resource_id", resourceID, "resource_type", resourceType, "tenant", tenantID)
			c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden", "message": "You don't have access to this resource"})
			c.Abort()
			return
		}

		logger.Debugw("resource ownership validated", "user_id", userID, "resource_id", resourceID, "resource_type", resourceType, "tenant", tenantID)
		c.Next()
	}
}

// TenantAccessMiddleware creates middleware for checking tenant access
func TenantAccessMiddleware(rbacService *rbac.Service, logger *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user and tenant info from context
		userID := types.GetUserID(c.Request.Context())
		tenantID := types.GetTenantID(c.Request.Context())

		if userID == "" || tenantID == "" {
			logger.Errorw("missing user or tenant information in context")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		// Check tenant access
		hasAccess, err := rbacService.CheckTenantAccess(c.Request.Context(), userID, tenantID)
		if err != nil {
			logger.Errorw("failed to check tenant access", "error", err, "user_id", userID, "tenant", tenantID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			c.Abort()
			return
		}

		if !hasAccess {
			logger.Warnw("tenant access denied", "user_id", userID, "tenant", tenantID)
			c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden", "message": "You don't have access to this tenant"})
			c.Abort()
			return
		}

		logger.Debugw("tenant access granted", "user_id", userID, "tenant", tenantID)
		c.Next()
	}
}
